package renderer

import (
	"math"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// TracerRenderer draws the gun's streaks as background-only moving trails, in
// the polarity each was fired with
type TracerRenderer struct {
	gameCtx  *engine.GameContext
	gradient [component.PaletteCount][]color.RGB
}

func NewTracerRenderer(gameCtx *engine.GameContext) *TracerRenderer {
	r := &TracerRenderer{gameCtx: gameCtx}
	base := [component.PaletteCount]color.RGB{
		component.PalettePositive: visual.RgbTracerPositive,
		component.PaletteNegative: visual.RgbTracerNegative,
		component.PaletteHostile:  visual.RgbRayHostile,
	}
	length := int(parameter.TracerTrailLength)
	for p := range r.gradient {
		r.gradient[p] = make([]color.RGB, length)
		for i := range length {
			t := 0.0
			if length > 1 {
				t = float64(i) / float64(length-1)
			}
			// Reversed smoothstep: a bright moving head with a soft, short tail.
			fade := 1.0 - t
			fade = fade * fade * (3.0 - 2.0*fade)
			intensity := parameter.TracerTrailTailIntensity +
				(parameter.TracerTrailHeadIntensity-parameter.TracerTrailTailIntensity)*fade
			r.gradient[p][i] = color.Scale(base[p], intensity)
		}
	}
	return r
}

// Render draws each tracer's trail of grid points, head brightest
func (r *TracerRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	tracers := r.gameCtx.World.Components.Tracer
	if tracers.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	tracers.Each(func(_ core.Entity, tracer *component.TracerComponent) bool {
		if tracer.Palette >= component.PaletteCount {
			return true
		}
		gradient := r.gradient[tracer.Palette]
		for i := range tracerVisibleTrailLen(tracer) {
			idx := (tracer.TrailHead - i + parameter.TracerTrailLength) % parameter.TracerTrailLength
			point := tracer.TrailRing[idx]

			screenX, screenY, visible := ctx.MapToScreen(point.X, point.Y)
			if !visible {
				continue
			}

			// Max background blending keeps overlapping auto-fire bounded and steady,
			// while rune/foreground channels remain untouched and readable.
			buf.Set(screenX, screenY, 0, visual.RgbBlack, gradient[min(i, len(gradient)-1)],
				render.BlendMaxBg, 1.0, terminal.AttrNone)
		}
		return true
	})
}

func tracerVisibleTrailLen(tracer *component.TracerComponent) int {
	visibleLen := tracer.TrailLen
	if !tracer.Blocked || tracer.DrainTotal <= 0 {
		return visibleLen
	}

	ratio := tracer.DrainRemaining / tracer.DrainTotal
	if ratio <= 0 {
		return 0
	}
	if ratio > 1 {
		ratio = 1
	}
	visibleLen = int(math.Ceil(float64(tracer.TrailLen) * ratio))
	return min(visibleLen, tracer.TrailLen)
}
