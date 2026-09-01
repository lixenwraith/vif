package app

import (
	"strings"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// correctionSteps is how often the two-participant criteria assert convergence.
//
// Weakened D-11 does not say two instances agree at every tick — Phase 4 removed
// the playout lead from the local path, so each participant's own artifacts land a
// lead earlier on it than on anyone else, by design. It says a guest is equal to the
// host as of the last applied correction. That is what these tests assert, at the
// only moments the rule makes a claim about.
const correctionSteps = 8

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
// participants agree on it is the crossing that changed it, and under Phase 4 that
// crossing lands a playout lead earlier on its producer than on anyone else.
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
	for range parameter.NetworkRelayHopLimit {
		advance()
		done := true
		for i, g := range guests {
			g.ApplyPendingCorrections()
			if statOf(g, "snapshot.corrections_applied") == before[i] {
				done = false
			}
		}
		if done {
			return want
		}
	}
	t.Fatalf("a correction for tick %d never reached every guest", tick)
	return nil
}

// assertCorrected fails with the first record a guest does not hold as the host
// held it when the correction was read.
func assertCorrected(t *testing.T, want []string, guest *App, label string) {
	t.Helper()
	got := guest.SnapshotShared()
	if idx, lw, lg, ok := FirstDiff(want, got); ok {
		t.Fatalf("%s did not converge on the correction, line %d\n  host:  %s\n  guest: %s\n%s",
			label, idx, lw, lg, strings.Join(Diff(want, got, 8), "\n"))
	}
}

// TestGuestConvergesOnEveryCorrection is Phase 4's headline criterion and the
// replacement for the lockstep one.
//
// Before this phase two participants re-derived the shared world from one artifact
// stream and were asserted equal at every tick. That is what a guest stopped doing:
// it applies its own input immediately and extrapolates between corrections, so it
// is *expected* to differ from the host, and what the rule now claims is that every
// correction closes the difference exactly. The magnitude in between is telemetry —
// it is asserted to be non-zero here, because a criterion that passed with a guest
// that never predicted anything would be proving nothing.
func TestGuestConvergesOnEveryCorrection(t *testing.T) {
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
	_, _, _, differs := FirstDiff(a.SnapshotShared(), b.SnapshotShared())
	return !differs
}

// TestCorrectionMagnitudeIsMeasuredNotAsserted pins the number that replaced
// DESYNC: how far this instance's prediction had drifted when the authority
// arrived, published rather than escalated.
func TestCorrectionMagnitudeIsMeasuredNotAsserted(t *testing.T) {
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

		delta := DiffCapture(base, next)
		rebuilt, err := ApplyCaptureDelta(base, delta)
		if err != nil {
			t.Fatalf("seed %#x apply delta: %v", seed, err)
		}
		wantBytes, err := EncodeCapture(next)
		if err != nil {
			t.Fatalf("seed %#x encode: %v", seed, err)
		}
		gotBytes, err := EncodeCapture(rebuilt)
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
		deltaBytes, err := EncodeCorrectionDelta(delta)
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

	delta := DiffCapture(base, next)
	if _, err := ApplyCaptureDelta(mid, delta); err == nil {
		t.Fatal("a delta was applied to a baseline it does not name")
	}

	// And a delta whose baseline tick is right but whose body is not: the tick
	// check passes and the integrity hash is what refuses it.
	forged := DiffCapture(base, next)
	forged.BaselineTick = mid.Header.Tick
	mid.Header.Tick = base.Header.Tick
	if _, err := ApplyCaptureDelta(mid, forged); err == nil {
		t.Fatal("a delta rebuilt a body its header does not describe and was accepted")
	}
}

