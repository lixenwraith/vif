package renderers

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
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
	defaultStyle := tcell.StyleDefault.Background(render.RgbBackground)

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

		// Draw the drain character with transparent background
		_, bg, _ := buf.DecomposeAt(screenX, screenY)

		if bg == tcell.ColorDefault {
			bg = render.RgbBackground
		}

		drainStyle := defaultStyle.Foreground(render.RgbDrain).Background(bg)
		buf.Set(screenX, screenY, constants.DrainChar, drainStyle)
	}
}