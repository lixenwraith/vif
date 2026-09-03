package app

import (
	"cmp"
	"slices"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// The replay suite is about the seam between two claims that pull in opposite
// directions: a correction makes a guest hold the authority's world, and a
// participant's own accepted actions must not disappear when one arrives.
//
// The window is what reconciles them. Production ticks bound retention; the
// authoritative apply tick decides membership. A capture at T contains everything
// due at or before T and cannot contain a crossing due after T, even when that
// crossing was produced at or before T and is still inside the playout lead.

// TestLocalCrossingsAfterTheBaselineSurviveExactlyOnce is requirement 9. A guest
// produces a crossing, then installs an authority taken before it, and the effect
// is present exactly once afterwards.
func TestLocalCrossingsAfterTheBaselineSurviveExactlyOnce(t *testing.T) {
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)
	deliverCorrection(t, host, []*App{guest}, advance)

	// The authority is read here, before the guest acts. It has to describe a tick
	// the guest has not already installed, or the correction is superseded rather
	// than applied.
	advance()
	if err := host.PublishCorrection(); err != nil {
		t.Fatalf("publish: %v", err)
	}
	baseline := host.Position().Tick

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
	suffix, dropped := replaySuffixOf(t, guest, baseline)
	if len(suffix) == 0 {
		t.Fatal("the guest retained no crossing produced after the baseline")
	}
	if suffix[0].ApplyTick <= baseline {
		t.Fatalf("the retained crossing applies at tick %d, at or before the baseline %d",
			suffix[0].ApplyTick, baseline)
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
	suffix, dropped := replaySuffixOf(t, guest, baseline)
	if len(suffix) != 1 {
		t.Fatalf("baseline %d offered %d crossings, want the one still in flight", baseline, len(suffix))
	}
	if suffix[0].ApplyTick <= baseline {
		t.Fatalf("the in-flight crossing applies at tick %d, at or before baseline %d",
			suffix[0].ApplyTick, baseline)
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

// TestARewindDoesNotReuseAProductionEpoch covers the other clock carried by a
// correction. The world tick may move backwards, but a source's wire epochs are a
// monotonic stream: reusing one makes every peer's replay filter discard the new
// batch before it can inspect the frames inside it.
func TestARewindDoesNotReuseAProductionEpoch(t *testing.T) {
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

	suffix, dropped := replaySuffixOf(t, guest, baseline)
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

// TestAGoldSequenceSurvivesACorrectionWithoutATick is requirement 9's second half:
// a whole gold run typed inside one tick is retained as a suffix and survives a
// correction taken before it, and every member is still gone afterwards.
func TestAGoldSequenceSurvivesACorrectionWithoutATick(t *testing.T) {
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

// TestAnIncompleteSuffixFallsBackToTheAuthority is requirement 10. Retention that
// dropped a record it would have needed offers nothing at all, and the guest
// installs the authority alone and says so.
func TestAnIncompleteSuffixFallsBackToTheAuthority(t *testing.T) {
	host, guest, advance := selectivePair(t, 0x5EEDBEEF)
	deliverCorrection(t, host, []*App{guest}, advance)

	advance()
	if err := host.PublishCorrection(); err != nil {
		t.Fatalf("publish: %v", err)
	}
	baseline := host.Position().Tick

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
	if _, _, ok := src.LocalReplaySuffix(baseline); ok {
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
	src := a.replaySourceLocked()
	if src == nil {
		t.Fatal("this run has no barrier to retain a suffix")
	}
	return src
}

// replaySuffixOf reads what a guest would replay onto one baseline.
func replaySuffixOf(t *testing.T, a *App, baseline uint64) ([]event.ScheduledWireFrame, int64) {
	t.Helper()
	src := replaySourceOf(t, a)
	frames, _, ok := src.LocalReplaySuffix(baseline)
	if !ok {
		t.Fatal("the suffix is unavailable before anything has been dropped")
	}
	_, dropped := src.ReplaySuffixSize()
	return frames, dropped
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
