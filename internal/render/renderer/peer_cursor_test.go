package renderer

import (
	"testing"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
)

// peerWorld builds a world holding one cursor per slot, with slot 0 local.
func peerWorld(t *testing.T, slots int) (*engine.GameContext, []core.Entity) {
	t.Helper()
	w := engine.NewWorld()
	ctx := engine.NewGameContextWithClock(w, 80, 24, engine.NewManualClock())

	cursors := make([]core.Entity, 0, slots)
	for slot := range slots {
		e := w.CreateEntity(core.DomainShared)
		w.Components.Cursor.SetComponent(e, component.CursorComponent{Slot: uint8(slot)})
		w.Positions.SetPosition(e, component.PositionComponent{X: 5 + slot, Y: 3})
		w.Resources.Player.Bind(uint8(slot), e)
		cursors = append(cursors, e)
	}
	w.Resources.Player.SetLocal(0)
	return ctx, cursors
}

// peerContext is the render context for a world, built directly: the renderer
// reads only the viewport and map geometry from it.
func peerContext(ctx *engine.GameContext) render.RenderContext {
	cfg := ctx.World.Resources.Config
	return render.RenderContext{
		ViewportWidth: cfg.ViewportWidth, ViewportHeight: cfg.ViewportHeight,
		MapWidth: cfg.MapWidth, MapHeight: cfg.MapHeight,
	}
}

// TestPeerCursorsAreDrawnAndTheLocalOneIsNot is the defect this closes: a
// participant used to be visible only through the effects it happened to be
// projecting, so one holding no shield was not on the map at all.
//
// The local cursor is excluded because its own renderer draws it, with the mode
// and the cell colour a player needs and a peer must not borrow.
func TestPeerCursorsAreDrawnAndTheLocalOneIsNot(t *testing.T) {
	t.Parallel()
	gameCtx, cursors := peerWorld(t, 3)
	rc := peerContext(gameCtx)

	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, 80, 24)
	r := NewPeerCursorRenderer(gameCtx)
	if !r.IsVisible() {
		t.Fatal("the peer renderer hid itself with peers on the map")
	}
	r.Render(rc, buf)

	cellFor := func(e core.Entity) terminal.Cell {
		t.Helper()
		pos, ok := gameCtx.World.Positions.GetPosition(e)
		if !ok {
			t.Fatal("a cursor this test placed has no position")
		}
		x, y, visible := rc.MapToScreen(pos.X, pos.Y)
		if !visible {
			t.Fatalf("cell (%d,%d) is outside a viewport this test placed it inside", pos.X, pos.Y)
		}
		return buf.CellAt(x, y)
	}

	if got := cellFor(cursors[0]).Bg; got == peerCursorColor(0) {
		t.Fatal("the peer renderer drew the local cursor")
	}
	for slot := 1; slot < len(cursors); slot++ {
		want := peerCursorColor(uint8(slot))
		if got := cellFor(cursors[slot]).Bg; got != want {
			t.Fatalf("slot %d drew background %v, want its slot colour %v", slot, got, want)
		}
	}
}

// TestASoloRunDrawsNoPeers keeps the renderer free on the path most runs take.
func TestASoloRunDrawsNoPeers(t *testing.T) {
	t.Parallel()
	gameCtx, _ := peerWorld(t, 1)
	if NewPeerCursorRenderer(gameCtx).IsVisible() {
		t.Fatal("a solo run drew peer cursors")
	}
}

// TestEverySlotHasItsOwnColour is what makes the colour identifying rather than
// decorative: two participants that shared one would read as one participant.
func TestEverySlotHasItsOwnColour(t *testing.T) {
	t.Parallel()
	seen := make(map[color.RGB]uint8, len(visual.RgbPeerCursor))
	for slot := range uint8(len(visual.RgbPeerCursor)) {
		c := peerCursorColor(slot)
		if prev, dup := seen[c]; dup {
			t.Fatalf("slots %d and %d share a colour", prev, slot)
		}
		seen[c] = slot
	}
	// Past the palette a slot wraps rather than losing its colour entirely: an
	// unrecognisable peer is still better than an invisible one.
	if peerCursorColor(uint8(len(visual.RgbPeerCursor))) != peerCursorColor(0) {
		t.Fatal("a slot past the palette did not wrap")
	}
}
