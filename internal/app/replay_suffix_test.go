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

// The replay suite is about the seam between two claims that pull in opposite
// directions: a correction makes a guest hold the authority's world, and a
// participant's own accepted actions must not disappear when one arrives.
//
// The window is what reconciles them. Production ticks bound retention; the
// capture's fence for this source decides membership. A capture contains what its
// producer's sequence says it contains — not what the receive schedule says was due
// — because an ordinary crossing applies immediately on its producer and a lead
// later everywhere else, so a copy already past its apply tick can still be missing
// from a capture read before it arrived.

// TestLocalCrossingsAfterTheBaselineSurviveExactlyOnce: A guest
// produces a crossing, then installs an authority taken before it, and the effect
// is present exactly once afterwards.
func TestLocalCrossingsAfterTheBaselineSurviveExactlyOnce(t *testing.T) {
	t.Parallel()
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)
	deliverCorrection(t, host, []*App{guest}, advance)

	// The authority is read here, before the guest acts. It has to describe a tick
	// the guest has not already installed, or the correction is superseded rather
	// than applied.
	advance()
	if err := host.PublishCorrection(); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Now the guest acts, after that baseline, and applies it immediately. No tick
	// separates the two: an artifact produced between two ticks belongs to the next
	// epoch, which is already past the baseline, and advancing here would let the
	// guest consume the correction before it had acted at all.
	before := cursorCell(t, guest, 1)
	inject(t, guest, intentMotion(input.MotionRight, 4))
	moved := cursorCell(t, guest, 1)
	if moved == before {
		t.Fatal("the local motion did not move the cursor at all")
	}
	fence := hostFence(t, host, guestParticipant)
	suffix, dropped := replaySuffixOf(t, guest, fence)
	if len(suffix) == 0 {
		t.Fatal("the guest retained no crossing the authority does not hold")
	}
	if suffix[0].Frame.Seq <= fence {
		t.Fatalf("the retained crossing is sequence %d, at or before the authority's fence %d",
			suffix[0].Frame.Seq, fence)
	}
	if dropped != 0 {
		t.Fatalf("retention dropped %d records in a four-tick window", dropped)
	}

	// The correction arrives and rebases the guest onto the earlier tick. Without
	// the replay the cursor would snap back and the player's own keystrokes would
	// arrive a playout lead later; with it the placement survives.
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
		t.Fatal("the correction did not replay the guest's own crossing")
	}
	if got := cursorCell(t, guest, 1); got != moved {
		t.Fatalf("after the correction the cursor stands at %v, want the placement it was moved to, %v",
			got, moved)
	}

	// Exactly once: the effect is not doubled, and the crossing is not replayed a
	// second time by the correction that finally carries it.
	replayedOnce := statOf(guest, "snapshot.replay_records")
	want := deliverCorrection(t, host, []*App{guest}, advance)
	assertCorrected(t, want, guest, "guest")
	if got := statOf(guest, "snapshot.replay_records"); got != replayedOnce {
		t.Fatalf("the same crossing was replayed again (%d then %d records)", replayedOnce, got)
	}
	if got := cursorCell(t, host, 1); got != moved {
		t.Fatalf("the pending wire copy never reached the authority: host stands at %v, want %v", got, moved)
	}
	if got := cursorCell(t, guest, 1); got != moved {
		t.Fatalf("the authority's next correction lost the pending crossing: guest stands at %v, want %v", got, moved)
	}
	if got := statOf(guest, "snapshot.replay_skipped"); got != 0 {
		t.Fatalf("replay was skipped %d times on a healthy suffix", got)
	}
}

