// FILE: render/renderers/post_process.go
package renderers

import (
	"time"

	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// DimRenderer applies brightness reduction to masked cells
type DimRenderer struct {
	gameCtx    *engine.GameContext
	factor     float64
	targetMask uint8
}

// NewDimRenderer creates a dim post-processor
// factor: brightness multiplier (0.0-1.0), targetMask: cells to affect
func NewDimRenderer(ctx *engine.GameContext, factor float64, targetMask uint8) *DimRenderer {
	return &DimRenderer{
		gameCtx:    ctx,
		factor:     factor,
		targetMask: targetMask,
	}
}

// Render applies dimming when game is paused
func (r *DimRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	if !ctx.IsPaused {
		return
	}
	buf.MutateDim(r.factor, r.targetMask)
}

// GrayoutRenderer applies desaturation effect based on game state
type GrayoutRenderer struct {
	gameCtx    *engine.GameContext
	duration   time.Duration
	targetMask uint8
}

// NewGrayoutRenderer creates a grayscale post-processor
// duration: fade-out time, targetMask: cells to affect
func NewGrayoutRenderer(ctx *engine.GameContext, duration time.Duration, targetMask uint8) *GrayoutRenderer {
	return &GrayoutRenderer{
		gameCtx:    ctx,
		duration:   duration,
		targetMask: targetMask,
	}
}

// Render applies grayscale with intensity from game state
func (r *GrayoutRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	intensity := r.gameCtx.State.GetGrayoutIntensity(ctx.GameTime, r.duration)
	if intensity <= 0 {
		return
	}
	buf.MutateGrayscale(intensity, r.targetMask)
}
