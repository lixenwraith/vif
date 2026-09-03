package app

import (
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
)

// correctionSteps is how often the two-participant criteria assert convergence.
//
// Weakened D-11 does not say two instances agree at every tick: the local path
// carries no playout lead, so each participant's own artifacts land a lead earlier
// on it than on anyone else. It says a guest is equal to the host as of the last
// applied correction, which is what these tests assert, at the only moments the
// rule makes a claim about.
const correctionSteps = 8

// The selective exchange's settling budget between two ticks.
//
// correctionExchangeFastPasses is what an in-process link needs: the manifest is
// answered on the first pass, the repair it provoked is served and applied on the
// second, and the third is the margin a keyframe fallback takes. The passes past
// it exist for a socket, which delivers on its own goroutine — they wait
// correctionExchangePoll each, so a round trip over loopback has about six
// milliseconds to complete before the harness gives up and advances a tick.
const (
	correctionExchangeFastPasses = 3
	correctionExchangePasses     = 30
	correctionExchangePoll       = 200 * time.Microsecond
)

// deliverCorrection publishes one authoritative correction from the host and gets
// it applied on the guest, returning the host's shared state at the instant it was
// read.
//
// The delivery loop is the transport's, not the test's: chunks reach a guest
// through the ordinary inbound drain, which is part of a tick, and a relayed
// session needs one tick per hop. The install is between two ticks, which on a
// driven run is where Tick puts it.
// deliverCorrection quiesces the session first, so what it asserts is convergence
// rather than the absence of traffic.
//
// Not everything SnapshotShared compares is in a capture, and the difference
// matters here. `context.crop_on_resize` is this instance's answer to a resize, so
// no capture carries it and no correction can converge it — what makes two
// participants agree on it is the crossing that changed it, and that crossing
// lands a playout lead earlier on its producer than on anyone else.
// Letting the lead drain before the world is read is what separates "the guest has
// the host's world" from "the guest has not caught up with an artifact yet", and
// only the first is a claim about corrections.
func deliverCorrection(t *testing.T, host *App, guests []*App, advance func()) []string {
	t.Helper()
	for range parameter.NetworkBarrierDelayTicks + 1 {
		advance()
	}
	return deliverCorrectionNow(t, host, guests, advance)
}

// deliverCorrectionNow publishes without draining the playout lead first, for a
// caller measuring how far a guest's prediction had actually gone.
func deliverCorrectionNow(t *testing.T, host *App, guests []*App, advance func()) []string {
	t.Helper()
	if err := host.PublishCorrection(); err != nil {
		t.Fatalf("publish correction: %v", err)
	}
	want := host.SnapshotShared()
	tick := host.Position().Tick

	// Waiting on the applied counter rather than on a tick number: a guest can
	// already be standing on the tick a correction describes and still not have
	// taken it, which is exactly the case a tick comparison reads as success.
	before := make([]int64, len(guests))
	for i, g := range guests {
		before[i] = statOf(g, "snapshot.corrections_applied")
	}
	// The exchange is driven before the clock is: a manifest is answered and the
	// repair it provokes served between two ticks, so a correction over a direct
	// link completes without the participants moving and the comparison is at the
	// tick the correction describes. A tick is still advanced when a round did not
	// complete, which is what a relayed session needs: its bodies travel as chunks
	// and every hop costs one.
	applied := func() bool {
		done := true
		for i, g := range guests {
			g.ApplyPendingCorrections()
			if statOf(g, "snapshot.corrections_applied") == before[i] {
				done = false
			}
		}
		return done
	}
	for range parameter.NetworkRelayHopLimit {
		// Each leg of the exchange is drained, answered and served between
		// two ticks, so the whole round trip completes without the participants
		// moving: the manifest is answered on one pass and the repair it provoked
		// is served and applied on the next. Settling it here is what makes the
		// comparison below one about the tick the correction describes rather than
		// about a guest that has predicted past it.
		//
		// The later passes wait a moment first. An in-process link hands a frame
		// over inside the call that sent it, so the exchange finishes in the first
		// two passes and no wait is paid; a socket delivers on its own goroutine,
		// and without the wait the harness would advance a tick before the manifest
		// had crossed the loopback interface — turning an asynchronous transport
		// into a divergence the protocol never had.
		for pass := range correctionExchangePasses {
			if pass >= correctionExchangeFastPasses {
				time.Sleep(correctionExchangePoll) // [wall] a transport wait, not a game one
			}
			host.ApplyPendingCorrections()
			if applied() {
				return want
			}
		}
		// A tick is still advanced when a round did not complete, which is what a
		// relayed session needs: its bodies travel as chunks and every hop costs one.
		advance()
	}
	t.Fatalf("a correction for tick %d never reached every guest", tick)
	return nil
}

// assertCorrected fails with the first record a guest does not hold as the host
// held it when the correction was read.
func assertCorrected(t *testing.T, want []string, guest *App, label string) {
	t.Helper()
	got := guest.SnapshotShared()
	if idx, lw, lg, ok := snapshot.FirstDiff(want, got); ok {
		t.Fatalf("%s did not converge on the correction, line %d\n  host:  %s\n  guest: %s\n%s",
			label, idx, lw, lg, strings.Join(snapshot.Diff(want, got, 8), "\n"))
	}
}

// TestGuestConvergesOnEveryCorrection is the headline criterion, and the
// replacement for a lockstep one.
//
// A guest applies its own input immediately and extrapolates between corrections,
// so it is expected to differ from the host; what the rule claims is that every
// correction closes the difference exactly. The magnitude in between is telemetry —
// it is asserted to be non-zero here, because a criterion that passed with a guest
// that never predicted anything would be proving nothing.
func TestGuestConvergesOnEveryCorrection(t *testing.T) {
	t.Parallel()
	const seed = 0x5EEDBEEF
	host, guest := pair(t, seed, 0)
	localA, localB := mirrorCursors(t, host, guest)

	advance := func() { host.Tick(1); guest.Tick(1) }
	drifted := false
	for round := range 6 {
		// Each participant drives its own cursor, which is what makes their shared
		// worlds disagree between corrections at all.
		inject(t, host, intentMotion(input.MotionRight, 1))
		inject(t, guest, intentMotion(input.MotionLeft, 1))
		for range 3 {
			advance()
		}
		if !sharedStatesAgree(host, guest) {
			drifted = true
		}
		want := deliverCorrection(t, host, []*App{guest}, advance)
		assertCorrected(t, want, guest, "guest")
		if round == 0 && localA == localB {
			t.Fatal("the two participants drive one cursor; the probe is vacuous")
		}
	}
	if !drifted {
		t.Fatal("the two instances never disagreed, so convergence proves nothing")
	}
	if applied := statOf(guest, "snapshot.corrections_applied"); applied == 0 {
		t.Fatal("the guest applied no correction")
	}
}

