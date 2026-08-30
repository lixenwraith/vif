package app

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
)

// mustHeadless builds a driven App on the embedded assets, failing the test on error
func mustHeadless(t *testing.T, seed uint64, w, h int) *App {
	t.Helper()
	a, err := NewHeadless(Config{Seed: seed, Width: w, Height: h, ForceDefault: true})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	return a
}

// mustJoiner builds a participant the way ConfigForJoin does for a real join: it
// adopts the session's D-14 bounds before its FSM boots, and it latches the world
// as shared. Its terminal is its own.
//
// Both halves matter and for different reasons. A joiner whose world took its
// bounds from that terminal would spawn cursor slot zero on a different cell than
// the host and never recover (D-11). And one that did not latch would leave the
// playout barrier disengaged, so every crossing it re-derives would apply a lead
// earlier than the session applied it — which is invisible until an FSM deadline
// falls inside that lead.
func mustJoiner(t *testing.T, seed uint64, w, h int, an event.JoinAnchor) *App {
	t.Helper()
	a, err := NewHeadless(Config{
		Seed: seed, Width: w, Height: h, ForceDefault: true,
		MapWidth: an.Anchor.MapWidth, MapHeight: an.Anchor.MapHeight,
		CropOnResize: an.Anchor.CropOnResize, LockMap: an.Anchor.SessionShared,
	})
	if err != nil {
		t.Fatalf("headless joiner: %v", err)
	}
	return a
}

// tickUntilCursor advances until the FSM has spawned the first cursor
func tickUntilCursor(t *testing.T, a *App) {
	t.Helper()
	for range 40 {
		a.Tick(1)
		var n int
		a.World().RunSafe(func() { n = a.World().Resources.Player.Count() })
		if n > 0 {
			return
		}
	}
	t.Fatal("no cursor after 40 ticks")
}

// spawnCursor adds one auto-slot cursor at the map centre and settles the request
func spawnCursor(t *testing.T, a *App) {
	t.Helper()
	var before, after int
	a.World().RunSafe(func() { before = a.World().Resources.Player.Count() })
	a.Context().PushEventOrigin(event.EventCursorSpawnRequest,
		&event.CursorSpawnRequestPayload{Auto: true, Center: true}, event.OriginDebug)
	a.Settle()
	a.World().RunSafe(func() { after = a.World().Resources.Player.Count() })
	if after != before+1 {
		t.Fatalf("roster has %d cursors, want %d", after, before+1)
	}
}

// plantSentinel places a bare shared entity near the far map corner, where a
// cropping resize would mark it for death
func plantSentinel(t *testing.T, a *App) (e core.Entity, mapW, mapH int) {
	t.Helper()
	w := a.World()
	cfg := w.Resources.Config
	w.RunSafe(func() {
		mapW, mapH = cfg.MapWidth, cfg.MapHeight
		e = w.CreateEntity(core.DomainShared)
		w.Positions.SetPosition(e, component.PositionComponent{X: mapW - 2, Y: mapH - 2})
	})
	return e, mapW, mapH
}

// sentinelState reports whether the entity still holds a position and whether the
// crop path marked it for death
func sentinelState(a *App, e core.Entity) (alive, doomed bool) {
	a.World().RunSafe(func() {
		alive = a.World().Positions.HasPosition(e)
		doomed = a.World().Components.Death.HasEntity(e)
	})
	return alive, doomed
}