// TestLocalCrossingInFlightAtTheBaselineSurvivesExactlyOnce is the boundary the
// production-tick test above does not cover. The guest has already closed the
// crossing's production epoch, but its authoritative apply tick is still ahead of
// the capture. A correction at that production tick therefore cannot contain the
// crossing and must replay it, even though it was not produced after the baseline.
func TestLocalCrossingInFlightAtTheBaselineSurvivesExactlyOnce(t *testing.T) {
	t.Parallel()
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)
	deliverCorrection(t, host, []*App{guest}, advance)

	before := cursorCell(t, guest, 1)
	inject(t, guest, intentMotion(input.MotionRight, 1))
	moved := cursorCell(t, guest, 1)
	if moved.X != before.X+1 || moved.Y != before.Y {
		t.Fatalf("the local motion moved the cursor from %v to %v", before, moved)
	}

	// Close and send the production epoch. The host still cannot apply the frame
	// until its agreed apply tick, one playout lead later.
	advance()
	baseline := host.Position().Tick
	if got := guest.Position().Tick; got != baseline {
		t.Fatalf("guest tick %d, want the host baseline %d", got, baseline)
	}
	fence := hostFence(t, host, guestParticipant)
	suffix, dropped := replaySuffixOf(t, guest, fence)
	if len(suffix) != 1 {
		t.Fatalf("fence %d offered %d crossings, want the one still in flight", fence, len(suffix))
	}
	if suffix[0].Frame.Seq <= fence {
		t.Fatalf("the in-flight crossing is sequence %d, at or before the fence %d",
			suffix[0].Frame.Seq, fence)
	}
	if dropped != 0 {
		t.Fatalf("retention dropped %d records in a one-crossing window", dropped)
	}

	replayed := statOf(guest, "snapshot.replay_records")
	deliverCorrectionNow(t, host, []*App{guest}, advance)
	if got := statOf(guest, "snapshot.replay_records"); got != replayed+1 {
		t.Fatalf("the correction replayed %d records, want one", got-replayed)
	}
	if got := cursorCell(t, guest, 1); got != moved {
		t.Fatalf("correction at production tick rolled the cursor from %v back to %v", moved, got)
	}

	// Once the host reaches the frame's apply tick, its next capture contains the
	// move and the guest neither loses nor replays it again.
	want := deliverCorrection(t, host, []*App{guest}, advance)
	assertCorrected(t, want, guest, "guest")
	if got := statOf(guest, "snapshot.replay_records"); got != replayed+1 {
		t.Fatalf("the same crossing was replayed again (%d total records)", got-replayed)
	}
	if got := cursorCell(t, guest, 1); got != moved {
		t.Fatalf("the authority applied the crossing at %v, want %v", got, moved)
	}
}

// TestACorrectionSupersedesAuthorityFramesItAlreadyContains pins the other side
// of the replay boundary. The authority applies its own ordinary crossings
// immediately, so a capture can contain a frame whose receive-side ApplyTick is
// still in the future. Keeping that peer copy after installing the capture makes
// a remote cursor walk backwards through already-authoritative positions.
func TestACorrectionSupersedesAuthorityFramesItAlreadyContains(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name              string
		scheduleBeforeCap bool
	}{
		{name: "already scheduled", scheduleBeforeCap: true},
		{name: "arrives after install"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			apps := meshSession(t, 0x5EEDBEEF, 2, [][2]int{{1, 2}})
			localCursors(t, apps)
			host, guest := apps[0], apps[1]
			advance := func() { tickAll(apps) }
			deliverCorrection(t, host, []*App{guest}, advance)

			before := cursorCell(t, host, 0)
			inject(t, host, intentMotion(input.MotionRight, 1))
			first := cursorCell(t, host, 0)
			if first.X != before.X+1 || first.Y != before.Y {
				t.Fatalf("the first host motion moved from %v to %v", before, first)
			}

			// Close the first motion's epoch. In one case the guest drains the
			// batch before the correction; in the other it remains on the link.
			host.Tick(1)
			if tc.scheduleBeforeCap {
				guest.Tick(1)
			}

			// This second motion is already in the host's world but its peer copy
			// remains in the open epoch. The capture therefore stands one cell
			// beyond the stale frame the guest has, or is about to receive.
			inject(t, host, intentMotion(input.MotionRight, 1))
			latest := cursorCell(t, host, 0)
			if latest.X != first.X+1 || latest.Y != first.Y {
				t.Fatalf("the second host motion moved from %v to %v", first, latest)
			}
			cap, err := host.CaptureShared()
			if err != nil {
				t.Fatalf("capture: %v", err)
			}
			if cap.Header.Authority != 1 || cap.Header.Crossings.Seq(1) == 0 {
				t.Fatalf("capture fences = authority %d, participant 1 sequence %d; want participant 1 and a completed crossing",
					cap.Header.Authority, cap.Header.Crossings.Seq(1))
			}
			if err := guest.corrections.install(cap); err != nil {
				t.Fatalf("install tick %d: %v", cap.Header.Tick, err)
			}
			if got := cursorCell(t, guest, 0); got != latest {
				t.Fatalf("correction installed host cursor at %v, want %v", got, latest)
			}

			// Do not tick the host: the second peer copy must stay unsent. Once
			// the first copy reaches its nominal ApplyTick, it must not overwrite
			// the newer position the correction already installed.
			for range parameter.NetworkBarrierDelayTicks + 1 {
				guest.Tick(1)
			}
			if got := cursorCell(t, guest, 0); got != latest {
				t.Fatalf("stale authority frame rolled the corrected cursor from %v back to %v", latest, got)
			}
		})
	}
}

