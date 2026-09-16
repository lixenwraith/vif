package app

import (
	"cmp"
	"slices"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// guestParticipant is the identity every fixture in this file gives its guest: the
// coordinator takes 1 and the first joiner takes 2.
const guestParticipant network.PeerID = 2

// The seam between two claims that pull opposite ways: a correction makes a guest
// hold the authority's world, and its own accepted actions must not disappear when
// one arrives. An install finds a crossing either pending on the barrier, which it
// leaves alone, or applied after the capture's tick, which the projection
// re-derives; the capture's fence for this source decides which.

// TestALocalCrossingPendingAtTheBaselineAppliesExactlyOnce: a guest produces a
// crossing, installs an authority taken before it, and the effect lands at the
// agreed tick on both — once, with the prediction that answered the press intact
// across the install.
func TestALocalCrossingPendingAtTheBaselineAppliesExactlyOnce(t *testing.T) {
	t.Parallel()
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)
	deliverCorrection(t, host, []*App{guest}, advance)

	// The authority is read here, before the guest acts, at a tick the guest has
	// not already installed.
	advance()
	if err := host.corrections.Publish(); err != nil {
		t.Fatalf("publish: %v", err)
	}
	at := guest.Position().Tick

	before := cursorCell(t, guest, 1)
	inject(t, guest, intentMotion(input.MotionRight, 4))
	predicted, _ := localCell(guest)
	if predicted.X != before.X+4 || predicted.Y != before.Y {
		t.Fatalf("four presses predicted %v, want four cells right of %v", predicted, before)
	}
	if got := cursorCell(t, guest, 1); got != before {
		t.Fatalf("the producer applied its own crossing at %v inside the lead", got)
	}
	// Pending is not missing: the fence offers nothing to project.
	if suffix, _ := replaySuffixOf(t, guest, hostFence(t, host, guestParticipant)); len(suffix) != 0 {
		t.Fatalf("%d crossings still pending on the barrier were offered for projection", len(suffix))
	}

	applied := statOf(guest, "snapshot.corrections_applied")
	for range parameter.NetworkRelayHopLimit {
		host.ApplyPendingCorrections()
		guest.ApplyPendingCorrections()
		if statOf(guest, "snapshot.corrections_applied") > applied {
			break
		}
		advance()
	}
	if statOf(guest, "snapshot.corrections_applied") <= applied {
		t.Fatal("the correction never reached the guest")
	}
	if got := guest.Position().Tick; got != at {
		t.Fatalf("the correction moved the guest's clock from %d to %d", at, got)
	}
	if got, _ := localCell(guest); got != predicted {
		t.Fatalf("the correction dropped the prediction: local cell %v, want %v", got, predicted)
	}

	// Exactly once, at the agreed tick, on both.
	want := deliverCorrection(t, host, []*App{guest}, advance)
	assertCorrected(t, want, guest, "guest")
	for _, x := range []*App{host, guest} {
		if got := cursorCell(t, x, 1); got != predicted {
			t.Fatalf("after the lead the cursor stands at %v, want %v", got, predicted)
		}
	}
	if got := statOf(guest, "snapshot.replay_records"); got != 0 {
		t.Fatalf("a pending crossing was projected %d times", got)
	}
	if got := statOf(guest, "snapshot.replay_skipped"); got != 0 {
		t.Fatalf("replay was skipped %d times on a healthy suffix", got)
	}
}

