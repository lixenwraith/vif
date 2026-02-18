package renderer

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

type strobeRenderFunc func(r *StrobeRenderer, ctx render.RenderContext, buf *render.RenderBuffer)

// StrobeRenderer applies screen flash effect to untouched background cells
type StrobeRenderer struct {
	gameCtx    *engine.GameContext
	renderFunc strobeRenderFunc
}

// NewStrobeRenderer creates a strobe post-processor
func NewStrobeRenderer(ctx *engine.GameContext) *StrobeRenderer {
	r := &StrobeRenderer{
		gameCtx: ctx,
	}
	if ctx.World.Resources.Config.ColorMode == terminal.ColorMode256 {
		r.renderFunc = strobeRenderNoop
	} else {
		r.renderFunc = strobeRenderTrueColor
	}
	return r
}

// Render configures background overlay if strobe is active
func (r *StrobeRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	r.renderFunc(r, ctx, buf)
}

func strobeRenderNoop(_ *StrobeRenderer, _ render.RenderContext, _ *render.RenderBuffer) {}

func strobeRenderTrueColor(r *StrobeRenderer, ctx render.RenderContext, buf *render.RenderBuffer) {
	strobe := r.gameCtx.World.Resources.Transient.Strobe
	if !strobe.Active {
		return
	}

	effectiveIntensity := computeEnvelopeIntensity(strobe)
	if effectiveIntensity <= 0 {
		return
	}

	buf.SetBackgroundOverlay(strobe.Color, effectiveIntensity)
}

// computeEnvelopeIntensity calculates intensity based on envelope position
func computeEnvelopeIntensity(s engine.StrobeState) float64 {
	if s.InitialDuration <= 0 {
		// Single-frame flash: full intensity
		return s.Intensity
	}

	elapsed := s.InitialDuration - s.Remaining
	riseDuration := float64(s.InitialDuration) * visual.StrobeRiseRatio
	decayDuration := float64(s.InitialDuration) * visual.StrobeDecayRatio

	elapsedF := float64(elapsed)
	var factor float64

	if elapsedF < riseDuration {
		// Rising phase: linear 0 -> 1
		if riseDuration > 0 {
			factor = elapsedF / riseDuration
		} else {
			factor = 1.0
		}
	} else {
		// Decay phase: linear 1 -> 0
		decayElapsed := elapsedF - riseDuration
		if decayDuration > 0 {
			factor = 1.0 - (decayElapsed / decayDuration)
		} else {
			factor = 0.0
		}
	}

	if factor < 0 {
		factor = 0
	} else if factor > 1 {
		factor = 1
	}

	return s.Intensity * factor
}