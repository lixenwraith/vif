package renderer

import (
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
)

// DimRenderer applies brightness reduction to masked cells
type DimRenderer struct {
	gameCtx *engine.GameContext
	dim     func(buf *render.RenderBuffer)
}

// NewDimRenderer creates a dim post-processor
func NewDimRenderer(ctx *engine.GameContext) *DimRenderer {
	r := &DimRenderer{gameCtx: ctx, dim: dimTrueColor}
	if ctx.World.Resources.Config.ColorMode == terminal.ColorMode256 {
		r.dim = dim256
	}
	return r
}

// Render applies dimming when game is paused
func (r *DimRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	if !ctx.IsPaused {
		return
	}
	r.dim(buf)
}

func dimTrueColor(buf *render.RenderBuffer) { buf.MutateDim(visual.DimFactor, visual.DimMask) }

// dim256 drains the paused world of color instead: a console shows most halved colors as black
func dim256(buf *render.RenderBuffer) { buf.MutateGrayscale(1, visual.DimMask, 0) }
