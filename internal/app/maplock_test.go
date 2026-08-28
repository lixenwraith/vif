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