// TestAuthorityCrossingFenceWaitsForDispatch keeps the capture watermark tied to
// state rather than to transport admission. A crossing may be encoded and queued
// before the scheduler can acquire the world lock; a capture in that interval
// must not claim the event it has not applied.
func TestAuthorityCrossingFenceWaitsForDispatch(t *testing.T) {
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

	queued, err := host.CaptureShared()
	if err != nil {
		t.Fatalf("capture with queued crossing: %v", err)
	}
	if got := queued.Header.Crossings.Seq(1); got != before.Header.Crossings.Seq(1) {
		t.Fatalf("queued crossing advanced the authority's fence from %d to %d before dispatch",
			before.Header.Crossings.Seq(1), got)
	}
	if got := cursorCell(t, host, 0); got != cell {
		t.Fatalf("queued crossing moved cursor from %v to %v before dispatch", cell, got)
	}

	host.Settle()
	applied, err := host.CaptureShared()
	if err != nil {
		t.Fatalf("capture after dispatch: %v", err)
	}
	if got := applied.Header.Crossings.Seq(1); got <= queued.Header.Crossings.Seq(1) {
		t.Fatalf("dispatched crossing left the authority's fence at %d, want after %d",
			got, queued.Header.Crossings.Seq(1))
	}
	if got := cursorCell(t, host, 0); got.X != cell.X+1 || got.Y != cell.Y {
		t.Fatalf("dispatched crossing moved cursor from %v to %v", cell, got)
	}
}

// TestARewindDoesNotReuseAProductionEpoch covers the other clock carried by a
// correction. The world tick may move backwards, but a source's wire epochs are a
// monotonic stream: reusing one makes every peer's replay filter discard the new
// batch before it can inspect the frames inside it.
func TestARewindDoesNotReuseAProductionEpoch(t *testing.T) {
	t.Parallel()
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)
	deliverCorrection(t, host, []*App{guest}, advance)

	// Capture one authority tick, then let the guest close the following epoch and
	// make sure the host has admitted its marker before rewinding the guest.
	advance()
	cap, err := host.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	baseline := cap.Header.Tick
	advance()
	host.Tick(1)
	if got := guest.Position().Tick; got != baseline+1 {
		t.Fatalf("guest reached tick %d, want %d before rewind", got, baseline+1)
	}
	if err := guest.corrections.install(cap); err != nil {
		t.Fatalf("install tick %d: %v", baseline, err)
	}
	if got := guest.Position().Tick; got != baseline {
		t.Fatalf("correction left guest at tick %d, want %d", got, baseline)
	}

	before := cursorCell(t, host, 1)
	inject(t, guest, intentMotion(input.MotionRight, 1))
	moved := cursorCell(t, guest, 1)
	if moved.X != before.X+1 || moved.Y != before.Y {
		t.Fatalf("guest moved from host cell %v to %v", before, moved)
	}

	suffix, dropped := replaySuffixOf(t, guest, hostFence(t, host, guestParticipant))
	if len(suffix) != 1 || dropped != 0 {
		t.Fatalf("post-rewind suffix has %d records and %d drops, want one healthy record",
			len(suffix), dropped)
	}
	applyTick := suffix[0].ApplyTick

	// The first re-simulated tick must not send another batch under baseline+1.
	// The next tick reaches the source's unsent epoch and closes one batch carrying
	// the move. Once the host reaches its ApplyTick it must have accepted and
	// applied the absolute cursor move exactly once.
	guest.Tick(2)
	for host.Position().Tick < applyTick {
		host.Tick(1)
	}
	if got := cursorCell(t, host, 1); got != moved {
		t.Fatalf("host discarded the post-rewind input: cursor is %v, want %v", got, moved)
	}
}

