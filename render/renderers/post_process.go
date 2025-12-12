// @lixen: #focus{render[post,dim,grayout]}
// @lixen: #interact{state[context,config],trigger[buffer]}
package renderers

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// DimRenderer applies brightness reduction to masked cells
type DimRenderer struct {
	gameCtx *engine.GameContext
}

// NewDimRenderer creates a dim post-processor
func NewDimRenderer(ctx *engine.GameContext) *DimRenderer {
	return &DimRenderer{
		gameCtx: ctx,
	}
}

// Render applies dimming when game is paused
func (r *DimRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	if !ctx.IsPaused {
		return
	}
	cfg := engine.MustGetResource[*engine.RenderConfig](world.Resources)
	buf.MutateDim(cfg.DimFactor, cfg.DimMask)
}

// GrayoutRenderer applies desaturation effect based on game state
type GrayoutRenderer struct {
	gameCtx *engine.GameContext
}

// NewGrayoutRenderer creates a grayscale post-processor
func NewGrayoutRenderer(ctx *engine.GameContext) *GrayoutRenderer {
	return &GrayoutRenderer{
		gameCtx: ctx,
	}
}

// Render applies grayscale with intensity from game state
func (r *GrayoutRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	cfg := engine.MustGetResource[*engine.RenderConfig](world.Resources)
	intensity := r.gameCtx.State.GetGrayoutIntensity(ctx.GameTime, cfg.GrayoutDuration)
	if intensity <= 0 {
		return
	}
	buf.MutateGrayscale(intensity, cfg.GrayoutMask)
}