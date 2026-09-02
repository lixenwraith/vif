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

// Phase 1 of the multiplayer plan states its goal as an equality: a session's local
// cursor and typing must respond exactly as a solo run does. Every test here
// therefore measures the same probe twice — once solo, once on the producing
// instance of a live two-participant session — and asserts the two agree.
//
// The session figures these replaced are recorded in
// doc/multi-player-enhancement.md §3: one keypress reaching the store only after
// the playout lead, one cell of five, and five typing errors out of six correct
// keystrokes. D-18's prediction is what closes the gap; the barrier below it is
// deliberately unchanged, and the first test asserts that too.

// soloInstance is one participant with a cursor and no session.
func soloInstance(t *testing.T, seed uint64) *App {
	t.Helper()
	a := mustHeadless(t, seed, 120, 40)
	t.Cleanup(a.Close)
	tickUntilCursor(t, a)
	a.Tick(1)
	return a
}

// liveInstance is the producing instance of a live two-participant session, past
// the first ticks so the barrier owns its crossings.
func liveInstance(t *testing.T, seed uint64) (*App, []*App) {
	t.Helper()
	apps := meshSession(t, seed, 2, [][2]int{{1, 2}})
	localCursors(t, apps)
	for range 3 {
		tickAll(apps)
	}
	return apps[0], apps
}

// localCell reads what this instance's own input and view resolve against.
func localCell(a *App) (pos component.PositionComponent, ok bool) {
	a.World().RunSafe(func() { pos, ok = a.World().LocalCursor() })
	return pos, ok
}

// localCursorEntity returns the shared cursor this instance drives.
func localCursorEntity(a *App) (e core.Entity) {
	a.World().RunSafe(func() { e = a.World().Resources.Player.Entity })
	return e
}

func inject(t *testing.T, a *App, intents ...*input.Intent) {
	t.Helper()
	if !a.Inject(intents...) {
		t.Fatal("intent quit the game")
	}
}

// TestOneKeypressMovesTheLocalCursorWithoutATick is §3's first row. Solo, a press
// lands before any tick; in a session it used to take the whole playout lead.
func TestOneKeypressMovesTheLocalCursorWithoutATick(t *testing.T) {
	press := func(a *App) (before, after component.PositionComponent) {
		t.Helper()
		before, _ = localCell(a)
		inject(t, a, intentMotion(input.MotionRight, 1))
		after, _ = localCell(a)
		return before, after
	}

	solo := soloInstance(t, 0x10CA1)
	soloBefore, soloAfter := press(solo)
	if soloAfter.X != soloBefore.X+1 || soloAfter.Y != soloBefore.Y {
		t.Fatalf("solo press moved the cursor to %#v, want one cell right of %#v", soloAfter, soloBefore)
	}

	live, apps := liveInstance(t, 0x10CA1)
	liveBefore, liveAfter := press(live)
	if liveAfter.X != liveBefore.X+1 || liveAfter.Y != liveBefore.Y {
		t.Fatalf("session press moved the cursor to %#v, want the solo answer: one cell right of %#v",
			liveAfter, liveBefore)
	}

	// Phase 1 predicted the cell this participant reads; Phase 4 took the playout
	// lead off the shared store as well, so the producer's own crossing is applied
	// in the tick that produced it and the prediction and the store agree at once.
	// The peers keep the lead, which is where a remote participant's motion is
	// interpolated from.
	local := localCursorEntity(live)
	want := liveBefore
	want.X++
	if got := cursorPosition(live, local); got != want {
		t.Fatalf("the producer's own crossing landed at %#v, want %#v with no lead", got, want)
	}
	if got := cursorPosition(apps[1], local); got != liveBefore {
		t.Fatalf("a peer applied the crossing at %#v before its apply tick", got)
	}
	for range parameter.NetworkBarrierDelayTicks + 1 {
		tickAll(apps)
	}
	for i, a := range apps {
		if got := cursorPosition(a, local); got != want {
			t.Fatalf("participant %d applied the crossing as %#v, want %#v", i+1, got, want)
		}
	}
}