// sharedStatesAgree reports whether two instances hold the same shared surface.
func sharedStatesAgree(a, b *App) bool {
	_, _, _, differs := snapshot.FirstDiff(a.SnapshotShared(), b.SnapshotShared())
	return !differs
}

// TestCorrectionMagnitudeIsMeasuredNotAsserted pins the number that replaced
// DESYNC: how far this instance's prediction had drifted when the authority
// arrived, published rather than escalated.
func TestCorrectionMagnitudeIsMeasuredNotAsserted(t *testing.T) {
	t.Parallel()
	const seed = 0x5EEDBEEF
	host, guest := pair(t, seed, 0)
	mirrorCursors(t, host, guest)
	advance := func() { host.Tick(1); guest.Tick(1) }

	// A placement only the host knows about: it applies locally at once and the
	// guest learns of it a playout lead later, so a correction read in between
	// describes a world the guest does not have.
	var hostCursor core.Entity
	host.World().RunSafe(func() { hostCursor = host.World().Resources.Player.Slot(0) })
	from := cursorPosition(host, hostCursor)
	host.Context().PushCrossing(event.EventCursorMoveRequest,
		&event.CursorMoveRequestPayload{Entity: hostCursor, X: from.X + 7, Y: from.Y + 3})
	host.Settle()

	want := deliverCorrectionNow(t, host, []*App{guest}, advance)
	assertCorrected(t, want, guest, "guest")

	got := guest.correctionMagnitude()
	if got.Entities == 0 || got.Entries == 0 {
		t.Fatalf("correction magnitude = %+v, want a drift the guest had to be told about", got)
	}
	if got.CellShift == 0 {
		t.Fatalf("correction magnitude = %+v, want a placement that moved", got)
	}
	// Nothing escalated: the drift is the ordinary condition this phase created.
	if diverged := host.World().Resources.Status.Bools.Has("network.diverged"); diverged {
		t.Fatal("network.diverged is still registered; DESYNC/DIVERGED were retired")
	}
}

// TestCorrectionDeltaRoundTripsExactly is the delta's whole claim: applying it to
// the baseline it names reproduces the sender's capture byte for byte.
//
// The integrity hash is what says "byte for byte" rather than "equivalent". A delta
// that rebuilt the same entities in a different store order would pass every value
// comparison and fail this, which is why the delta carries entity order at all.
func TestCorrectionDeltaRoundTripsExactly(t *testing.T) {
	t.Parallel()
	for _, seed := range []uint64{0x5EEDBEEF, 0xC0FFEE, 0x1234} {
		a := mustHeadless(t, seed, 120, 40)
		tickUntilCursor(t, a)
		a.Tick(60)

		base, err := a.CaptureShared()
		if err != nil {
			t.Fatalf("seed %#x baseline capture: %v", seed, err)
		}
		a.Tick(40)
		next, err := a.CaptureShared()
		if err != nil {
			t.Fatalf("seed %#x capture: %v", seed, err)
		}

		delta := snapshot.DiffCapture(base, next)
		rebuilt, err := snapshot.ApplyCaptureDelta(base, delta)
		if err != nil {
			t.Fatalf("seed %#x apply delta: %v", seed, err)
		}
		wantBytes, err := snapshot.EncodeCapture(next)
		if err != nil {
			t.Fatalf("seed %#x encode: %v", seed, err)
		}
		gotBytes, err := snapshot.EncodeCapture(rebuilt)
		if err != nil {
			t.Fatalf("seed %#x encode rebuilt: %v", seed, err)
		}
		if string(wantBytes) != string(gotBytes) {
			t.Fatalf("seed %#x: a delta rebuilt %d bytes, want the sender's %d",
				seed, len(gotBytes), len(wantBytes))
		}
		// Non-vacuous in both directions: the delta has to be smaller than the
		// capture, or it is buying nothing, and it has to carry something, or the
		// two captures were the same world and the round trip proved nothing.
		deltaBytes, err := snapshot.EncodeCorrectionDelta(delta)
		if err != nil {
			t.Fatalf("seed %#x encode delta: %v", seed, err)
		}
		if len(deltaBytes) >= len(wantBytes) {
			t.Fatalf("seed %#x: delta is %d bytes against a %d-byte capture",
				seed, len(deltaBytes), len(wantBytes))
		}
		if delta.World.DeltaEntries() == 0 {
			t.Fatalf("seed %#x: forty ticks moved nothing, so the round trip is vacuous", seed)
		}
		a.Close()
	}
}

// TestCorrectionDeltaRefusesAForeignBaseline is the other half of the delta's
// contract. A delta is worthless without the keyframe it names, and a receiver that
// applied one to the wrong world would install a world nobody has — the state would
// look consistent and describe nothing.
func TestCorrectionDeltaRefusesAForeignBaseline(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, 0x5EEDBEEF, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)
	a.Tick(40)
	base, err := a.CaptureShared()
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	a.Tick(20)
	mid, err := a.CaptureShared()
	if err != nil {
		t.Fatalf("mid: %v", err)
	}
	a.Tick(20)
	next, err := a.CaptureShared()
	if err != nil {
		t.Fatalf("next: %v", err)
	}

	delta := snapshot.DiffCapture(base, next)
	if _, err := snapshot.ApplyCaptureDelta(mid, delta); err == nil {
		t.Fatal("a delta was applied to a baseline it does not name")
	}

	// And a delta whose baseline tick is right but whose body is not: the tick
	// check passes and the integrity hash is what refuses it.
	forged := snapshot.DiffCapture(base, next)
	forged.BaselineTick = mid.Header.Tick
	mid.Header.Tick = base.Header.Tick
	if _, err := snapshot.ApplyCaptureDelta(mid, forged); err == nil {
		t.Fatal("a delta rebuilt a body its header does not describe and was accepted")
	}
}

