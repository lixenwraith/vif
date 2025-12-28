package renderers

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// NuggetRenderer draws the collectible nugget entity
type NuggetRenderer struct {
	gameCtx *engine.GameContext

	nuggetStore   *engine.Store[component.NuggetComponent]
	typeableStore *engine.Store[component.TypeableComponent]
}

// NewNuggetRenderer creates a new nugget renderer
func NewNuggetRenderer(gameCtx *engine.GameContext) *NuggetRenderer {
	return &NuggetRenderer{
		gameCtx:       gameCtx,
		nuggetStore:   engine.GetStore[component.NuggetComponent](gameCtx.World),
		typeableStore: engine.GetStore[component.TypeableComponent](gameCtx.World),
	}
}

// Render draws all nugget entities
func (r *NuggetRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	entities := r.nuggetStore.All()
	if len(entities) == 0 {
		return
	}

	buf.SetWriteMask(constant.MaskTypeable)

	for _, entity := range entities {
		pos, hasPos := r.gameCtx.World.Positions.Get(entity)
		if !hasPos {
			continue
		}

		typeable, hasTypeable := r.typeableStore.Get(entity)
		if !hasTypeable {
			continue
		}

		screenX := ctx.GameX + pos.X
		screenY := ctx.GameY + pos.Y

		if screenX < ctx.GameX || screenX >= ctx.Width ||
			screenY < ctx.GameY || screenY >= ctx.GameY+ctx.GameHeight {
			continue
		}

		buf.SetFgOnly(screenX, screenY, typeable.Char, render.RgbNuggetOrange, terminal.AttrNone)
	}
}