package renderer

import (
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
)

type strobeRenderFunc func(r *StrobeRenderer, ctx render.RenderContext, buf *render.RenderBuffer)

// StrobeRenderer applies screen flash effect to untouched background cells
type StrobeRenderer struct {
	gameCtx    *engine.GameContext
	renderFunc strobeRenderFunc
	console    *render.Console
}

// NewStrobeRenderer creates a strobe post-processor
func NewStrobeRenderer(ctx *engine.GameContext) *StrobeRenderer {
	r := &StrobeRenderer{
		gameCtx: ctx,
	}
	if cfg := ctx.World.Resources.Config; cfg.ColorMode == terminal.ColorMode256 {
		r.renderFunc, r.console = strobeRender256, render.ConsoleFor(cfg.ConsolePalette)
	} else {
		r.renderFunc = strobeRenderTrueColor
	}
	return r
}

// Render configures background overlay if strobe is active
func (r *StrobeRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	r.renderFunc(r, ctx, buf)
}

// strobeRender256 flashes in one step, while the envelope is past half its peak, in the console
// background nearest the strobe color: a console cannot fade between its colors
func strobeRender256(r *StrobeRenderer, _ render.RenderContext, buf *render.RenderBuffer) {
	strobe := r.gameCtx.World.Resources.View.Strobe
	if !strobe.Active || computeEnvelopeIntensity(strobe) < strobe.Intensity/2 {
		return
	}
	buf.SetBackgroundOverlay(r.console.Color(r.console.NearestBackground(strobe.Color)), 1)
}

func strobeRenderTrueColor(r *StrobeRenderer, ctx render.RenderContext, buf *render.RenderBuffer) {
	strobe := r.gameCtx.World.Resources.View.Strobe
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