// TestStagingWorldIsBuiltOnceAndReused. A second App per install costs 9 to 31 ms,
// which suits a join and not a correction at cadence.
//
// Re-use is only sound if the second install leaves exactly what a world built for
// it alone would: a carrier that merged rather than replaced, or a store that kept
// an entity the next capture does not have, would resolve the following correction
// against a world the sender never held.
func TestStagingWorldIsBuiltOnceAndReused(t *testing.T) {
	t.Parallel()
	const seed = 0x5EEDBEEF
	source := mustHeadless(t, seed, 120, 40)
	defer source.Close()
	tickUntilCursor(t, source)
	source.Tick(40)
	first, err := source.CaptureShared()
	if err != nil {
		t.Fatalf("first capture: %v", err)
	}
	source.Tick(60)
	second, err := source.CaptureShared()
	if err != nil {
		t.Fatalf("second capture: %v", err)
	}

	// One receiver takes both in sequence, re-using its staging world.
	reused := mustHeadless(t, seed, 120, 40)
	defer reused.Close()
	tickUntilCursor(t, reused)
	stagedFirst, err := reused.StageShared(first)
	if err != nil {
		t.Fatalf("stage first: %v", err)
	}
	world := stagedFirst.StagingWorld()
	stagedFirst.Discard()
	stagedSecond, err := reused.StageShared(second)
	if err != nil {
		t.Fatalf("stage second: %v", err)
	}
	if stagedSecond.StagingWorld() != world {
		t.Fatal("the second install built a second staging world")
	}
	got := stagedSecond.StagingWorld().SnapshotShared()
	stagedSecond.Discard()

	// Another takes only the second, into a world built for it.
	fresh := mustHeadless(t, seed, 120, 40)
	defer fresh.Close()
	tickUntilCursor(t, fresh)
	stagedFresh, err := fresh.StageShared(second)
	if err != nil {
		t.Fatalf("stage fresh: %v", err)
	}
	want := stagedFresh.StagingWorld().SnapshotShared()
	stagedFresh.Discard()

	if idx, lw, lg, ok := snapshot.FirstDiff(want, got); ok {
		t.Fatalf("a re-used staging world differs from a fresh one at line %d\n  fresh:  %s\n  reused: %s\n%s",
			idx, lw, lg, strings.Join(snapshot.Diff(want, got, 8), "\n"))
	}
}

// TestReconcileMatchesAFullInstall is what lets a commit stop being a second full
// write. A correction moves the live world onto the capture instead of clearing and
// re-inserting it, and the only thing that makes that safe is that the two produce
// the same world — including the components an entity stopped carrying and the
// entities the authority no longer has at all.
func TestReconcileMatchesAFullInstall(t *testing.T) {
	t.Parallel()
	const seed = 0x5EEDBEEF
	source := mustHeadless(t, seed, 120, 40)
	defer source.Close()
	tickUntilCursor(t, source)
	source.Tick(40)
	early, err := source.CaptureShared()
	if err != nil {
		t.Fatalf("early capture: %v", err)
	}
	source.Tick(120)
	late, err := source.CaptureShared()
	if err != nil {
		t.Fatalf("late capture: %v", err)
	}

	replaced := mustHeadless(t, seed, 120, 40)
	defer replaced.Close()
	tickUntilCursor(t, replaced)
	if err := replaced.InstallShared(early); err != nil {
		t.Fatalf("install early: %v", err)
	}
	if err := replaced.InstallShared(late); err != nil {
		t.Fatalf("install late: %v", err)
	}

	moved := mustHeadless(t, seed, 120, 40)
	defer moved.Close()
	tickUntilCursor(t, moved)
	if err := moved.InstallShared(early); err != nil {
		t.Fatalf("install early: %v", err)
	}
	diff, err := moved.reconcileShared(late)
	if err != nil {
		t.Fatalf("reconcile late: %v", err)
	}
	if diff.Entries == 0 {
		t.Fatal("120 ticks moved nothing, so the reconcile proved nothing")
	}

	want, got := replaced.SnapshotShared(), moved.SnapshotShared()
	if idx, lw, lg, ok := snapshot.FirstDiff(want, got); ok {
		t.Fatalf("reconcile differs from a full install at line %d\n  install:   %s\n  reconcile: %s\n%s",
			idx, lw, lg, strings.Join(snapshot.Diff(want, got, 8), "\n"))
	}

	// And the futures agree, which is the claim the digest alone cannot make.
	for range 60 {
		replaced.Tick(1)
		moved.Tick(1)
	}
	want, got = replaced.SnapshotShared(), moved.SnapshotShared()
	if idx, lw, lg, ok := snapshot.FirstDiff(want, got); ok {
		t.Fatalf("reconciled world diverged 60 ticks later at line %d\n  install:   %s\n  reconcile: %s",
			idx, lw, lg)
	}
}

// TestCrossingApplyTimes covers both halves of the ordering rule over one pair.
//
// An ordinary crossing applies on its producer in the tick that produced it; the
// peers keep the playout lead, which is an interpolation buffer for remote action
// rather than a barrier on anyone's input. Arrival and departure are the exception:
// they create and destroy shared cursors, and a shared entity's identity and
// creation order are what every capture references by, so they apply at one agreed
// tick on the producer too.
func TestCrossingApplyTimes(t *testing.T) {
	t.Parallel()
	a, b := pair(t, 0x5EEDBEEF, 0)
	mirrorCursors(t, a, b)

	var target core.Entity
	a.World().RunSafe(func() { target = a.World().Resources.Player.Slot(0) })
	start := cursorPosition(a, target)
	want := start
	want.X += 3

	a.Context().PushCrossing(event.EventCursorMoveRequest,
		&event.CursorMoveRequestPayload{Entity: target, X: want.X, Y: want.Y})
	a.Settle()

	if got := cursorPosition(a, target); got != want {
		t.Fatalf("the producer's own crossing landed at %#v, want %#v with no lead", got, want)
	}
	if got := cursorPosition(b, target); got != start {
		t.Fatalf("the peer applied a crossing before its apply tick: %#v", got)
	}
	for range parameter.NetworkBarrierDelayTicks + 1 {
		a.Tick(1)
		b.Tick(1)
	}
	if got := cursorPosition(b, target); got != want {
		t.Fatalf("the peer applied the crossing as %#v, want %#v", got, want)
	}
	if local := statOf(a, "network.crossings_local"); local == 0 {
		t.Fatal("no crossing was counted as applied without the lead")
	}
	if sent := statOf(a, "network.crossings_sent"); sent == 0 {
		t.Fatal("the crossing applied locally but never reached the wire")
	}

	before := 0
	a.World().RunSafe(func() { before = a.World().Resources.Player.Count() })
	a.World().PushEventFull(event.EventParticipantJoined,
		&event.ParticipantJoinedPayload{Participant: 3, Slot: 2}, event.OriginSession, core.DomainPlayer)
	a.Settle()

	var during int
	a.World().RunSafe(func() { during = a.World().Resources.Player.Count() })
	if during != before {
		t.Fatalf("an arrival crossing spawned on its producer before its apply tick: %d cursors", during)
	}
	for range parameter.NetworkBarrierDelayTicks + 1 {
		a.Tick(1)
		b.Tick(1)
	}
	var afterA, afterB int
	var entityA, entityB core.Entity
	a.World().RunSafe(func() {
		afterA = a.World().Resources.Player.Count()
		entityA = a.World().Resources.Player.Slot(2)
	})
	b.World().RunSafe(func() {
		afterB = b.World().Resources.Player.Count()
		entityB = b.World().Resources.Player.Slot(2)
	})
	if afterA != before+1 || afterB != before+1 {
		t.Fatalf("arrival produced %d and %d cursors, want %d on both", afterA, afterB, before+1)
	}
	if entityA == 0 || entityA != entityB {
		t.Fatalf("the arrival took entity %d on the host and %d on the guest", entityA, entityB)
	}
}

