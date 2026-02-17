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
	grayout := r.gameCtx.World.Resources.Transient.Grayout

	if !grayout.Active || grayout.Intensity <= 0 {
		return
	}

	buf.MutateGrayscale(grayout.Intensity, visual.GrayoutMask, visual.MaskPing|visual.MaskField|visual.MaskTransient|visual.MaskComposite|visual.MaskHealthBar|visual.MaskUI)
}