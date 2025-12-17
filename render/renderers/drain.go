package renderers

import (
	"fmt"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// DrainRenderer draws the drain entity with transparent background
type DrainRenderer struct {
	gameCtx    *engine.GameContext
	drainStore *engine.Store[components.DrainComponent]
}

// NewDrainRenderer creates a new drain renderer
func NewDrainRenderer(gameCtx *engine.GameContext) *DrainRenderer {
	return &DrainRenderer{
		gameCtx:    gameCtx,
		drainStore: engine.GetStore[components.DrainComponent](gameCtx.World),
	}
}

// Render draws all drain entities
func (r *DrainRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	buf.SetWriteMask(render.MaskEffect)
	// Get all drains
	drainEntities := r.drainStore.All()
	if len(drainEntities) == 0 {
		return
	}

	// Iterate on all drains
	for _, drainEntity := range drainEntities {
		// Get current position
		drainPos, ok := world.Positions.Get(drainEntity)
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
		buf.SetFgOnly(screenX, screenY, constants.DrainChar, render.RgbDrain, terminal.AttrNone)
	}
}