// TestHostRefusesARosterCrossingFromAnyoneElse validates this. The
// coordinator is the only producer of an arrival or a departure, because one
// producer is what gives them a single apply tick; an artifact of either kind from
// anyone else would create or destroy a shared entity the session never agreed to.
func TestHostRefusesARosterCrossingFromAnyoneElse(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 3, [][2]int{{1, 2}, {2, 3}, {1, 3}})
	localCursors(t, apps)
	host, forger := apps[0], apps[1]

	before := 0
	host.World().RunSafe(func() { before = host.World().Resources.Player.Count() })

	// Participant 2 produces an arrival for a slot nobody assigned.
	forger.World().PushEventFull(event.EventParticipantJoined,
		&event.ParticipantJoinedPayload{Participant: 4, Slot: 3}, event.OriginSession, core.DomainPlayer)
	forger.Settle()
	for range parameter.NetworkBarrierDelayTicks + 2 {
		tickAll(apps)
	}

	var after int
	host.World().RunSafe(func() { after = host.World().Resources.Player.Count() })
	if after != before {
		t.Fatalf("a forged arrival created a cursor on the host: %d, want %d", after, before)
	}
	if refused := statOf(host, "network.artifacts_refused"); refused == 0 {
		t.Fatal("the host applied the forged roster artifact instead of refusing it")
	}
}

// TestSessionLagIsMeasuredEveryTick. Measuring the gap only at admission leaves a
// guest whose machine falls behind mid-session producing artifacts that reach the
// host after the ticks they name, with no way to know.
func TestSessionLagIsMeasuredEveryTick(t *testing.T) {
	t.Parallel()
	a, b := pair(t, 0x5EEDBEEF, 0)
	mirrorCursors(t, a, b)

	for range 4 {
		a.Tick(1)
		b.Tick(1)
	}
	if lag := statOf(b, "network.lag_ticks"); lag != 0 {
		t.Fatalf("a participant in step reports %d ticks of lag", lag)
	}
	if statBoolOf(b, "network.stale") {
		t.Fatal("a participant in step is reported stale")
	}

	// Only the host advances: the guest falls behind exactly as a slow machine
	// would, and the measurement has to say so without anything asking it to.
	for range parameter.SnapshotStaleTicks + 4 {
		a.Tick(1)
	}
	b.Tick(1)
	if lag := statOf(b, "network.lag_ticks"); lag <= int64(parameter.SnapshotStaleTicks) {
		t.Fatalf("a guest %d ticks behind reports %d ticks of lag",
			parameter.SnapshotStaleTicks+4, lag)
	}
	if !statBoolOf(b, "network.stale") {
		t.Fatal("a guest past the playout lead is not reported stale")
	}
}

// TestCorrectionCarriesTheWholeDeclaredSurface guards the one shortcut the delta
// takes: only the world half is differenced, and everything else — stream
// positions, every D-19 carrier, the FSM's runtime position, the compared status
// surface — travels whole in both shapes.
func TestCorrectionCarriesTheWholeDeclaredSurface(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, 0x5EEDBEEF, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)
	a.Tick(40)
	base, err := a.CaptureShared()
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	a.Tick(40)
	next, err := a.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	delta := snapshot.DiffCapture(base, next)

	if len(delta.Streams) != len(next.Streams) || len(delta.Streams) == 0 {
		t.Fatalf("delta carries %d stream positions, want the capture's %d",
			len(delta.Streams), len(next.Streams))
	}
	if len(delta.Systems) != len(next.Systems) || len(delta.Systems) == 0 {
		t.Fatalf("delta carries %d system records, want the capture's %d",
			len(delta.Systems), len(next.Systems))
	}
	if len(delta.Status.Ints) != len(next.Status.Ints) {
		t.Fatalf("delta carries %d status integers, want the capture's %d",
			len(delta.Status.Ints), len(next.Status.Ints))
	}
	if len(delta.FSM.Regions) != len(next.FSM.Regions) || len(delta.FSM.Regions) == 0 {
		t.Fatalf("delta carries %d FSM regions, want the capture's %d",
			len(delta.FSM.Regions), len(next.FSM.Regions))
	}
}

// TestWorldDifferenceCountsWhatMoved pins the magnitude's unit, since it is the
// number a cadence gets chosen from.
func TestWorldDifferenceCountsWhatMoved(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, 0x5EEDBEEF, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)

	before, err := a.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if d := engine.SharedWorldDifference(before.World, before.World); d != (engine.WorldDifference{}) {
		t.Fatalf("a world compared against itself differs by %+v", d)
	}

	var cursor core.Entity
	a.World().RunSafe(func() { cursor = a.World().Resources.Player.Slot(0) })
	from := cursorPosition(a, cursor)
	a.Context().PushEventOrigin(event.EventCursorMoveRequest,
		&event.CursorMoveRequestPayload{Entity: cursor, X: from.X + 5, Y: from.Y}, event.OriginDebug)
	a.Settle()

	after, err := a.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	d := engine.SharedWorldDifference(before.World, after.World)
	if d.Entities == 0 || d.Entries == 0 {
		t.Fatalf("moving a shared cursor five cells reported %+v", d)
	}
	if d.CellShift != 5 {
		t.Fatalf("cell shift = %d, want the five cells the cursor moved", d.CellShift)
	}
}

// TestJoinReusesTheCadencesKeyframe is the world-lock half of a mid-run join.
//
// Reading the world once per participant on the accept goroutine makes a second
// participant dialling mid-join wait behind the first one's read as well as behind
// its transfer. A host publishes keyframes on a cadence, so a join takes whichever
// one is fresh enough and reads the world only when none is: the read is
// per-cadence rather than per-join, and two joins arriving together share one.
func TestJoinReusesTheCadencesKeyframe(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, 0x5EEDBEEF, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)
	a.Tick(20)

	deadline := time.Now().Add(socketWait)
	first, firstTick, err := a.corrections.keyframeAt(0, deadline)
	if err != nil {
		t.Fatalf("first keyframe: %v", err)
	}
	second, secondTick, err := a.corrections.keyframeAt(firstTick, deadline)
	if err != nil {
		t.Fatalf("second keyframe: %v", err)
	}
	if secondTick != firstTick || &second[0] != &first[0] {
		t.Fatalf("a second join at tick %d read the world again instead of taking the keyframe at %d",
			secondTick, firstTick)
	}

	// A join needs a world *later* than its admission, and asking for one this run
	// has not reached is a refusal rather than a stale capture.
	if _, _, err := a.corrections.keyframeAt(firstTick+4, time.Now().Add(50*time.Millisecond)); err == nil {
		t.Fatal("a keyframe was produced for a tick the session has not reached")
	}

	a.Tick(8)
	third, thirdTick, err := a.corrections.keyframeAt(firstTick+4, deadline)
	if err != nil {
		t.Fatalf("third keyframe: %v", err)
	}
	if thirdTick <= firstTick {
		t.Fatalf("keyframe tick %d, want one past %d", thirdTick, firstTick)
	}
	if len(third) == 0 {
		t.Fatal("the fresh keyframe is empty")
	}
}