// TestFiveKeypressesBetweenTicksReachFiveCells is §3's second row. Every press
// resolves its motion from the cell the previous one selected, so five presses
// select five cells; a session that re-read the shared store selected one, four
// times over.
func TestFiveKeypressesBetweenTicksReachFiveCells(t *testing.T) {
	const presses = 5

	cells := func(a *App, drain func()) (int, component.PositionComponent) {
		t.Helper()
		local := localCursorEntity(a)
		seen := map[component.PositionComponent]bool{}
		a.SetDispatchTap(func(ev event.GameEvent) {
			if ev.Type != event.EventCursorMoved {
				return
			}
			if p, ok := ev.Payload.(*event.CursorMovedPayload); ok && p.Entity == local {
				seen[component.PositionComponent{X: p.X, Y: p.Y}] = true
			}
		})
		defer a.SetDispatchTap(nil)

		for range presses {
			inject(t, a, intentMotion(input.MotionRight, 1))
		}
		drain()
		pos, _ := localCell(a)
		return len(seen), pos
	}

	solo := soloInstance(t, 0x5CE115)
	soloStart, _ := localCell(solo)
	soloCells, soloEnd := cells(solo, func() { solo.Tick(1) })
	if soloCells != presses {
		t.Fatalf("solo placed the cursor on %d cells, want %d", soloCells, presses)
	}
	if soloEnd.X != soloStart.X+presses {
		t.Fatalf("solo cursor ended at %#v, want %d cells right of %#v", soloEnd, presses, soloStart)
	}

	live, apps := liveInstance(t, 0x5CE115)
	liveStart, _ := localCell(live)
	liveCells, liveEnd := cells(live, func() {
		for range parameter.NetworkBarrierDelayTicks + 1 {
			tickAll(apps)
		}
	})
	if liveCells != soloCells {
		t.Fatalf("the session placed the cursor on %d cells, want the solo %d", liveCells, soloCells)
	}
	if liveEnd.X != liveStart.X+presses {
		t.Fatalf("session cursor ended at %#v, want %d cells right of %#v", liveEnd, presses, liveStart)
	}
}

// glyphRun writes runes into the cells the local cursor stands on and to its right,
// so a keystroke that lands on its own cell finds its own character there.
//
// The run is player-domain, which is what a corpus glyph is (§4: every shared glyph
// is a gold composite member). Whatever the corpus already put on those cells is
// destroyed first, because the typing path answers with the first glyph it finds in
// the cell; a shared one would make the probe measure a composite instead, and the
// test says so rather than quietly measuring something else.
func glyphRun(t *testing.T, a *App, runes string) {
	t.Helper()
	pos, ok := localCell(a)
	if !ok {
		t.Fatal("no local cursor")
	}

	var shared []component.PositionComponent
	a.World().RunSafe(func() {
		w := a.World()
		var buf [parameter.MaxEntitiesPerCell]core.Entity
		for i, r := range runes {
			cell := component.PositionComponent{X: pos.X + i, Y: pos.Y}
			n := w.Positions.GetAllEntitiesAtInto(cell.X, cell.Y, buf[:])
			for _, e := range buf[:n] {
				if !w.Components.Glyph.HasEntity(e) {
					continue
				}
				if e.Domain() == core.DomainShared {
					shared = append(shared, cell)
					continue
				}
				w.DestroyEntity(e)
			}
			g := w.CreateEntity(core.DomainPlayer)
			w.Positions.SetPosition(g, cell)
			w.Components.Glyph.SetComponent(g, component.GlyphComponent{
				Rune: r, Type: component.GlyphRed, Level: 1,
			})
		}
	})
	if len(shared) > 0 {
		t.Fatalf("a shared glyph occupies %v; the run would answer with it instead", shared)
	}
}

// TestFastTypingOverAGlyphRunScoresNoErrors is §3's third row, and the one that
// matters most: in a typing game, keystrokes issued faster than the playout lead
// were not merely dropped, they were scored against the player, because each one
// resolved against a cell whose glyph the previous keystroke had already consumed.
func TestFastTypingOverAGlyphRunScoresNoErrors(t *testing.T) {
	const run = "abcdef"

	typed := func(a *App) (correct, errors int64) {
		t.Helper()
		glyphRun(t, a, run)
		inject(t, a, intentModeSwitch(input.ModeTargetInsert))
		for _, r := range run {
			inject(t, a, intentTextChar(r))
		}
		a.World().RunSafe(func() {
			reg := a.World().Resources.Status
			correct = reg.Ints.Get("typing.correct").Load()
			errors = reg.Ints.Get("typing.errors").Load()
		})
		return correct, errors
	}

	solo := soloInstance(t, 0x7791AB)
	soloCorrect, soloErrors := typed(solo)
	if soloCorrect != int64(len(run)) || soloErrors != 0 {
		t.Fatalf("solo typed %d correct, %d errors; want %d and 0", soloCorrect, soloErrors, len(run))
	}

	live, _ := liveInstance(t, 0x7791AB)
	liveCorrect, liveErrors := typed(live)
	if liveCorrect != soloCorrect || liveErrors != soloErrors {
		t.Fatalf("the session typed %d correct, %d errors; want the solo %d and %d",
			liveCorrect, liveErrors, soloCorrect, soloErrors)
	}
}

