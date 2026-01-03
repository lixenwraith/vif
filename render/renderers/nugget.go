package renderers

import (
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// NuggetRenderer draws the collectible nugget entity
type NuggetRenderer struct {
	gameCtx *engine.GameContext
}

// NewNuggetRenderer creates a new nugget renderer
func NewNuggetRenderer(gameCtx *engine.GameContext) *NuggetRenderer {
	return &NuggetRenderer{
		gameCtx: gameCtx,
	}
}

// Render draws all nugget entities
func (r *NuggetRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	entities := r.gameCtx.World.Components.Nugget.All()
	if len(entities) == 0 {
		return
	}

	buf.SetWriteMask(constant.MaskGlyph)

	for _, entity := range entities {
		pos, hasPos := r.gameCtx.World.Positions.Get(entity)
		if !hasPos {
			continue
		}

		glyph, hasGlyph := r.gameCtx.World.Components.Glyph.Get(entity)
		if !hasGlyph {
			continue
		}

		screenX := ctx.GameX + pos.X
		screenY := ctx.GameY + pos.Y

		if screenX < ctx.GameX || screenX >= ctx.Width ||
			screenY < ctx.GameY || screenY >= ctx.GameY+ctx.GameHeight {
			continue
		}

		buf.SetFgOnly(screenX, screenY, glyph.Rune, render.RgbNuggetOrange, terminal.AttrNone)
	}
}