// TestMidRunJoinWaitsOutThePlayoutLead is the window this phase closed.
//
// D-22 admits a participant before the world is read for it, so that the epochs
// produced in between reach it rather than falling into the gap. What that ordering
// does not cover is an epoch produced *before* the admission and flushed to the
// peers this instance had at that moment: it reaches the joiner not at all, and a
// capture taken at the admission tick does not contain it either, because its apply
// tick is still a playout lead ahead and the barrier's floor does not drop it.
//
// A join therefore asks for a world a lead further on. By then every artifact
// produced before the admission has applied into the capture, and the copies that do
// arrive are recognised as already-contained.
func TestMidRunJoinWaitsOutThePlayoutLead(t *testing.T) {
	t.Parallel()
	host := mustHeadless(t, 0x5EEDBEEF, 120, 40)
	defer host.Close()
	tickUntilCursor(t, host)
	host.Tick(30)
	if err := host.BeginHosting("127.0.0.1:0"); err != nil {
		t.Fatalf("begin hosting: %v", err)
	}

	admission := host.Position().Tick
	done := make(chan uint64, 1)
	go func() {
		_, tick, err := host.corrections.keyframeAt(
			admission+parameter.NetworkBarrierDelayTicks, time.Now().Add(socketWait))
		if err != nil {
			done <- 0
			return
		}
		done <- tick
	}()

	// The join is waiting on ticks it cannot take itself.
	for range parameter.NetworkBarrierDelayTicks + 2 {
		host.Tick(1)
	}
	select {
	case tick := <-done:
		if tick < admission+parameter.NetworkBarrierDelayTicks {
			t.Fatalf("a join installed the world at tick %d, admitted at %d with a lead of %d",
				tick, admission, parameter.NetworkBarrierDelayTicks)
		}
	case <-time.After(socketWait):
		t.Fatal("the join never got its world")
	}
}

// TestCorrectionsLeaveOneOrbPerArmedWeapon is the D-4/D-13 boundary a correction
// used to cross, and the shape of the defect is worth stating because nothing in
// the suite could see it.
//
// A weapon orb is a player-domain entity, and its handle means nothing on any other
// instance. The index that named a cursor's orbs by weapon type lived on
// `CursorViewComponent`, which is attached to the *shared* cursor, so it travelled
// in every capture: the host does not simulate a guest's weapons, its copy of that
// array is zero, and each correction handed the guest back zeroes over its own live
// handles. The next tick found three missing references and spawned three
// replacements. The entities the zeroes had named stayed in the Orb store —
// protected from decay, no longer followed by updateOrbs, and drawn by a renderer
// that iterates the store — so an armed guest accumulated three permanently
// rendered, permanently frozen orbs per correction until the player-domain per-cell
// limit started rejecting them.
//
// The index is derived from the Orb store now and no shared component names a
// player entity at all, which is what this asserts: after repeated corrections the
// guest holds exactly one orb per armed weapon, and every one of them is its own.
func TestCorrectionsLeaveOneOrbPerArmedWeapon(t *testing.T) {
	t.Parallel()
	const seed = 0x5EEDBEEF
	host, guest := pair(t, seed, 0)
	mirrorCursors(t, host, guest)
	advance := func() { host.Tick(1); guest.Tick(1) }

	var cursor core.Entity
	guest.World().RunSafe(func() { cursor = guest.World().Resources.Player.Slot(1) })

	armed := []component.WeaponType{component.WeaponRod, component.WeaponLauncher, component.WeaponDisruptor}
	for _, wt := range armed {
		guest.Context().PushLocal(event.EventWeaponAddRequest,
			&event.WeaponAddRequestPayload{Entity: cursor, Weapon: wt})
	}
	guest.Settle()
	advance()

	if got := orbsPerWeapon(guest, cursor); got != [component.WeaponCount]int{1, 1, 1} {
		t.Fatalf("orbs after arming = %v, want one per armed weapon", got)
	}
	before := statOf(guest, "snapshot.corrections_applied")

	for round := range 6 {
		deliverCorrection(t, host, []*App{guest}, advance)
		advance()

		// The guest still authors its own cursor, or the rest is vacuous: a cursor
		// this instance stopped simulating grows no orbs at all (D-2).
		if !simulatesLocally(guest, cursor) {
			t.Fatalf("round %d: the guest stopped simulating its own cursor %d", round, cursor)
		}
		if got := orbsPerWeapon(guest, cursor); got != [component.WeaponCount]int{1, 1, 1} {
			t.Fatalf("round %d: orbs = %v, want exactly one per armed weapon", round, got)
		}
		if got := orbCount(guest); got != len(armed) {
			t.Fatalf("round %d: the world holds %d orbs, the roster justifies %d",
				round, got, len(armed))
		}
	}
	if statOf(guest, "snapshot.corrections_applied") <= before {
		t.Fatal("no correction was applied; the pile-up had no chance to form")
	}
}

// TestCorrectionKeepsTheReceiversOwnCursorState is the other half of the same
// boundary. The owner-authored set has exactly one author and travels as values on
// its own stream (D-13); what a capture holds for a cursor the *receiver* drives is
// the sender's mirror of that stream, a sync period behind at best. A correction
// that adopted it rolled the guest's own energy, heat and loadout back to whatever
// the host had last heard, five times a second.
//
// The grant is settled and the correction published with no tick in between, so the
// host cannot have been told: what it publishes is provably its stale mirror, and
// the guest's own value is the only one with an author.
func TestCorrectionKeepsTheReceiversOwnCursorState(t *testing.T) {
	t.Parallel()
	const seed = 0x5EEDBEEF
	host, guest := pair(t, seed, 0)
	mirrorCursors(t, host, guest)
	advance := func() { host.Tick(1); guest.Tick(1) }

	var cursor core.Entity
	guest.World().RunSafe(func() { cursor = guest.World().Resources.Player.Slot(1) })

	guest.Context().PushLocal(event.EventWeaponAddRequest,
		&event.WeaponAddRequestPayload{Entity: cursor, Weapon: component.WeaponRod})
	guest.Settle()

	armed := rodCharges(guest, cursor)
	if armed == 0 {
		t.Fatal("the guest did not arm its own cursor")
	}
	if mirrored := rodCharges(host, cursor); mirrored == armed {
		t.Fatalf("the host already mirrors %d charges without a tick having passed", mirrored)
	}

	deliverCorrectionNow(t, host, []*App{guest}, advance)

	if got := rodCharges(guest, cursor); got != armed {
		t.Fatalf("the correction rewrote the guest's own loadout to %d charges, it authored %d",
			got, armed)
	}
	// And the host's own cursor is still the host's to author: the guest holds
	// whatever the correction carried for it, not a value of its own.
	if !simulatesLocally(guest, cursor) {
		t.Fatal("the guest stopped simulating the cursor it authors")
	}
}

