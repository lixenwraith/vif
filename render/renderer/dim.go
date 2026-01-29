package renderer

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
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
func (r *DimRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	if !ctx.IsPaused {
		return
	}
	buf.MutateDim(visual.DimFactor, visual.DimMask)
}