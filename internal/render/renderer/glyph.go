package renderer

import (
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
)

// GlyphRenderer draws typeable spawned content entities
type GlyphRenderer struct {
	gameCtx *engine.GameContext
}

// NewGlyphRenderer creates a new glyph renderer
func NewGlyphRenderer(gameCtx *engine.GameContext) *GlyphRenderer {
	return &GlyphRenderer{
		gameCtx: gameCtx,
	}
}

// Render draws all glyph entities
func (r *GlyphRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	glyphs := r.gameCtx.World.Components.Glyph
	if glyphs.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskGlyph)

	glyphs.Each(func(entity core.Entity, glyph *component.GlyphComponent) bool {
		// Gold is handled in its own composite renderer with a different mask
		if glyph.Type == component.GlyphGold {
			return true
		}

		pos, ok := r.gameCtx.World.Positions.GetPosition(entity)
		if !ok {
			return true
		}

		screenX, screenY, visible := ctx.MapToScreen(pos.X, pos.Y)
		if !visible {
			return true
		}

		fg := visual.GlyphColorLUT[glyph.Type][glyph.Level]

		buf.SetFgOnly(screenX, screenY, glyph.Rune, fg, terminal.AttrNone)
		return true
	})
}