// TestAuthorityCrossingFenceWaitsForTheApplyTick keeps the capture watermark tied
// to state rather than to production. A crossing is named and scheduled a playout
// lead before it applies; a capture in that interval must not claim the effect the
// world does not yet hold.
func TestAuthorityCrossingFenceWaitsForTheApplyTick(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 2, [][2]int{{1, 2}})
	localCursors(t, apps)
	host := apps[0]

	before, err := host.CaptureShared()
	if err != nil {
		t.Fatalf("capture before crossing: %v", err)
	}
	cell := cursorCell(t, host, 0)
	var cursor core.Entity
	host.World().RunSafe(func() { cursor = host.World().Resources.Player.Slot(0) })
	host.Context().PushCrossing(event.EventCursorMoveRequest,
		&event.CursorMoveRequestPayload{Entity: cursor, X: cell.X + 1, Y: cell.Y})
	host.Settle()

	scheduled, err := host.CaptureShared()
	if err != nil {
		t.Fatalf("capture with scheduled crossing: %v", err)
	}
	if got := scheduled.Header.Crossings.Seq(1); got != before.Header.Crossings.Seq(1) {
		t.Fatalf("a scheduled crossing advanced the authority's fence from %d to %d before its apply tick",
			before.Header.Crossings.Seq(1), got)
	}
	if got := cursorCell(t, host, 0); got != cell {
		t.Fatalf("a scheduled crossing moved the cursor from %v to %v before its apply tick", cell, got)
	}

	for range parameter.NetworkBarrierDelayTicks + 1 {
		tickAll(apps)
	}
	applied, err := host.CaptureShared()
	if err != nil {
		t.Fatalf("capture after the apply tick: %v", err)
	}
	if got := applied.Header.Crossings.Seq(1); got <= scheduled.Header.Crossings.Seq(1) {
		t.Fatalf("the applied crossing left the authority's fence at %d, want after %d",
			got, scheduled.Header.Crossings.Seq(1))
	}
	if got := cursorCell(t, host, 0); got.X != cell.X+1 || got.Y != cell.Y {
		t.Fatalf("the applied crossing moved the cursor from %v to %v", cell, got)
	}
}

// TestACorrectionBehindTheClockIsProjectedNotRewound is the projection criterion:
// a capture older than the guest's clock is simulated forward over the guest's own
// crossings and written at the present, so the clock never moves backwards, what
// the guest already re-derived is neither torn down nor rebuilt, and the source's
// production epochs stay the monotonic stream the host's replay filter accepts.
func TestACorrectionBehindTheClockIsProjectedNotRewound(t *testing.T) {
	t.Parallel()
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)
	deliverCorrection(t, host, []*App{guest}, advance)

	advance()
	cap := mustCaptureShared(t, host)
	baseline := cap.Header.Tick

	before := cursorCell(t, guest, 1)
	inject(t, guest, intentMotion(input.MotionRight, 1))
	moved := before
	moved.X++
	for range parameter.NetworkBarrierDelayTicks + 1 {
		advance()
	}
	if got := cursorCell(t, guest, 1); got != moved {
		t.Fatalf("the guest stands at %v after the lead, want its own crossing applied at %v", got, moved)
	}
	ahead := guest.Position().Tick
	held := guest.SnapshotShared()

	installCorrection(t, guest, cap)
	if got := guest.Position().Tick; got != ahead {
		t.Fatalf("the correction moved the guest's clock from %d to %d", ahead, got)
	}
	if got, want := statOf(guest, "snapshot.projected_ticks"), int64(ahead-baseline); got != want {
		t.Fatalf("the correction was projected over %d ticks, want %d", got, want)
	}
	if got := cursorCell(t, guest, 1); got != moved {
		t.Fatalf("the projection lost the guest's own crossing: cursor at %v, want %v", got, moved)
	}
	// The projection re-derived the same world the guest already held.
	assertCorrected(t, held, guest, "the projected guest")
	if got := statOf(guest, "snapshot.correction_cells"); got != 0 {
		t.Fatalf("the projection shifted a placement by %d cells", got)
	}

	// The next crossing leaves under an epoch the host has not seen and applies
	// there once.
	inject(t, guest, intentMotion(input.MotionRight, 1))
	moved.X++
	for range parameter.NetworkBarrierDelayTicks + 1 {
		advance()
	}
	if got := cursorCell(t, host, 1); got != moved {
		t.Fatalf("the host discarded the post-install input: cursor at %v, want %v", got, moved)
	}
	want := deliverCorrection(t, host, []*App{guest}, advance)
	assertCorrected(t, want, guest, "guest")
}

