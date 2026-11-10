package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// FlashRenderer draws the cells flashing away
type FlashRenderer struct {
	gameCtx *engine.GameContext
}

func NewFlashRenderer(gameCtx *engine.GameContext) *FlashRenderer {
	return &FlashRenderer{
		gameCtx: gameCtx,
	}
}

// Render draws each batch's glyphs in the removal flash's color, fading as one
func (r *FlashRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	world := r.gameCtx.World
	flashes := world.Components.Flash
	if flashes.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	flashes.Each(func(e core.Entity, flash *component.FlashComponent) bool {
		progress, ok := deadlineProgress(ctx, world, e)
		if !ok {
			return true
		}
		// Additive on the foreground alone, which keeps the cell's background
		flashColor := color.Scale(visual.RgbRemovalFlash, 1-progress)
		for i := range flash.Cells {
			cell := &flash.Cells[i]
			if screenX, screenY, visible := ctx.MapToScreen(cell.X, cell.Y); visible {
				buf.Set(screenX, screenY, cell.Rune, flashColor, visual.RgbBlack, render.BlendAddFg, 1.0, terminal.AttrNone)
			}
		}
		return true
	})
}

// deadlineProgress is how far e's deadline has run at the frame's moment, which
// stands part of a tick past the tick that last aged it; false once it has run out
func deadlineProgress(ctx render.RenderContext, world *engine.World, e core.Entity) (float64, bool) {
	timer, ok := world.Components.Timer.GetPtr(e)
	if !ok || !timer.Running(component.TimerDeadline) {
		return 0, false
	}
	since, _ := sinceTick(ctx, world.Resources.Time)
	deadline := timer.Slots[component.TimerDeadline]
	return min(float64(deadline.Duration-deadline.Remaining+since)/float64(deadline.Duration), 1), true
}