// TestTypedGoldMembersDisappearWithoutATick pins the shared half of local input
// prediction. Gold is composite and shared, but the producing peer must still see
// each correct member leave the screen before the next terminal frame; the same
// crossing reaches the other peers on their playout schedule and corrections
// remain free to reconcile the provisional result.
func TestTypedGoldMembersDisappearWithoutATick(t *testing.T) {
	type member struct {
		entity core.Entity
		cell   component.PositionComponent
		rune   rune
	}

	live, _ := liveInstance(t, 0x601D)
	live.Context().PushEventOrigin(event.EventGoldSpawnRequest, nil, event.OriginDebug)
	live.Settle()
	live.Tick(2)

	var run []member
	live.World().RunSafe(func() {
		w := live.World()
		for _, headerEntity := range w.Components.Header.GetAllEntities() {
			header, ok := w.Components.Header.GetComponent(headerEntity)
			if !ok || header.Behavior != component.BehaviorGold {
				continue
			}
			for _, entry := range header.MemberEntries {
				glyph, glyphOK := w.Components.Glyph.GetComponent(entry.Entity)
				cell, cellOK := w.Positions.GetPosition(entry.Entity)
				if glyphOK && cellOK {
					run = append(run, member{entity: entry.Entity, cell: cell, rune: glyph.Rune})
				}
			}
			break
		}
		slices.SortFunc(run, func(a, b member) int { return cmp.Compare(a.cell.X, b.cell.X) })
		if len(run) > 0 {
			cursor := w.Resources.Player.Entity
			w.Positions.SetPosition(cursor, run[0].cell)
			w.Resources.Player.DropPrediction()
		}
	})
	if len(run) != parameter.GoldSequenceLength {
		t.Fatalf("gold run has %d members, want %d", len(run), parameter.GoldSequenceLength)
	}

	inject(t, live, intentModeSwitch(input.ModeTargetInsert))
	startTick := live.Position().Tick
	for i, m := range run {
		inject(t, live, intentTextChar(m.rune))
		if got := live.Position().Tick; got != startTick {
			t.Fatalf("typing member %d advanced tick %d to %d", i, startTick, got)
		}
		live.World().RunSafe(func() {
			if live.World().Components.Glyph.HasEntity(m.entity) {
				t.Fatalf("typed gold member %d remains renderable before a tick", i)
			}
		})
		if got, _ := sharedGlyphs(live); got != len(run)-i-1 {
			t.Fatalf("after member %d, %d shared glyphs remain; want %d", i, got, len(run)-i-1)
		}
	}
}

// TestPredictedLocalCursorReconcilesAndSnaps is D-18's reconcile half. A placement
// this participant did not request is the authority, and the prediction it disagrees
// with is discarded rather than merged: the queue is emptied and the local cell
// falls back to the store.
func TestPredictedLocalCursorReconcilesAndSnaps(t *testing.T) {
	live, apps := liveInstance(t, 0xD18ADD)
	local := localCursorEntity(live)
	start, _ := localCell(live)

	// Two of this participant's own placements, outstanding behind the barrier.
	inject(t, live, intentMotion(input.MotionRight, 1))
	inject(t, live, intentMotion(input.MotionRight, 1))
	predicted := start
	predicted.X += 2
	if got, _ := localCell(live); got != predicted {
		t.Fatalf("two presses predicted %#v, want %#v", got, predicted)
	}

	// A placement the prediction did not produce. Stamped shared, so it is not a
	// crossing and CursorSystem applies it at once — a level setup, a wall push-out
	// and a reset all reach the local cursor this way.
	snap := start
	snap.X -= 4
	live.Context().PushEventOrigin(event.EventCursorMoveRequest,
		&event.CursorMoveRequestPayload{Entity: local, X: snap.X, Y: snap.Y}, event.OriginDebug)
	live.Settle()

	if got, _ := localCell(live); got != snap {
		t.Fatalf("local cell after an unpredicted placement = %#v, want the authoritative %#v", got, snap)
	}
	if got := cursorPosition(live, local); got != snap {
		t.Fatalf("store after an unpredicted placement = %#v, want %#v", got, snap)
	}

	// Discarded, not merged, and nothing comes back to un-discard it. Phase 4 took
	// the playout lead off the local path, so the two crossings the prediction
	// described had already applied on this instance before the authoritative
	// placement replaced them; what is still in flight is the peers' copies, which
	// land at the agreed tick and are then corrected by the host like any other
	// disagreement.
	for range parameter.NetworkBarrierDelayTicks + 1 {
		tickAll(apps)
	}
	if got, _ := localCell(live); got != snap {
		t.Fatalf("local cell after the lead drained = %#v, want the authoritative %#v", got, snap)
	}
	if got := cursorPosition(live, local); got != snap {
		t.Fatalf("store after the lead drained = %#v, want %#v", got, snap)
	}
	if got := cursorPosition(apps[1], local); got != predicted {
		t.Fatalf("the peer applied the outstanding crossings as %#v, want %#v", got, predicted)
	}
}
