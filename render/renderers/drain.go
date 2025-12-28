package renderers

import (
	"fmt"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// DrainRenderer draws the drain entity with transparent background
type DrainRenderer struct {
	gameCtx    *engine.GameContext
	drainStore *engine.Store[component.DrainComponent]
}

// NewDrainRenderer creates a new drain renderer
func NewDrainRenderer(gameCtx *engine.GameContext) *DrainRenderer {
	return &DrainRenderer{
		gameCtx:    gameCtx,
		drainStore: engine.GetStore[component.DrainComponent](gameCtx.World),
	}
}

// Render draws all drain entities
func (r *DrainRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	// Get all drains
	drainEntities := r.drainStore.All()
	if len(drainEntities) == 0 {
		return
	}

	buf.SetWriteMask(constant.MaskTransient)

	// Iterate on all drains
	for _, drainEntity := range drainEntities {
		// Get current position
		drainPos, ok := r.gameCtx.World.Positions.Get(drainEntity)
		if !ok {
			panic(fmt.Errorf("drain destroyed"))
		}

		// Calculate screen position
		screenX := ctx.GameX + drainPos.X
		screenY := ctx.GameY + drainPos.Y

		// Bounds check
		if screenX < ctx.GameX || screenX >= ctx.Width || screenY < ctx.GameY || screenY >= ctx.GameY+ctx.GameHeight {
			continue
		}

		// Drain floats over backgrounds - use SetFgOnly
		buf.SetFgOnly(screenX, screenY, constant.DrainChar, render.RgbDrain, terminal.AttrNone)
	}
}