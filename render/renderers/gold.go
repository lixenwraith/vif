package renderers

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// GoldRenderer draws gold sequence composite entities
type GoldRenderer struct {
	gameCtx *engine.GameContext
}

// NewGoldRenderer creates a new gold renderer
func NewGoldRenderer(gameCtx *engine.GameContext) *GoldRenderer {
	return &GoldRenderer{
		gameCtx: gameCtx,
	}
}

// Render draws all gold sequence members
func (r *GoldRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	headers := r.gameCtx.World.Components.Header.AllEntity()
	if len(headers) == 0 {
		return
	}

	buf.SetWriteMask(constant.MaskComposite)

	for _, anchor := range headers {
		header, ok := r.gameCtx.World.Components.Header.GetComponent(anchor)
		if !ok || header.BehaviorID != component.BehaviorGold {
			continue
		}

		for _, member := range header.MemberEntries {
			if member.Entity == 0 {
				continue
			}

			pos, ok := r.gameCtx.World.Positions.Get(member.Entity)
			if !ok {
				continue
			}

			glyph, hasGlyph := r.gameCtx.World.Components.Glyph.GetComponent(member.Entity)
			if !hasGlyph {
				continue
			}

			screenX := ctx.GameX + pos.X
			screenY := ctx.GameY + pos.Y

			if screenX < ctx.GameX || screenX >= ctx.Width ||
				screenY < ctx.GameY || screenY >= ctx.GameY+ctx.GameHeight {
				continue
			}

			buf.SetFgOnly(screenX, screenY, glyph.Rune, render.RgbSequenceGold, terminal.AttrNone)
		}
	}
}