// TestMapSizeLockedWithSecondCursor asserts D-14: once a second participant exists,
// this instance's terminal no longer rewrites the shared map, and nothing outside
// the shrunken viewport is cropped away.
func TestMapSizeLockedWithSecondCursor(t *testing.T) {
	a := mustHeadless(t, 0x14AB, 200, 60)
	defer a.Close()
	tickUntilCursor(t, a)
	spawnCursor(t, a)
	a.Tick(1)

	sentinel, mapW, mapH := plantSentinel(t, a)

	a.Resize(60, 24)
	a.Tick(2)

	cfg := a.World().Resources.Config
	var gotW, gotH int
	a.World().RunSafe(func() { gotW, gotH = cfg.MapWidth, cfg.MapHeight })
	if gotW != mapW || gotH != mapH {
		t.Fatalf("map resized under a second cursor: %dx%d, want %dx%d", gotW, gotH, mapW, mapH)
	}
	if alive, doomed := sentinelState(a, sentinel); !alive || doomed {
		t.Fatalf("sentinel outside the new viewport was cropped: alive=%v doomed=%v", alive, doomed)
	}
	if !a.World().Resources.Status.Bools.Get("context.map_locked").Load() {
		t.Fatal("context.map_locked not published")
	}
	if sw, sh := engine.ScreenSize(cfg); sw != 60 || sh != 24 {
		t.Fatalf("viewport did not follow the terminal: ScreenSize = %dx%d", sw, sh)
	}
}

// TestMapSizeCropsWithOneCursor is the negative control: the same resize with a
// single participant crops the map and destroys the sentinel, so the guard above
// is asserting a suppression that would otherwise fire.
func TestMapSizeCropsWithOneCursor(t *testing.T) {
	a := mustHeadless(t, 0x14AC, 200, 60)
	defer a.Close()
	tickUntilCursor(t, a)

	sentinel, mapW, _ := plantSentinel(t, a)

	a.Resize(60, 24)
	a.Tick(2)

	cfg := a.World().Resources.Config
	var gotW, viewW int
	a.World().RunSafe(func() { gotW, viewW = cfg.MapWidth, cfg.ViewportWidth })
	if gotW == mapW || gotW != viewW {
		t.Fatalf("map %d after crop, want the new viewport %d (was %d)", gotW, viewW, mapW)
	}
	if alive, _ := sentinelState(a, sentinel); alive {
		t.Fatal("crop left an out-of-bounds entity alive; the locked-map test proves nothing")
	}
	if a.World().Resources.Status.Bools.Get("context.map_locked").Load() {
		t.Fatal("context.map_locked set while the map still follows the terminal")
	}
}

// TestJoinerOnAnotherTerminalSharesTheMapFromTickZero is the regression for the
// divergence two windows of different size produced from the first tick: the FSM
// boot script spawns cursor slot zero at the centre of the map, inside New and
// before anything is joined, so a participant that adopted the session's bounds
// afterwards held that shared cursor on its own terminal's centre instead. Nothing
// in the model corrects a shared position, so the two never agreed again.
func TestJoinerOnAnotherTerminalSharesTheMapFromTickZero(t *testing.T) {
	host := mustHeadless(t, 0x14AD, 160, 48)
	defer host.Close()
	an := host.JoinAnchor()
	guest := mustJoiner(t, 0x14AD, 84, 26, an)
	defer guest.Close()
	if err := guest.Join(an); err != nil {
		t.Fatalf("join: %v", err)
	}

	for _, a := range []*App{host, guest} {
		tickUntilCursor(t, a)
		a.Tick(1)
	}
	assertSharedParity(t, host, guest, 0)

	var hostView, guestView int
	host.World().RunSafe(func() { hostView = host.World().Resources.Config.ViewportWidth })
	guest.World().RunSafe(func() { guestView = guest.World().Resources.Config.ViewportWidth })
	if hostView == guestView {
		t.Fatalf("both participants ran a %d-column viewport; the criterion proves nothing", hostView)
	}
}