// rodCharges reads one instance's copy of a cursor's rod loadout.
func rodCharges(a *App, cursor core.Entity) (n int) {
	a.World().RunSafe(func() {
		if c, ok := a.World().Components.Weapon.GetComponent(cursor); ok {
			n = c.Charges[component.WeaponRod]
		}
	})
	return n
}

// orbsPerWeapon counts one cursor's orbs by weapon type, from the store rather than
// from any index: the count this pins is the number of entities that exist.
func orbsPerWeapon(a *App, cursor core.Entity) (out [component.WeaponCount]int) {
	a.World().RunSafe(func() {
		w := a.World()
		for _, e := range w.Components.Orb.Entities() {
			orb, ok := w.Components.Orb.GetComponent(e)
			if !ok || orb.OwnerEntity != cursor {
				continue
			}
			if orb.WeaponType >= 0 && orb.WeaponType < component.WeaponCount {
				out[orb.WeaponType]++
			}
		}
	})
	return out
}

// orbCount is every orb in one instance's world, whoever owns it.
func orbCount(a *App) (n int) {
	a.World().RunSafe(func() { n = a.World().Components.Orb.CountEntities() })
	return n
}

// simulatesLocally reports whether an instance still authors a cursor (D-2).
func simulatesLocally(a *App, cursor core.Entity) (ok bool) {
	a.World().RunSafe(func() { ok = a.World().SimulatesLocally(cursor) })
	return ok
}

// The selective suite drives whole sessions. What the manifest suite proves about
// the index, these prove about the exchange: which messages actually left, how many
// bytes they were, and that every refusal ends at the keyframe the host was going
// to send anyway.

// selectivePair is a two-participant session with the roster split, ready to
// exchange corrections.
func selectivePair(t *testing.T, seed uint64) (host, guest *App, advance func()) {
	t.Helper()
	host, guest = pair(t, seed, 0)
	mirrorCursors(t, host, guest)
	advance = func() { host.Tick(1); guest.Tick(1) }
	return host, guest, advance
}

// deliverSameTick publishes a correction and settles the whole exchange without
// advancing either participant.
//
// It is the only shape that can assert what was *not* sent. Every tick moves the
// clock-derived half of the compared surface — a region's time in state, the gold
// deadline — so a comparison across one is a comparison of two different instants
// and will always find something. Holding the clock still is what makes "the guest
// already agreed, so nothing travelled" a statement about the protocol rather than
// about the tick it straddled.
func deliverSameTick(t *testing.T, host *App, guests []*App) []string {
	t.Helper()
	return deliverCorrectionNow(t, host, guests, func() {})
}

// divergeGuest perturbs one shared cell on a guest and nowhere else, which is the
// disagreement a repair exists to close.
//
// A crossing would not do: the producer applies it immediately and the authority
// applies it a playout lead later, so the two converge on their own. This is a
// difference the session has no artifact for, which is what a lost frame or a
// mispredicted step actually leaves behind.
func divergeGuest(t *testing.T, guest *App) {
	t.Helper()
	moved := false
	guest.World().RunSafe(func() {
		w := guest.World()
		// The mirror of a cursor this instance does not drive: shared placement,
		// not owner-authored, and present from the first tick of any session.
		e := w.Resources.Player.Slot(0)
		if e == 0 {
			return
		}
		pos, ok := w.Positions.GetPosition(e)
		if !ok {
			return
		}
		pos.X += 3
		w.Positions.SetPosition(e, pos)
		moved = true
	})
	if !moved {
		t.Skip("the fixture session holds no mirrored cursor to perturb")
	}
}

// TestAConvergedGuestReceivesTheIndexAndNoState as a session: a
// guest whose prediction was right gets hashes and nothing else.
func TestAConvergedGuestReceivesTheIndexAndNoState(t *testing.T) {
	t.Parallel()
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)

	// The first correction is a keyframe — a guest holds no authority yet — and it
	// leaves the two holding one world at one tick. The claim is about the ones
	// after that.
	deliverCorrection(t, host, []*App{guest}, advance)
	shardBytes := statOf(host, "snapshot.shard_bytes_sent")
	bodyBytes := statOf(host, "snapshot.correction_bytes_sent")
	manifestBytes := statOf(host, "snapshot.manifest_bytes_sent")
	hashOnly := statOf(guest, "snapshot.corrections_hash_only")

	for range 4 {
		// One tick each, from one world: the two simulate the same thing, so the
		// index is the whole of what the correction has to carry.
		advance()
		want := deliverSameTick(t, host, []*App{guest})
		assertCorrected(t, want, guest, "guest")
	}

	if got := statOf(guest, "snapshot.corrections_hash_only") - hashOnly; got == 0 {
		t.Fatal("a converged guest recorded no hash-only correction")
	}
	if got := statOf(host, "snapshot.shard_bytes_sent") - shardBytes; got != 0 {
		t.Fatalf("a converged guest was sent %d bytes of state", got)
	}
	if got := statOf(host, "snapshot.correction_bytes_sent") - bodyBytes; got != 0 {
		t.Fatalf("a converged guest was sent %d bytes of correction body", got)
	}
	index := statOf(host, "snapshot.manifest_bytes_sent") - manifestBytes
	if index == 0 {
		t.Fatal("no index was published at all")
	}
	t.Logf("four converged corrections: %d index bytes, no state", index)
}