// TestAGoldSequenceSurvivesACorrectionWithoutATick's second half:
// a whole gold run typed inside one tick is retained as a suffix and survives a
// correction taken before it, and every member is still gone afterwards.
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
	if err := host.PublishCorrection(); err != nil {
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
	// replay is what keeps it gone.
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

	// And the session converges: the host applies the same crossings on its own
	// schedule, and the next correction finds nothing left to disagree about.
	want := deliverCorrection(t, host, []*App{guest}, advance)
	assertCorrected(t, want, guest, "guest")
}

// TestAnIncompleteSuffixFallsBackToTheAuthority: Retention that
// dropped a record it would have needed offers nothing at all, and the guest
// installs the authority alone and says so.
func TestAnIncompleteSuffixFallsBackToTheAuthority(t *testing.T) {
	t.Parallel()
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)
	deliverCorrection(t, host, []*App{guest}, advance)

	advance()
	if err := host.PublishCorrection(); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Past the record bound inside one window. Nothing an ordinary session does
	// reaches this — the bounds are far wider than a cadence — but a participant
	// that produced faster than retention allows must not be replayed from a
	// suffix with a hole in it.
	src := replaySourceOf(t, guest)
	for range parameter.SnapshotReplayRecords + 32 {
		inject(t, guest, intentMotion(input.MotionRight, 1))
		inject(t, guest, intentMotion(input.MotionLeft, 1))
	}
	if _, dropped := src.ReplaySuffixSize(); dropped == 0 {
		t.Fatal("retention dropped nothing, so there is no hole to refuse")
	}
	if _, _, ok := src.LocalReplaySuffix(hostFence(t, host, guestParticipant)); ok {
		t.Fatal("a suffix with a hole was offered as if it were complete")
	}

	skipped := statOf(guest, "snapshot.replay_skipped")
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
	if got := statOf(guest, "snapshot.replay_skipped"); got <= skipped {
		t.Fatal("an unavailable suffix was not reported as skipped")
	}
	if !statBoolOf(guest, "snapshot.replay_suffix_unavailable") {
		t.Fatal("an unavailable suffix left the indicator clear")
	}
	if got := statOf(guest, "snapshot.replay_records"); got != 0 {
		t.Fatalf("an unavailable suffix replayed %d records anyway", got)
	}
	if got := statOf(guest, "snapshot.replay_overflow"); got == 0 {
		t.Fatal("retention overflow was not published")
	}

	// The authority is intact rather than half-applied, and the session converges
	// on the next correction as it always did.
	want := deliverCorrection(t, host, []*App{guest}, advance)
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

// TestALateGuestActionIsNotUndoneByTheCorrectionThatMissedIt is the regression this
// whole fence exists for, driven end to end.
//
// What a player sees when it is wrong: they press a key, their cursor moves, and a
// fifth of a second later it jumps back to where it was — then moves again. On one
// machine the link never misses the playout lead and it almost never happens; add
// Internet delay and it is the ordinary case for every action a correction
// straddles.
//
// The mechanism is a boundary that asks the wrong question. The guest produces a
// crossing for tick T+3 and applies it at once; the host has not received it when it
// reads its world at T+9, so the capture cannot contain it. Judging membership by
// tick, the guest sees an apply tick six ticks in the past, concludes the correction
// already holds the action, and drops it — undoing its own keystroke. The host
// applies the late frame when it finally arrives and the next capture puts it back,
// which is the second half of the flicker.
//
// The fence asks the right question: the capture says how much of this guest's
// stream the authority had, and everything past that is replayed.
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
	moved := cursorCell(t, guest, 1)
	if moved.X != before.X+3 || moved.Y != before.Y {
		t.Fatalf("the guest's own motion moved its cursor from %v to %v", before, moved)
	}

	// Past the apply tick the crossing named, while the shaped link still holds it.
	for range 2 * parameter.NetworkBarrierDelayTicks {
		host.Tick(1)
		guest.Tick(1)
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

	if err := guest.corrections.install(cap); err != nil {
		t.Fatalf("install: %v", err)
	}
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
