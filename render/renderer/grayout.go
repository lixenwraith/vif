package renderer

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
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
	envEntities := r.gameCtx.World.Components.Environment.GetAllEntities()
	if len(envEntities) == 0 {
		return
	}
	envEntity := envEntities[0]
	envComp, ok := r.gameCtx.World.Components.Environment.GetComponent(envEntity)
	if !ok {
		return
	}

	if !envComp.GrayoutActive {
		return
	}

	intensity := envComp.GrayoutIntensity
	if intensity <= 0 {
		return
	}

	buf.MutateGrayscale(intensity, visual.GrayoutMask, visual.MaskPing|visual.MaskField|visual.MaskTransient|visual.MaskComposite|visual.MaskHealthBar|visual.MaskUI)
}