// TestASelectiveRepairRestoresTheAuthority is the ordinary case: the two
// participants drive their own cursors, so they disagree, and the repair closes it.
func TestASelectiveRepairRestoresTheAuthority(t *testing.T) {
	t.Parallel()
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)
	deliverCorrection(t, host, []*App{guest}, advance)

	repaired := false
	for range 5 {
		inject(t, host, intentMotion(input.MotionRight, 1))
		inject(t, guest, intentMotion(input.MotionLeft, 1))
		for range 3 {
			advance()
		}
		deliverCorrection(t, host, []*App{guest}, advance)
		advance()
		divergeGuest(t, guest)
		before := statOf(guest, "snapshot.pages_repaired")
		want := deliverSameTick(t, host, []*App{guest})
		assertCorrected(t, want, guest, "guest")
		if statOf(guest, "snapshot.pages_repaired") > before {
			repaired = true
		}
	}
	if !repaired {
		t.Fatal("two participants driving apart never needed a page repaired")
	}
	if got := statOf(guest, "snapshot.proof_failures"); got != 0 {
		t.Fatalf("%d repairs failed their proof on a healthy link", got)
	}
	if got := statOf(guest, "snapshot.keyframe_fallbacks"); got != 0 {
		t.Fatalf("a healthy link fell back to a keyframe %d times", got)
	}
	if got := statOf(guest, "snapshot.shards_refused"); got != 0 {
		t.Fatalf("%d repairs were refused on a healthy link", got)
	}
}

// TestOwnerAuthoredDisagreementNeverDegradesToKeyframes's second
// half and the failure the plan names by hand: a guest keeps its own energy, heat
// and loadout over the host's mirror for the cursor it drives, so those cells
// disagree for the life of the session. A hashed surface that carried them would
// produce a root mismatch no repair could close, and the protocol would fall back
// to a whole world every correction, forever.
func TestOwnerAuthoredDisagreementNeverDegradesToKeyframes(t *testing.T) {
	t.Parallel()
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)
	deliverCorrection(t, host, []*App{guest}, advance)

	var guestCursor core.Entity
	guest.World().RunSafe(func() { guestCursor = guest.World().Resources.Player.Slot(1) })
	if guestCursor == 0 {
		t.Fatal("the guest drives no cursor")
	}

	// Force a standing disagreement: the guest's own cursor holds values the host's
	// mirror does not, which is exactly what a sync period behind looks like.
	setOwnerAuthored := func(a *App, e core.Entity, energy int64, heat int) {
		a.World().RunSafe(func() {
			w := a.World()
			if c, ok := w.Components.Energy.GetPtr(e); ok {
				c.Current = energy
			}
			if c, ok := w.Components.Heat.GetPtr(e); ok {
				c.Current = heat
			}
		})
	}

	fallbacks := statOf(guest, "snapshot.keyframe_fallbacks")
	for round := range 5 {
		setOwnerAuthored(guest, guestCursor, int64(40+round), 10+round)
		setOwnerAuthored(host, guestCursor, 90, 90)
		want := deliverCorrection(t, host, []*App{guest}, advance)
		assertCorrected(t, want, guest, "guest")

		var energy int64
		guest.World().RunSafe(func() {
			if c, ok := guest.World().Components.Energy.GetComponent(guestCursor); ok {
				energy = c.Current
			}
		})
		if energy == 90 {
			t.Fatalf("round %d: the correction adopted the host's mirror of a cursor the guest authors", round)
		}
	}
	if got := statOf(guest, "snapshot.keyframe_fallbacks") - fallbacks; got != 0 {
		t.Fatalf("a standing owner-authored disagreement drove %d keyframe fallbacks", got)
	}
	if got := statOf(guest, "snapshot.proof_failures"); got != 0 {
		t.Fatalf("a standing owner-authored disagreement drove %d proof failures", got)
	}
}

// TestASilentPeerIsSentWholeBodies is the fallback that keeps a peer which cannot
// answer the index from starving: after SnapshotManifestSilenceCorrections
// unanswered manifests the host publishes the whole body again.
func TestASilentPeerIsSentWholeBodies(t *testing.T) {
	t.Parallel()
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)
	deliverCorrection(t, host, []*App{guest}, advance)

	// The guest stops answering: its ticks still run, but nothing drains or replies
	// to the index. Publishing is driven, so each round is one manifest.
	for range parameter.SnapshotManifestSilenceCorrections + 1 {
		if err := host.PublishCorrection(); err != nil {
			t.Fatalf("publish: %v", err)
		}
		host.Tick(1)
	}
	report := host.SelectiveReport()
	peer, ok := report.PeerState[2]
	if !ok {
		t.Fatalf("the host holds no standing for participant 2: %+v", report.PeerState)
	}
	if peer.Silence < parameter.SnapshotManifestSilenceCorrections {
		t.Fatalf("a peer that answered nothing recorded %d unanswered manifests", peer.Silence)
	}

	// The next publish carries a whole body again, and the guest — still ticking —
	// takes it through the ordinary correction path.
	before := statOf(host, "snapshot.correction_bytes_sent")
	if err := host.PublishCorrection(); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if got := statOf(host, "snapshot.correction_bytes_sent"); got == before {
		t.Fatal("a silent peer was left with an index it could not answer")
	}
	applied := statOf(guest, "snapshot.corrections_applied")
	for range parameter.NetworkRelayHopLimit {
		advance()
		guest.ApplyPendingCorrections()
		if statOf(guest, "snapshot.corrections_applied") > applied {
			return
		}
	}
	t.Fatal("the fallback body never reached the guest")
}

// TestLinkPacingPricesTheSelectiveWire: the controller's cost
// model is fed the bytes the new protocol actually sends, not the whole delta it
// replaced.
func TestLinkPacingPricesTheSelectiveWire(t *testing.T) {
	t.Parallel()
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)
	for range 5 {
		deliverCorrection(t, host, []*App{guest}, advance)
	}

	report := host.CadenceReport()
	measured := statOf(host, "snapshot.selective_bytes")
	if measured == 0 {
		t.Fatal("the selective wire size was never measured")
	}
	if report.DeltaBytes != measured {
		t.Fatalf("the controller prices a non-keyframe correction at %d bytes, the wire measured %d",
			report.DeltaBytes, measured)
	}
	if report.KeyframeBytes <= report.DeltaBytes {
		t.Fatalf("a keyframe (%d bytes) is priced no higher than a selective correction (%d)",
			report.KeyframeBytes, report.DeltaBytes)
	}
	// Admission reads the same pair, so a link is judged against what the session
	// will actually put on it.
	sizes := host.cadenceSizes()
	if sizes.Delta != measured || sizes.Keyframe != report.KeyframeBytes {
		t.Fatalf("admission prices %+v, the report says keyframe %d delta %d",
			sizes, report.KeyframeBytes, report.DeltaBytes)
	}
	t.Logf("priced from the wire: keyframe %d bytes, selective correction %d bytes",
		sizes.Keyframe, sizes.Delta)
}