// TestAGoldSequenceSurvivesACorrectionWithoutATick: a typed member is the one
// crossing its producer applies at once, so a whole run typed inside one tick is
// retained as a suffix, projected over a correction taken before it, and every
// member is still gone afterwards.
func TestAGoldSequenceSurvivesACorrectionWithoutATick(t *testing.T) {
	t.Parallel()
	host, apps := liveInstance(t, 0x601D)
	guest := apps[1]
	advance := func() { tickAll(apps) }

	// One gold sequence on the authority, carried to the guest by a correction, so
	// the two hold the same run before anything is typed.
	host.Context().PushEventOrigin(event.EventGoldSpawnRequest, nil, event.OriginDebug)
	host.Settle()
	for range 3 {
		advance()
	}
	deliverCorrection(t, host, []*App{guest}, advance)

	run := goldRun(t, guest)
	if len(run) != parameter.GoldSequenceLength {
		t.Fatalf("the guest holds %d gold members, want %d", len(run), parameter.GoldSequenceLength)
	}

	// The authority is read before the run is typed, at a tick the guest has not
	// already installed.
	advance()
	if err := host.corrections.Publish(); err != nil {
		t.Fatalf("publish: %v", err)
	}

	guest.World().RunSafe(func() {
		w := guest.World()
		w.Positions.SetPosition(w.Resources.Player.Entity, run[0].cell)
		w.Resources.Player.DropPrediction()
	})
	inject(t, guest, intentModeSwitch(input.ModeTargetInsert))
	startTick := guest.Position().Tick
	for i, m := range run {
		inject(t, guest, intentTextChar(m.rune))
		if got := guest.Position().Tick; got != startTick {
			t.Fatalf("typing member %d advanced tick %d to %d", i, startTick, got)
		}
		guest.World().RunSafe(func() {
			if guest.World().Components.Glyph.HasEntity(m.entity) {
				t.Fatalf("typed gold member %d remains renderable before a tick", i)
			}
		})
	}

	// The correction describes a world in which the run is still standing. The
	// projection is what keeps it gone.
	replayed := statOf(guest, "snapshot.replay_records")
	for range parameter.NetworkRelayHopLimit {
		host.ApplyPendingCorrections()
		guest.ApplyPendingCorrections()
		if statOf(guest, "snapshot.replay_records") > replayed {
			break
		}
		advance()
	}
	if statOf(guest, "snapshot.replay_records") <= replayed {
		t.Fatal("the correction replayed none of the typed sequence")
	}
	guest.World().RunSafe(func() {
		for i, m := range run {
			if guest.World().Components.Glyph.HasEntity(m.entity) {
				t.Fatalf("gold member %d came back after the correction", i)
			}
		}
	})

	// And the session converges: the host applies the same crossings at the agreed
	// tick, and the next correction finds nothing left to disagree about.
	want := deliverCorrection(t, host, []*App{guest}, advance)
	assertCorrected(t, want, guest, "guest")
}

// TestAnIncompleteSuffixFallsBackToTheAuthority: Retention that
// dropped a record it would have needed offers nothing at all, and the guest
// installs the authority alone and says so.
func TestAnIncompleteSuffixFallsBackToTheAuthority(t *testing.T) {
	t.Parallel()
	host, guest := pair(t, 0x5EEDBEEF, 0)
	mirrorCursors(t, host, guest)
	advance := func() { host.Tick(1); guest.Tick(1) }
	lagHostReceive(t, host, 2*parameter.NetworkBarrierDelayTicks)

	// Past the record bound inside one window, and applied here before the host
	// has seen any of it. Nothing an ordinary session does reaches the bound — it
	// is far wider than a cadence — but a participant that produced faster than
	// retention allows must not be projected from a suffix with a hole in it.
	src := replaySourceOf(t, guest)
	for range parameter.SnapshotReplayRecords + 32 {
		inject(t, guest, intentMotion(input.MotionRight, 1))
		inject(t, guest, intentMotion(input.MotionLeft, 1))
	}
	for range parameter.NetworkBarrierDelayTicks + 1 {
		advance()
	}
	if _, dropped := src.ReplaySuffixSize(); dropped == 0 {
		t.Fatal("retention dropped nothing, so there is no hole to refuse")
	}
	cap := mustCaptureShared(t, host)
	want := host.SnapshotShared()
	if _, _, ok := src.LocalReplaySuffix(cap.Header.Crossings.Seq(guestParticipant)); ok {
		t.Fatal("a suffix with a hole was offered as if it were complete")
	}

	installCorrection(t, guest, cap)
	if got := statOf(guest, "snapshot.replay_skipped"); got != 1 {
		t.Fatalf("an unavailable suffix was reported skipped %d times, want once", got)
	}
	if !statBoolOf(guest, "snapshot.replay_suffix_unavailable") {
		t.Fatal("an unavailable suffix left the indicator clear")
	}
	if got := statOf(guest, "snapshot.replay_records"); got != 0 {
		t.Fatalf("an unavailable suffix projected %d records anyway", got)
	}
	if got := statOf(guest, "snapshot.replay_overflow"); got == 0 {
		t.Fatal("retention overflow was not published")
	}
	// The authority alone, rather than half-applied.
	assertCorrected(t, want, guest, "guest")

	// And the session converges once the link carries the lead again.
	lagHostReceive(t, host, 0)
	want = deliverCorrection(t, host, []*App{guest}, advance)
	assertCorrected(t, want, guest, "guest")
}

