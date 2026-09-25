package renderer

import (
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
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
	headers := r.gameCtx.World.Components.Header
	if headers.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskComposite)

	headers.Each(func(_ core.Entity, header *component.HeaderComponent) bool {
		if header.Behavior != component.BehaviorGold {
			return true
		}

		for _, member := range header.MemberEntries {
			if member.Entity == 0 {
				continue
			}

			pos, ok := r.gameCtx.World.Positions.GetPosition(member.Entity)
			if !ok {
				continue
			}

			glyph, ok := r.gameCtx.World.Components.Glyph.GetPtr(member.Entity)
			if !ok {
				continue
			}

			screenX, screenY, visible := ctx.MapToScreen(pos.X, pos.Y)
			if !visible {
				continue
			}

			buf.SetFgOnly(screenX, screenY, glyph.Rune, visual.RgbGlyphGold, terminal.AttrNone)
		}
		return true
	})
}
