package app

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/snapshot"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// correctionSteps is how often the two-participant criteria assert convergence.
// Weakened D-11 does not claim agreement at every tick — each participant's own
// artifacts land a playout lead earlier on it — but equality as of the last applied
// correction, which is the only moment the rule makes a claim about.
const correctionSteps = 8

// The selective exchange's settling budget between two ticks.
// correctionExchangeFastPasses is what an in-process link needs: manifest, repair,
// and a margin for the keyframe fallback. The passes past it exist for a socket,
// which delivers on its own goroutine; the budget is generous because a loopback
// round trip here competes with every other parallel test on the machine.
const (
	correctionExchangeFastPasses = 3
	correctionExchangePasses     = 250
	correctionExchangePoll       = 200 * time.Microsecond
)

// deliverCorrection publishes one authoritative correction and gets it applied on
// every guest, returning the host's shared state at the instant it was read. It
// drains the playout lead first: not everything SnapshotShared compares is in a
// capture, so a crossing still in flight would read as a guest that had not
// converged rather than as one that had not caught up.
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
	if err := host.corrections.Publish(); err != nil {
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
	// The exchange is driven before the clock is, so a correction over a direct link
	// completes without the participants moving and the comparison is at the tick
	// the correction describes. A tick is advanced only when a round did not
	// complete, which is what a relayed session needs: every hop costs one.
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
		// Each leg is drained, answered and served between two ticks, so the round
		// trip completes without the participants moving. The later passes wait
		// first: an in-process link hands a frame over inside the call that sent it
		// and pays nothing, while a socket delivers on its own goroutine and would
		// otherwise see a tick advance before the manifest crossed loopback.
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

// TestGuestConvergesOnEveryCorrection is the headline criterion. Crossings apply at
// one agreed tick everywhere, so the two disagree only over what one of them derived
// and the other did not; the claim is that every correction closes that difference
// exactly. The magnitude is asserted non-zero, because a guest that never differed
// would pass a convergence criterion while proving nothing.
func TestGuestConvergesOnEveryCorrection(t *testing.T) {
	t.Parallel()
	const seed = 0x5EEDBEEF
	host, guest := pair(t, seed, 0)
	localA, localB := mirrorCursors(t, host, guest)

	advance := func() { host.Tick(1); guest.Tick(1) }
	drifted := false
	for round := range 6 {
		// Each participant drives its own cursor, and the host takes a placement the
		// guest never derives — a stamped shared one, as a wall push-out is.
		inject(t, host, intentMotion(input.MotionRight, 1))
		inject(t, guest, intentMotion(input.MotionLeft, 1))
		from := cursorPosition(host, localA)
		host.Context().PushEventOrigin(event.EventCursorMoveRequest,
			&event.CursorMoveRequestPayload{Entity: localA, X: from.X, Y: from.Y + 1}, event.OriginDebug)
		host.Settle()
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

	// A placement only the host derives — stamped shared, as a wall push-out is —
	// so a correction read after it describes a world the guest does not have.
	var hostCursor core.Entity
	host.World().RunSafe(func() { hostCursor = host.World().Resources.Player.Slot(0) })
	from := cursorPosition(host, hostCursor)
	host.Context().PushEventOrigin(event.EventCursorMoveRequest,
		&event.CursorMoveRequestPayload{Entity: hostCursor, X: from.X + 7, Y: from.Y + 3}, event.OriginDebug)
	host.Settle()

	want := deliverCorrectionNow(t, host, []*App{guest}, advance)
	assertCorrected(t, want, guest, "guest")

	entries := statOf(guest, "snapshot.correction_entries")
	entities := statOf(guest, "snapshot.correction_entities")
	cells := statOf(guest, "snapshot.correction_cells")
	if entries == 0 || entities == 0 {
		t.Fatalf("correction magnitude = %d entries over %d entities, want a drift the "+
			"guest had to be told about", entries, entities)
	}
	if cells == 0 {
		t.Fatal("correction magnitude reports no placement shift, want one that moved")
	}
	// Nothing escalated: the drift is the ordinary condition this phase created.
	if diverged := host.World().Resources.Status.Bools.Has("network.diverged"); diverged {
		t.Fatal("network.diverged is still registered; DESYNC/DIVERGED were retired")
	}
}

// TestCorrectionDeltaRoundTripsExactly is the delta's whole claim: applying it to
// the baseline it names reproduces the sender's capture byte for byte. The integrity
// hash is what makes that "byte for byte" rather than "equivalent" — a delta that
// rebuilt the same entities in a different order would fail only this.
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
// which suits a join and not a correction at cadence. Re-use is sound only if the
// second install leaves what a world built for it alone would: a carrier that merged
// rather than replaced would resolve the next correction against a world nobody had.
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

// TestCrossingApplyTimes is the ordering rule over one pair: a crossing applies at
// one agreed tick, a playout lead after its production, on its producer as on its
// peer. A producer that applied its own a lead early was the one every correction
// inside that lead had to undo.
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

	for range parameter.NetworkBarrierDelayTicks {
		if got := cursorPosition(a, target); got != start {
			t.Fatalf("the producer applied its own crossing at %#v inside the lead", got)
		}
		if got := cursorPosition(b, target); got != start {
			t.Fatalf("the peer applied a crossing before its apply tick: %#v", got)
		}
		a.Tick(1)
		b.Tick(1)
	}
	a.Tick(1)
	b.Tick(1)
	for _, x := range []*App{a, b} {
		if got := cursorPosition(x, target); got != want {
			t.Fatalf("the crossing applied as %#v at the agreed tick, want %#v", got, want)
		}
	}
	if deferred := statOf(a, "network.barrier_deferred"); deferred == 0 {
		t.Fatal("the producer's own crossing was not counted as deferred")
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
// Reading the world once per participant makes a second dialler wait behind the
// first one's read as well as its transfer. A host publishes keyframes anyway, so a
// join takes whichever is fresh enough: the read is per-cadence, not per-join.
func TestJoinReusesTheCadencesKeyframe(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, 0x5EEDBEEF, 120, 40)
	defer a.Close()
	tickUntilCursor(t, a)
	a.Tick(20)

	deadline := time.Now().Add(socketWait)
	first, firstTick, err := a.corrections.KeyframeAt(0, deadline)
	if err != nil {
		t.Fatalf("first keyframe: %v", err)
	}
	second, secondTick, err := a.corrections.KeyframeAt(firstTick, deadline)
	if err != nil {
		t.Fatalf("second keyframe: %v", err)
	}
	if secondTick != firstTick || &second[0] != &first[0] {
		t.Fatalf("a second join at tick %d read the world again instead of taking the keyframe at %d",
			secondTick, firstTick)
	}

	// A join needs a world *later* than its admission, and asking for one this run
	// has not reached is a refusal rather than a stale capture.
	if _, _, err := a.corrections.KeyframeAt(firstTick+4, time.Now().Add(50*time.Millisecond)); err == nil {
		t.Fatal("a keyframe was produced for a tick the session has not reached")
	}

	a.Tick(8)
	third, thirdTick, err := a.corrections.KeyframeAt(firstTick+4, deadline)
	if err != nil {
		t.Fatalf("third keyframe: %v", err)
	}
	if thirdTick <= firstTick {
		t.Fatalf("keyframe tick %d, want one past %d", thirdTick, firstTick)
	}
	if len(third) == 0 {
		t.Fatal("the fresh keyframe is empty")
	}

	// And a restart retires it, whatever its tick says. A reset re-bases the tick
	// counter, so the keyframe above outranks every tick the new run has reached and
	// the reuse test at the top of this function would hand a joiner the previous
	// game — a world carrying the session it was taken in, which that joiner refuses
	// because it is not the session it was offered.
	a.Context().PushEventOrigin(event.EventGameResetRequest,
		&event.GameResetPayload{}, event.OriginDebug)
	a.Settle()
	a.Tick(4)
	if got := a.Position().Run; got == 0 {
		t.Fatal("the reset did not start a new run")
	}
	fresh, freshTick, err := a.corrections.KeyframeAt(0, deadline)
	if err != nil {
		t.Fatalf("keyframe after a restart: %v", err)
	}
	if freshTick >= thirdTick {
		t.Fatalf("the keyframe after a restart is at tick %d, still the previous run's %d",
			freshTick, thirdTick)
	}
	if &fresh[0] == &third[0] {
		t.Fatal("a join after a restart was handed the previous run's world")
	}
}

// TestMidRunJoinWaitsOutThePlayoutLead. D-22 admits a participant before the world
// is read for it, but an epoch flushed just *before* the admission reaches the
// joiner not at all and is in no capture taken at the admission tick either. A join
// therefore asks for a world a playout lead further on, by which point every such
// artifact has applied into it and the copies that arrive are already contained.
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
		_, tick, err := host.corrections.KeyframeAt(
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
// crosses: a player-domain handle carried on a shared component means nothing on the
// receiving instance, so every correction overwrote a guest's live orb handles with
// the host's zeroes and the next tick spawned replacements over the orphans. No
// shared component names a player entity now, which is what this asserts.
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
// boundary: the owner-authored set has one author (D-13), and what a capture holds
// for a cursor the receiver drives is the sender's mirror, a sync period behind. The
// grant is settled and the correction published with no tick in between, so the host
// provably publishes a stale mirror and the guest's own value is the only authored one.
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

// deliverSameTick publishes a correction and settles the exchange without advancing
// either participant. It is the only shape that can assert what was *not* sent:
// every tick moves the clock-derived half of the compared surface, so a comparison
// across one always finds something.
func deliverSameTick(t *testing.T, host *App, guests []*App) []string {
	t.Helper()
	return deliverCorrectionNow(t, host, guests, func() {})
}

// divergeGuest perturbs one shared cell on a guest and nowhere else, which is the
// disagreement a repair exists to close. A crossing would not do — the two converge
// on their own — so this is a difference the session has no artifact for, which is
// what a lost frame or a mispredicted step leaves behind.
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

// TestAManifestAheadOfTheClockIsComparedAtItsTick: a guest that runs past the tick
// an index names before it can answer still answers for that tick, so a converged
// guest is proved converged rather than repaired onto the world it already had.
func TestAManifestAheadOfTheClockIsComparedAtItsTick(t *testing.T) {
	t.Parallel()
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)
	deliverCorrection(t, host, []*App{guest}, advance)
	advance()

	host.Tick(1)
	if err := host.corrections.Publish(); err != nil {
		t.Fatalf("publish correction: %v", err)
	}
	hashOnly := statOf(guest, "snapshot.corrections_hash_only")
	guest.Tick(3) // drains the index a tick early, then runs two past it
	guest.ApplyPendingCorrections()

	if got := statOf(guest, "snapshot.corrections_hash_only") - hashOnly; got != 1 {
		t.Fatalf("the guest recorded %d hash-only corrections for an index it matched at its tick", got)
	}
	if got := statOf(guest, "snapshot.manifests_off_tick"); got != 0 {
		t.Fatalf("%d manifests were compared off their tick", got)
	}
}

// TestAManifestBehindTheClockIsComparedAtThePromisedTick: an index names the tick
// the next one will describe, so a guest already past that tick when the next index
// arrives still answers for it rather than repairing onto the world it had.
func TestAManifestBehindTheClockIsComparedAtThePromisedTick(t *testing.T) {
	t.Parallel()
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)
	deliverCorrection(t, host, []*App{guest}, advance)
	advance()
	deliverSameTick(t, host, []*App{guest}) // an index, and the promise it carries

	cadence := int(statOf(host, "snapshot.cadence_ticks"))
	hashOnly := statOf(guest, "snapshot.corrections_hash_only")
	offTick := statOf(guest, "snapshot.manifests_off_tick")
	host.Tick(cadence)
	guest.Tick(cadence + 2)
	if err := host.corrections.Publish(); err != nil {
		t.Fatalf("publish correction: %v", err)
	}
	guest.ApplyPendingCorrections()

	if got := statOf(guest, "snapshot.corrections_hash_only") - hashOnly; got != 1 {
		t.Fatalf("the guest recorded %d hash-only corrections for an index it matched at the promised tick", got)
	}
	if got := statOf(guest, "snapshot.manifests_off_tick") - offTick; got != 0 {
		t.Fatalf("%d manifests were compared off their tick", got)
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

// TestOwnerAuthoredDisagreementNeverDegradesToKeyframes: a guest keeps its own
// energy, heat and loadout over the host's mirror, so those cells disagree for the
// life of the session. A hashed surface carrying them would produce a root mismatch
// no repair could close, and the protocol would fall back to a whole world forever.
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
		if err := host.corrections.Publish(); err != nil {
			t.Fatalf("publish: %v", err)
		}
		host.Tick(1)
	}
	report := host.corrections.Selective()
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
	if err := host.corrections.Publish(); err != nil {
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

	report := host.corrections.Cadence()
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
	t.Logf("priced from the wire: keyframe %d bytes, selective correction %d bytes",
		report.KeyframeBytes, report.DeltaBytes)
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

// TestCorrectionLeavesPlayerStreamsAlone is D-8 read onto the RNG inventory: a
// capture is the shared world, so it carries the streams driving shared mechanics
// and none a participant draws for itself. Carrying both froze every receiver's
// Player streams at the sender's position — a host spawning no nuggets of its own
// pinned each guest's nugget glyph and cell to wherever its stream last sat.
func TestCorrectionLeavesPlayerStreamsAlone(t *testing.T) {
	t.Parallel()
	const seed = 0x5EEDBEEF
	source := mustHeadless(t, seed, 120, 40)
	defer source.Close()
	tickUntilCursor(t, source)
	source.Tick(40)
	cap, err := source.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	for _, st := range cap.Streams {
		if st.Domain != core.DomainShared {
			t.Fatalf("a %s stream rode a shared capture: %s", core.DomainNames[st.Domain], st.Label)
		}
	}

	receiver := mustHeadless(t, seed, 120, 40)
	defer receiver.Close()
	tickUntilCursor(t, receiver)
	receiver.Tick(140)
	own := receiver.World().Resources.Rand.SaveStreams(core.DomainPlayer)
	if len(own) == 0 {
		t.Fatal("the receiver issued no player streams, so the install proved nothing")
	}
	if err := receiver.InstallShared(cap); err != nil {
		t.Fatalf("install: %v", err)
	}
	if got := receiver.World().Resources.Rand.SaveStreams(core.DomainPlayer); !reflect.DeepEqual(got, own) {
		t.Fatalf("a correction moved the receiver's own streams:\n  before %v\n  after  %v", own, got)
	}
}

// TestACorrectionDoesNotStallTheOwnerStateSync is the D-13 cadence boundary. The
// authority mirrors a guest's cursor from a sync up to NetworkSyncTicks old, so a
// correction can carry a stale mirror to everyone else. That is bounded only because
// an install leaves the sync sequence alone, whatever distance it projects across.
func TestACorrectionDoesNotStallTheOwnerStateSync(t *testing.T) {
	t.Parallel()
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)

	var cursor core.Entity
	guest.World().RunSafe(func() { cursor = guest.World().Resources.Player.Slot(1) })

	syncOnce := func() {
		for range parameter.NetworkSyncTicks + 2 {
			advance()
		}
	}
	arm := func() int {
		guest.Context().PushLocal(event.EventWeaponAddRequest,
			&event.WeaponAddRequestPayload{Entity: cursor, Weapon: component.WeaponRod})
		guest.Settle()
		return rodCharges(guest, cursor)
	}

	first := arm()
	syncOnce()
	if got := rodCharges(host, cursor); got != first {
		t.Fatalf("host mirror = %d charges after a sync period, the guest authored %d", got, first)
	}

	// Run the guest several sync periods past the host, so the correction it installs
	// describes a tick well behind it and is projected across the gap.
	for range 4 * parameter.NetworkSyncTicks {
		guest.Tick(1)
	}
	ahead := guest.Position().Tick
	deliverCorrectionNow(t, host, []*App{guest}, advance)
	if got := guest.Position().Tick; got < ahead {
		t.Fatalf("guest tick %d rewound from %d; a correction behind the clock is projected", got, ahead)
	}

	second := arm()
	if second == first {
		t.Fatal("the second grant did not change the guest's loadout")
	}
	syncOnce()
	if got := rodCharges(host, cursor); got != second {
		t.Fatalf("host mirror stalled at %d charges after an install rewound the guest, it authored %d",
			got, second)
	}
}

// TestATowerGuestAnswersHashOnly: in the tower region both participants fight, so
// owner-authored cursor state moves every tick, snakes rewrite their segments in
// place and overlays toggle. None of it may reach the compared world, or a guest
// there repairs nearly every manifest instead of proving the authority's world.
func TestATowerGuestAnswersHashOnly(t *testing.T) {
	t.Parallel()
	apps := meshSessionOf(t, towerScenario(t, 0x70E4), 2, [][2]int{{1, 2}})
	host, guest := apps[0], apps[1]
	const lead = 2
	for _, a := range apps {
		transportOf(t, a).SetShape(network.LinkShape{LatencyTicks: lead})
		injectExCommand(t, a, "god")
	}
	// A live session refuses an operator's region change, so both instances take it
	// at one tick, as a shared FSM re-derives it.
	for _, a := range apps {
		a.World().RunSafe(func() {
			a.World().PushEventDomain(event.EventFSMRegionRequest,
				&event.FSMRegionPayload{Op: event.RegionSpawn, Region: "tower", State: "TowerSetup"},
				core.DomainShared)
		})
		a.Settle()
	}
	for range 120 {
		tickAll(apps)
	}
	var state string
	guest.World().RunSafe(func() { state = guest.World().Resources.Status.Strings.Get("fsm.tower.state").Load() })
	if state == "" || state == "-" {
		t.Fatal("the tower region never started")
	}
	// The guest trails the authority by the link, which is where pacing holds it.
	for range lead {
		host.Tick(1)
		_ = host.corrections.PublishDue()
	}

	rng := vmath.NewFastRand(7)
	motions := []input.MotionOp{input.MotionLeft, input.MotionRight, input.MotionUp, input.MotionDown}
	for range 1200 {
		for _, a := range apps {
			if rng.Intn(3) == 0 {
				inject(t, a, intentMotion(motions[rng.Intn(4)], 1+rng.Intn(3)))
			}
			if rng.Intn(6) == 0 {
				inject(t, a, &input.Intent{Type: input.IntentFireMain, Count: 1})
			}
		}
		host.Tick(1)
		_ = host.corrections.PublishDue()
		guest.Tick(1)
		guest.ApplyPendingCorrections()
	}

	manifests := statOf(guest, "snapshot.manifests_received")
	hashOnly := statOf(guest, "snapshot.corrections_hash_only")
	if manifests == 0 || hashOnly*100 < manifests*95 {
		t.Fatalf("the guest answered %d of %d manifests hash-only, want 95%%", hashOnly, manifests)
	}
}

// TestACorrectionThatMovesNothingWritesNothing: a correction the live world already
// equals on the compared surface is taken as a hash-only answer is, so the mirror of
// a cursor another participant owns keeps what its owner's sync last wrote there.
func TestACorrectionThatMovesNothingWritesNothing(t *testing.T) {
	t.Parallel()
	host, apps := liveInstance(t, 0x0A12)
	guest := apps[1]

	var mirror core.Entity
	var want int64
	guest.World().RunSafe(func() {
		w := guest.World()
		mirror = w.Resources.Player.Slot(0)
		if e, ok := w.Components.Energy.GetPtr(mirror); ok {
			e.Current += 1234
			want = e.Current
		}
	})
	if want == 0 {
		t.Fatal("the host's cursor carries no energy mirror on the guest")
	}
	applied := statOf(guest, "snapshot.corrections_applied")
	deliverCorrectionNow(t, host, apps[1:], func() { tickAll(apps) })
	if statOf(guest, "snapshot.corrections_applied") == applied || statOf(guest, "snapshot.corrections_hash_only") > 0 {
		t.Fatal("the guest installed no whole correction; nothing was asked of the write")
	}
	if n := statOf(guest, "snapshot.correction_entries"); n != 0 {
		t.Fatalf("the correction moved %d entries; the world was meant to equal it", n)
	}
	var got int64
	guest.World().RunSafe(func() {
		if e, ok := guest.World().Components.Energy.GetComponent(mirror); ok {
			got = e.Current
		}
	})
	if got != want {
		t.Fatalf("the mirror reads %d after a correction that moved nothing, its owner's sync wrote %d", got, want)
	}
}

// TestAnInstallMeasuresNoLocalCursorState: every instance re-derives its cursors'
// control and keeps the owner-authored cells the sync stream carries, so a
// correction measures the shared state it moved and nothing of those.
func TestAnInstallMeasuresNoLocalCursorState(t *testing.T) {
	t.Parallel()
	host, apps := liveInstance(t, 0x0A11)
	guest := apps[1]
	advance := func() { tickAll(apps) }

	var hostCursor core.Entity
	host.World().RunSafe(func() { hostCursor = host.World().Resources.Player.Slot(0) })
	from := cursorPosition(host, hostCursor)
	host.Context().PushEventOrigin(event.EventCursorMoveRequest,
		&event.CursorMoveRequestPayload{Entity: hostCursor, X: from.X + 7, Y: from.Y + 3}, event.OriginDebug)
	host.Settle()
	guest.World().RunSafe(func() {
		w := guest.World()
		if e, ok := w.Components.Energy.GetPtr(w.Resources.Player.Entity); ok {
			e.Current += 1234
		}
	})

	deliverCorrectionNow(t, host, apps[1:], advance)
	if n := statOf(guest, "snapshot.correction_entities"); n != 1 {
		t.Fatalf("the correction measured %d entities, want the one cursor it moved", n)
	}
}
