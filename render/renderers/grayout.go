package renderers

import (
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

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
func (r *GrayoutRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	envEntity := r.gameCtx.World.Resources.Environment.Entity
	envComp, _ := r.gameCtx.World.Components.Environment.GetComponent(envEntity)

	if !envComp.GrayoutActive {
		return
	}

	intensity := envComp.GrayoutIntensity
	if intensity <= 0 {
		return
	}

	buf.MutateGrayscale(intensity, constant.GrayoutMask, constant.MaskPing|constant.MaskField|constant.MaskTransient|constant.MaskComposite|constant.MaskUI)
}