package renderer

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
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
	headers := r.gameCtx.World.Components.Header.GetAllEntities()
	if len(headers) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskComposite)

	for _, anchor := range headers {
		header, ok := r.gameCtx.World.Components.Header.GetComponent(anchor)
		if !ok || header.Behavior != component.BehaviorGold {
			continue
		}

		for _, member := range header.MemberEntries {
			if member.Entity == 0 {
				continue
			}

			pos, ok := r.gameCtx.World.Positions.GetPosition(member.Entity)
			if !ok {
				continue
			}

			glyph, ok := r.gameCtx.World.Components.Glyph.GetComponent(member.Entity)
			if !ok {
				continue
			}

			screenX := ctx.GameXOffset + pos.X
			screenY := ctx.GameYOffset + pos.Y

			if screenX < ctx.GameXOffset || screenX >= ctx.ScreenWidth ||
				screenY < ctx.GameYOffset || screenY >= ctx.GameYOffset+ctx.GameHeight {
				continue
			}

			buf.SetFgOnly(screenX, screenY, glyph.Rune, visual.RgbGlyphGold, terminal.AttrNone)
		}
	}
}