// TestStagingWorldIsBuiltOnceAndReused is doorstep item 1. Phase 3 built a whole
// second App per install — 9 to 31 ms — which is right for a join and wrong for a
// correction at cadence.
//
// Re-use is only sound if the second install leaves exactly what a world built for
// it alone would: a carrier that merged rather than replaced, or a store that kept
// an entity the next capture does not have, would resolve the following correction
// against a world the sender never held.
func TestStagingWorldIsBuiltOnceAndReused(t *testing.T) {
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

	if idx, lw, lg, ok := FirstDiff(want, got); ok {
		t.Fatalf("a re-used staging world differs from a fresh one at line %d\n  fresh:  %s\n  reused: %s\n%s",
			idx, lw, lg, strings.Join(Diff(want, got, 8), "\n"))
	}
}

// TestReconcileMatchesAFullInstall is what lets a commit stop being a second full
// write. A correction moves the live world onto the capture instead of clearing and
// re-inserting it, and the only thing that makes that safe is that the two produce
// the same world — including the components an entity stopped carrying and the
// entities the authority no longer has at all.
func TestReconcileMatchesAFullInstall(t *testing.T) {
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
	if idx, lw, lg, ok := FirstDiff(want, got); ok {
		t.Fatalf("reconcile differs from a full install at line %d\n  install:   %s\n  reconcile: %s\n%s",
			idx, lw, lg, strings.Join(Diff(want, got, 8), "\n"))
	}

	// And the futures agree, which is the claim the digest alone cannot make.
	for range 60 {
		replaced.Tick(1)
		moved.Tick(1)
	}
	want, got = replaced.SnapshotShared(), moved.SnapshotShared()
	if idx, lw, lg, ok := FirstDiff(want, got); ok {
		t.Fatalf("reconciled world diverged 60 ticks later at line %d\n  install:   %s\n  reconcile: %s",
			idx, lw, lg)
	}
}

// TestLocalCrossingSkipsThePlayoutLead is requirement 5. The producer applies its
// own artifact in the tick it produced it for; the peers keep the lead, which is an
// interpolation buffer for remote action rather than a barrier on anyone's input.
func TestLocalCrossingSkipsThePlayoutLead(t *testing.T) {
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
}

// TestRosterCrossingsKeepTheAgreedApplyTick is the exception requirement 5 does not
// reach. An arrival creates a shared cursor and a departure destroys one, and a
// shared entity's identity and creation order are what every capture references by
// — so those apply at one agreed tick on the producer too.
func TestRosterCrossingsKeepTheAgreedApplyTick(t *testing.T) {
	a, b := pair(t, 0x5EEDBEEF, 0)
	mirrorCursors(t, a, b)

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

// TestHostRefusesARosterCrossingFromAnyoneElse is requirement 2's validation. The
// coordinator is the only producer of an arrival or a departure, because one
// producer is what gives them a single apply tick; an artifact of either kind from
// anyone else would create or destroy a shared entity the session never agreed to.
func TestHostRefusesARosterCrossingFromAnyoneElse(t *testing.T) {
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

// TestSessionLagIsMeasuredEveryTick is doorstep item 3. Phase 3 measured the gap a
// participant may be behind exactly once, at admission, and refused a join it could
// not close — after which nothing looked at it again, so a guest whose machine fell
// behind mid-session produced artifacts that reached the host after the ticks they
// named and had no way to know.
func TestSessionLagIsMeasuredEveryTick(t *testing.T) {
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

// statBoolOf reads a boolean counter as an integer, so the numeric helpers above
// can assert both kinds the same way.
func statBoolOf(a *App, key string) (v bool) {
	a.World().RunSafe(func() { v = a.World().Resources.Status.Bools.Get(key).Load() })
	return v
}

// TestCorrectionCarriesTheWholeDeclaredSurface guards the one shortcut the delta
// takes: only the world half is differenced, and everything else — stream
// positions, every D-19 carrier, the FSM's runtime position, the compared status
// surface — travels whole in both shapes.
func TestCorrectionCarriesTheWholeDeclaredSurface(t *testing.T) {
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
	delta := DiffCapture(base, next)

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
