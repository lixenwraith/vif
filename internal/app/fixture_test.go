// Package app tests: the fixtures every group builds on.
package app

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/resource"
)

// mustHeadless builds a driven App on the embedded assets, failing the test on error
func mustHeadless(t *testing.T, seed uint64, w, h int) *App {
	t.Helper()
	a, err := NewHeadless(Config{Seed: seed, Width: w, Height: h, Resources: resource.Options{Embedded: true}})
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
		Seed: seed, Width: w, Height: h, Resources: resource.Options{Embedded: true},
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

func statOf(a *App, key string) (v int64) {
	a.World().RunSafe(func() { v = a.World().Resources.Status.Ints.Get(key).Load() })
	return v
}

// tickAll advances every participant by one tick, which is the paired boundary the
// barrier's fixed playout lead is measured against.
func tickAll(apps []*App) {
	for _, a := range apps {
		a.Tick(1)
	}
}

// statBoolOf reads a boolean counter as an integer, so the numeric helpers above
// can assert both kinds the same way.
func statBoolOf(a *App, key string) (v bool) {
	a.World().RunSafe(func() { v = a.World().Resources.Status.Bools.Get(key).Load() })
	return v
}

// cursorPosition reads one roster entity under the world lock.
func cursorPosition(a *App, e core.Entity) (pos component.PositionComponent) {
	a.World().RunSafe(func() { pos, _ = a.World().Positions.GetPosition(e) })
	return pos
}
