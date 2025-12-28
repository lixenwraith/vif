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

	headerStore   *engine.Store[component.CompositeHeaderComponent]
	typeableStore *engine.Store[component.TypeableComponent]
}

// NewGoldRenderer creates a new gold renderer
func NewGoldRenderer(gameCtx *engine.GameContext) *GoldRenderer {
	return &GoldRenderer{
		gameCtx:       gameCtx,
		headerStore:   engine.GetStore[component.CompositeHeaderComponent](gameCtx.World),
		typeableStore: engine.GetStore[component.TypeableComponent](gameCtx.World),
	}
}

// Render draws all gold sequence members
func (r *GoldRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	headers := r.headerStore.All()
	if len(headers) == 0 {
		return
	}

	buf.SetWriteMask(constant.MaskComposite)

	for _, anchor := range headers {
		header, ok := r.headerStore.Get(anchor)
		if !ok || header.BehaviorID != component.BehaviorGold {
			continue
		}

		for _, member := range header.Members {
			if member.Entity == 0 {
				continue
			}

			pos, hasPos := r.gameCtx.World.Positions.Get(member.Entity)
			if !hasPos {
				continue
			}

			typeable, hasTypeable := r.typeableStore.Get(member.Entity)
			if !hasTypeable {
				continue
			}

			screenX := ctx.GameX + pos.X
			screenY := ctx.GameY + pos.Y

			if screenX < ctx.GameX || screenX >= ctx.Width ||
				screenY < ctx.GameY || screenY >= ctx.GameY+ctx.GameHeight {
				continue
			}

			buf.SetFgOnly(screenX, screenY, typeable.Char, render.RgbSequenceGold, terminal.AttrNone)
		}
	}
}