// TestAFailedProofReachesTheKeyframeFallback is the session half: a
// repair that does not verify is refused without touching the world, the guest asks
// for a whole world instead, and the world it gets converges it.
func TestAFailedProofReachesTheKeyframeFallback(t *testing.T) {
	t.Parallel()
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)
	deliverCorrection(t, host, []*App{guest}, advance)
	advance()
	divergeGuest(t, guest)

	corrupt, awaiting := outstandingRepair(t, host, guest, func(set *snapshot.CorrectionShardSet) {
		set.Shards[0].Hash++
	})
	_ = awaiting

	before := statOf(guest, "snapshot.proof_failures")
	guest.corrections.applyRepair(corrupt)
	if statOf(guest, "snapshot.proof_failures") <= before {
		t.Fatal("a corrupted repair passed its proof")
	}
	if got := statOf(guest, "snapshot.keyframe_fallbacks"); got == 0 {
		t.Fatal("a failed proof did not reach the keyframe fallback")
	}
	if got := statOf(guest, "snapshot.corrections_applied"); got != 1 {
		t.Fatalf("a refused repair was installed anyway (%d corrections applied)", got)
	}

	// The fallback resolves it: the next correction converges the guest whole, and
	// it does so through the keyframe path rather than by repairing.
	want := deliverSameTick(t, host, []*App{guest})
	assertCorrected(t, want, guest, "guest")
}

// TestSupersededRepairsAreRefusedRatherThanCombined is the other half of
// a repair that answers a baseline the receiver has moved past is
// refused, so two of them can never be spliced into one world.
func TestSupersededRepairsAreRefusedRatherThanCombined(t *testing.T) {
	t.Parallel()
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)
	deliverCorrection(t, host, []*App{guest}, advance)
	advance()
	divergeGuest(t, guest)

	stale, staleTick := outstandingRepair(t, host, guest, nil)

	// A newer manifest supersedes what the guest was awaiting, and the guest is now
	// waiting on a repair for a later baseline. The held one answers a state this
	// instance has moved past.
	advance()
	divergeGuest(t, guest)
	_, freshTick := outstandingRepair(t, host, guest, nil)
	if freshTick <= staleTick {
		t.Fatalf("the second round named tick %d, not later than %d", freshTick, staleTick)
	}

	before := statOf(guest, "snapshot.shards_refused")
	baselines := statOf(guest, "snapshot.baseline_refusals")
	applied := statOf(guest, "snapshot.corrections_applied")
	guest.corrections.applyRepair(stale)
	if statOf(guest, "snapshot.shards_refused") <= before {
		t.Fatal("a superseded repair was accepted")
	}
	if got := statOf(guest, "snapshot.corrections_applied"); got != applied {
		t.Fatalf("a superseded repair was installed anyway (%d corrections applied, was %d)",
			got, applied)
	}
	if statOf(guest, "snapshot.baseline_refusals") <= baselines {
		t.Fatal("a superseded repair was refused for some reason other than its baseline")
	}
	if got := statOf(guest, "snapshot.proof_failures"); got != 0 {
		t.Fatalf("a superseded repair was spliced and then failed its root %d times", got)
	}

	// And the session recovers on its own: the fresh baseline is still outstanding,
	// so the next round converges the guest.
	advance()
	want := deliverSameTick(t, host, []*App{guest})
	assertCorrected(t, want, guest, "guest")
}

// outstandingRepair drives the exchange far enough for the guest to be awaiting a
// repair, then builds the repair the host would have sent — optionally corrupted —
// without letting the guest apply it.
//
// The interception is white-box on purpose. A repair reaches the receiver's queue
// and is applied in the same drain, so there is no moment in the live path where a
// test could reach in and change one; building the same message from the same two
// indexes is the only way to ask what the receiver does with a bad one.
func outstandingRepair(t *testing.T, host, guest *App, corrupt func(*snapshot.CorrectionShardSet)) ([]byte, uint64) {
	t.Helper()
	if err := host.PublishCorrection(); err != nil {
		t.Fatalf("publish: %v", err)
	}
	host.ApplyPendingCorrections()
	guest.ApplyPendingCorrections() // answers the index, now awaiting a repair

	guest.corrections.selectiveMu.Lock()
	outstanding := guest.corrections.selective.awaiting
	guest.corrections.selectiveMu.Unlock()
	if len(outstanding) == 0 {
		t.Fatal("the guest is not awaiting a repair; the injected divergence produced none")
	}
	awaiting := outstanding[len(outstanding)-1]
	req, _, _ := snapshot.CompareRequest(awaiting.index, awaiting.manifest)
	if req.Converged() {
		t.Fatal("the guest reported convergence; the injected divergence produced no request")
	}

	host.corrections.publishMu.Lock()
	held, ok := host.corrections.retainedAtLocked(awaiting.tick)
	host.corrections.publishMu.Unlock()
	if !ok {
		t.Fatalf("the host retained no capture for tick %d", awaiting.tick)
	}
	set, pages, err := snapshot.BuildShardSet(held.index, req)
	if err != nil {
		t.Fatalf("build repair: %v", err)
	}
	if pages == 0 {
		t.Fatal("the request asked for no page")
	}
	if corrupt != nil {
		corrupt(&set)
	}
	body, err := snapshot.EncodeShardSet(set)
	if err != nil {
		t.Fatalf("encode repair: %v", err)
	}
	return body, awaiting.tick
}

// TestSelectiveCorrectionKeepsThePlayerDomainUntouched seen from
// the world rather than from the index: a selective apply may not create, destroy
// or move a player-domain entity.
func TestSelectiveCorrectionKeepsThePlayerDomainUntouched(t *testing.T) {
	t.Parallel()
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)
	deliverCorrection(t, host, []*App{guest}, advance)

	playerEntities := func(a *App) map[core.Entity]component.PositionComponent {
		out := map[core.Entity]component.PositionComponent{}
		a.World().RunSafe(func() {
			for _, e := range a.World().Positions.Entities() {
				if e.Domain() == core.DomainPlayer {
					pos, _ := a.World().Positions.GetPosition(e)
					out[e] = pos
				}
			}
		})
		return out
	}

	for range 4 {
		inject(t, host, intentMotion(input.MotionRight, 1))
		inject(t, guest, intentMotion(input.MotionLeft, 1))
		for range 3 {
			advance()
		}
		deliverCorrection(t, host, []*App{guest}, advance)
		advance()
		divergeGuest(t, guest)
		before := playerEntities(guest)
		want := deliverSameTick(t, host, []*App{guest})
		assertCorrected(t, want, guest, "guest")
		after := playerEntities(guest)
		if len(before) != len(after) {
			t.Fatalf("a correction changed the player-domain population from %d to %d",
				len(before), len(after))
		}
		for e, pos := range before {
			got, ok := after[e]
			if !ok {
				t.Fatalf("a correction destroyed player-domain entity %d", uint64(e))
			}
			if got != pos {
				t.Fatalf("a correction moved player-domain entity %d from %v to %v",
					uint64(e), pos, got)
			}
		}
	}
}