// TestRetentionIsBounded pins the three bounds and the overflow they publish.
func TestRetentionIsBounded(t *testing.T) {
	t.Parallel()
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)
	deliverCorrection(t, host, []*App{guest}, advance)

	src := replaySourceOf(t, guest)
	for range parameter.SnapshotReplayRecords + 64 {
		inject(t, guest, intentMotion(input.MotionRight, 1))
		inject(t, guest, intentMotion(input.MotionLeft, 1))
	}
	retained, dropped := src.ReplaySuffixSize()
	if retained > parameter.SnapshotReplayRecords {
		t.Fatalf("retention holds %d records, past the %d-record bound",
			retained, parameter.SnapshotReplayRecords)
	}
	if dropped == 0 {
		t.Fatalf("retention held %d records without dropping any, so no bound was reached", retained)
	}
	t.Logf("retention held %d records and dropped %d", retained, dropped)
}

// === helpers ===

// cursorCell reads one roster slot's placement.
func cursorCell(t *testing.T, a *App, slot uint8) component.PositionComponent {
	t.Helper()
	var out component.PositionComponent
	var ok bool
	a.World().RunSafe(func() {
		w := a.World()
		e := w.Resources.Player.Slot(slot)
		if e == 0 {
			return
		}
		out, ok = w.Positions.GetPosition(e)
	})
	if !ok {
		t.Fatalf("roster slot %d holds no placed cursor", slot)
	}
	return out
}

// replaySourceOf reaches the barrier that retains the suffix.
func replaySourceOf(t *testing.T, a *App) replaySource {
	t.Helper()
	src, _ := a.replaySource()
	if src == nil {
		t.Fatal("this run has no barrier to retain a suffix")
	}
	return src
}

// replaySuffixOf reads what a participant would replay onto one fence.
func replaySuffixOf(t *testing.T, a *App, fence uint64) ([]event.ScheduledWireFrame, int64) {
	t.Helper()
	src := replaySourceOf(t, a)
	frames, _, ok := src.LocalReplaySuffix(fence)
	if !ok {
		t.Fatal("the suffix is unavailable before anything has been dropped")
	}
	_, dropped := src.ReplaySuffixSize()
	return frames, dropped
}

// hostFence is what the authority's world currently holds of one participant's
// ordinary crossings: the boundary a correction taken now would carry, which is
// what the guest measures its own suffix against.
func hostFence(t *testing.T, host *App, participant network.PeerID) uint64 {
	t.Helper()
	cap, err := host.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	return cap.Header.Crossings.Seq(participant)
}

// TestADepartedParticipantLeavesNoCrossingFenceBehind is the same disappearing
// keystroke on a reconnect. An identity returns to the pool when its participant
// leaves and a crossing sequence starts at one, so a fence kept from the previous
// holder claims a captured world already contains crossings the new one has not
// produced — and the install drops its first crossings as if a correction undid them.
func TestADepartedParticipantLeavesNoCrossingFenceBehind(t *testing.T) {
	t.Parallel()
	host, guest := pair(t, 0x5EEDBEEF, 0)
	mirrorCursors(t, host, guest)

	inject(t, guest, intentMotion(input.MotionRight, 3))
	for range parameter.NetworkBarrierDelayTicks + 2 {
		host.Tick(1)
		guest.Tick(1)
	}
	if hostFence(t, host, guestParticipant) == 0 {
		t.Fatal("the host applied nothing from the guest; there is no fence to leave behind")
	}

	// The departure the coordinator produces when the link goes, applied at the tick
	// it names on every instance.
	host.crossDeparture(guestParticipant, 1)
	for range parameter.NetworkBarrierDelayTicks + 2 {
		host.Tick(1)
	}
	if got := hostFence(t, host, guestParticipant); got != 0 {
		t.Fatalf("a capture still claims participant %d's crossings through sequence %d "+
			"after it left; the next holder of that identity starts at 1",
			guestParticipant, got)
	}
}

// goldMember is one member of a gold run, with the cell and rune a typist needs.
type goldMember struct {
	entity core.Entity
	cell   component.PositionComponent
	rune   rune
}

