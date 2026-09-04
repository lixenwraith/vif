package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
)

// PeerCursorRenderer draws the cursors this instance does not drive.
//
// Without it another participant is visible only through the effects it happens
// to be projecting — its shield, its ember — so a player holding none is not on
// the map at all. The cursor itself is the one thing every participant always
// has, and it is what a person looks for.
//
// It is deliberately not the local cursor's renderer with a loop around it. The
// local cursor answers "where am I, and what am I about to act on", so it takes
// the colour of the thing under it and follows the local input mode; a peer
// answers "where is that player", so it keeps one colour per roster slot whatever
// it is standing on. Drawing them the same way would make the two indistinguishable
// exactly when it matters — when they overlap.
type PeerCursorRenderer struct {
	gameCtx *engine.GameContext
}

// NewPeerCursorRenderer creates a renderer for the non-local cursors.
func NewPeerCursorRenderer(gameCtx *engine.GameContext) *PeerCursorRenderer {
	return &PeerCursorRenderer{gameCtx: gameCtx}
}

// IsVisible reports whether the roster holds a cursor other than this one's.
// A solo run draws nothing and pays one integer compare for the privilege.
func (r *PeerCursorRenderer) IsVisible() bool {
	return r.gameCtx.World.Resources.Player.Count() > 1
}

// Render draws every rostered cursor this instance does not simulate.
func (r *PeerCursorRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	world := r.gameCtx.World
	roster := world.Resources.Player

	buf.SetWriteMask(visual.MaskUI)

	world.Components.Cursor.Each(func(e core.Entity, c *component.CursorComponent) bool {
		if roster.IsLocal(e) {
			return true
		}
		pos, ok := world.Positions.GetPosition(e)
		if !ok {
			return true
		}
		screenX, screenY, visible := ctx.MapToScreen(pos.X, pos.Y)
		if !visible {
			return true
		}

		// The cell's own character, drawn over the slot colour rather than under
		// it: what a peer is standing on stays readable, and the colour saying
		// which peer it is does not change with the cell.
		char := ' '
		glyphEntity, sigilEntity := cursorCellContent(r.gameCtx, pos.X, pos.Y)
		switch {
		case glyphEntity != 0:
			if glyph, ok := world.Components.Glyph.GetPtr(glyphEntity); ok {
				char = glyph.Rune
			}
		case sigilEntity != 0:
			if sigil, ok := world.Components.Sigil.GetPtr(sigilEntity); ok {
				char = sigil.Rune
			}
		}

		buf.SetWithBg(screenX, screenY, char, visual.RgbPeerCursorText, peerCursorColor(c.Slot))
		return true
	})
}

// peerCursorColor is one roster slot's colour, wrapped so a slot beyond the
// palette is drawn in some peer's colour rather than in none.
func peerCursorColor(slot uint8) color.RGB {
	return visual.RgbPeerCursor[int(slot)%len(visual.RgbPeerCursor)]
}
