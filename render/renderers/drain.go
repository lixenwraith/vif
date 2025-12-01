package renderers

import (
	"fmt"

	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// DrainRenderer draws the drain entity with transparent background
type DrainRenderer struct{}

// NewDrainRenderer creates a new drain renderer
func NewDrainRenderer() *DrainRenderer {
	return &DrainRenderer{}
}

// Render draws all drain entities
func (d *DrainRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	// Get all drains
	drainEntities := world.Drains.All()
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

		// Preserve existing background (e.g., Shield)
		cell := buf.Get(screenX, screenY)
		bg := cell.Bg

		buf.SetWithBg(screenX, screenY, constants.DrainChar, render.RgbDrain, bg)
	}
}