// TestSessionRunNeverCropsItsMap covers the window the peer count left open: a host
// waiting in its lobby has no peer and one cursor, so the old guard let a resize
// crop the very bounds the anchor it is handing out names.
func TestSessionRunNeverCropsItsMap(t *testing.T) {
	a, err := NewHeadless(Config{Seed: 0x14AE, Width: 200, Height: 60, ForceDefault: true, LockMap: true})
	if err != nil {
		t.Fatalf("headless: %v", err)
	}
	defer a.Close()
	tickUntilCursor(t, a)

	sentinel, mapW, mapH := plantSentinel(t, a)
	before := a.JoinAnchor()

	a.Resize(60, 24)
	a.Tick(2)

	cfg := a.World().Resources.Config
	var gotW, gotH int
	a.World().RunSafe(func() { gotW, gotH = cfg.MapWidth, cfg.MapHeight })
	if gotW != mapW || gotH != mapH {
		t.Fatalf("hosting run cropped to %dx%d, want the offered %dx%d", gotW, gotH, mapW, mapH)
	}
	if alive, doomed := sentinelState(a, sentinel); !alive || doomed {
		t.Fatalf("hosting run cropped away an entity: alive=%v doomed=%v", alive, doomed)
	}
	after := a.JoinAnchor()
	if after.Anchor.MapWidth != before.Anchor.MapWidth ||
		after.Anchor.MapHeight != before.Anchor.MapHeight {
		t.Fatalf("the anchor a joiner adopts moved under a resize: %dx%d then %dx%d",
			before.Anchor.MapWidth, before.Anchor.MapHeight,
			after.Anchor.MapWidth, after.Anchor.MapHeight)
	}
	if !after.Anchor.SessionShared {
		t.Fatal("a hosting run's anchor does not carry the D-14 latch a reproduction adopts")
	}
}

// TestLocalViewChangesLeaveTheFlowFieldPhaseAlone covers D-17 at its two producers.
//
// NavigationSystem's flow-field cache is throttled: MarkDirty only latches, and a
// field is recomputed once the interval allows, so the cache's phase is shared
// state and every producer of a dirty mark has to be shared. EventCursorMoved is
// one such producer, and two purely local view changes were announcing it — a
// resize that reconciled cursors the locked map had not moved, and the rebind that
// binds this participant's own slot. Either put the two instances on different
// recompute phases, after which they steer shared species along fields of different
// ages: a divergence that begins in kinetics, long before any cell moves.
//
// The rebind is the one that matters most, because it fires at session start on
// every participant but slot zero.
func TestLocalViewChangesLeaveTheFlowFieldPhaseAlone(t *testing.T) {
	a := mustHeadless(t, 0x14AF, 200, 60)
	defer a.Close()
	tickUntilCursor(t, a)
	spawnCursor(t, a) // a second cursor locks the map (D-14)
	a.Tick(2)

	moves := 0
	a.SetDispatchTap(func(ev event.GameEvent) {
		if ev.Type == event.EventCursorMoved {
			moves++
		}
	})
	a.Resize(90, 30)
	a.Tick(2)
	if moves != 0 {
		t.Fatalf("a resize under a locked map announced %d cursor moves", moves)
	}

	// A local rebind is the same rule in a second place, and the one that fires at
	// session start: every participant but slot zero binds its own cursor, so the
	// announcement offset the two instances' flow-field phases from tick one.
	spawnCursor(t, a)
	a.Tick(1)
	moves = 0
	a.Context().PushEventOrigin(event.EventCursorSetLocalRequest,
		&event.CursorSetLocalPayload{Slot: 1}, event.OriginDebug)
	a.Settle()
	a.Tick(1)
	if moves != 0 {
		t.Fatalf("a local rebind announced %d cursor moves", moves)
	}
	var bound uint8
	a.World().RunSafe(func() { bound = a.World().Resources.Player.LocalSlot() })
	if bound != 1 {
		t.Fatalf("rebind left the local slot at %d, want 1; the guard proves nothing", bound)
	}

	// The negative control: the same resize with one cursor crops the map, which
	// really does move cursors, and there the announcement is the mechanism.
	b := mustHeadless(t, 0x14AF, 200, 60)
	defer b.Close()
	tickUntilCursor(t, b)
	b.Tick(2)
	moves = 0
	b.SetDispatchTap(func(ev event.GameEvent) {
		if ev.Type == event.EventCursorMoved {
			moves++
		}
	})
	b.Resize(90, 30)
	b.Tick(2)
	if moves == 0 {
		t.Fatal("a cropping resize announced no cursor move; the guard above proves nothing")
	}
}