// goldRun reads the standing gold sequence, left to right.
func goldRun(t *testing.T, a *App) []goldMember {
	t.Helper()
	var run []goldMember
	a.World().RunSafe(func() {
		w := a.World()
		for _, headerEntity := range w.Components.Header.GetAllEntities() {
			header, ok := w.Components.Header.GetComponent(headerEntity)
			if !ok || header.Behavior != component.BehaviorGold {
				continue
			}
			for _, entry := range header.MemberEntries {
				glyph, glyphOK := w.Components.Glyph.GetComponent(entry.Entity)
				cell, cellOK := w.Positions.GetPosition(entry.Entity)
				if glyphOK && cellOK {
					run = append(run, goldMember{entity: entry.Entity, cell: cell, rune: glyph.Rune})
				}
			}
			break
		}
	})
	slices.SortFunc(run, func(a, b goldMember) int { return cmp.Compare(a.cell.X, b.cell.X) })
	return run
}

// TestALateGuestActionIsNotUndoneByTheCorrectionThatMissedIt is the regression the
// fence exists for. A guest produces a crossing for T+3 and applies it there; the
// host has not received it at T+9. Judging membership by tick, the guest sees an apply
// tick in the past, concludes the correction holds the action and drops it — undoing
// its own keystroke. The fence asks what the authority actually had of that stream.
func TestALateGuestActionIsNotUndoneByTheCorrectionThatMissedIt(t *testing.T) {
	t.Parallel()
	host, guest := pair(t, 0x5EEDBEEF, 0)
	mirrorCursors(t, host, guest)

	// Delay only what the host receives, by twice the playout lead. The guest's
	// crossing will pass its agreed apply tick before the host has seen it, which is
	// exactly a link that cannot hold the lead.
	lagHostReceive(t, host, 2*parameter.NetworkBarrierDelayTicks)

	before := cursorCell(t, guest, 1)
	inject(t, guest, intentMotion(input.MotionRight, 3))
	moved, _ := localCell(guest)
	if moved.X != before.X+3 || moved.Y != before.Y {
		t.Fatalf("the guest's own motion predicted %v, want three cells right of %v", moved, before)
	}

	// Past the apply tick the crossing named, while the shaped link still holds it.
	for range 2 * parameter.NetworkBarrierDelayTicks {
		host.Tick(1)
		guest.Tick(1)
	}
	if got := cursorCell(t, guest, 1); got != moved {
		t.Fatalf("the guest stands at %v past the apply tick, want %v", got, moved)
	}
	if got := cursorCell(t, host, 1); got != before {
		t.Fatalf("the host already applied the crossing at %v; the link is not lagging", got)
	}

	cap, err := host.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	// The fixture is only the regression if the capture genuinely straddles the
	// crossing: past its apply tick, and behind its sequence.
	fence := cap.Header.Crossings.Seq(guestParticipant)
	suffix, _ := replaySuffixOf(t, guest, fence)
	if len(suffix) == 0 {
		t.Fatal("the guest retained nothing the capture is missing")
	}
	if suffix[0].ApplyTick > cap.Header.Tick {
		t.Fatalf("the retained crossing applies at tick %d, still ahead of the capture at %d; "+
			"this is the ordinary in-flight case, not the late one",
			suffix[0].ApplyTick, cap.Header.Tick)
	}

	installCorrection(t, guest, cap)
	if got := cursorCell(t, guest, 1); got != moved {
		t.Fatalf("the correction undid the guest's own action: cursor at %v, want %v", got, moved)
	}

	// And it survives the ticks that follow, rather than being undone a moment later
	// by the copy the barrier still holds.
	for range parameter.NetworkBarrierDelayTicks + 2 {
		guest.Tick(1)
	}
	if got := cursorCell(t, guest, 1); got != moved {
		t.Fatalf("the guest's cursor drifted to %v after the install, want %v", got, moved)
	}
}

// lagHostReceive delays everything one instance receives, leaving what it sends
// untouched. That asymmetry is the condition: the guest's crossings arrive after the
// ticks they named while the authority's corrections still reach the guest on time.
func lagHostReceive(t *testing.T, a *App, ticks uint64) {
	t.Helper()
	var port engine.NetworkPort
	a.World().RunSafe(func() {
		if r := a.World().Resources.Network; r != nil {
			port = r.Port
		}
	})
	mesh, ok := port.(*network.MeshPort)
	if !ok {
		t.Fatalf("this fixture no longer runs on a mesh port: %T", port)
	}
	mesh.SetShape(network.LinkShape{LatencyTicks: ticks})
}
