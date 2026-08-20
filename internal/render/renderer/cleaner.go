package renderer

import (
	"math"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
)

// CleanerRenderer draws cleaner entities as background-only moving trails.
type CleanerRenderer struct {
	gameCtx *engine.GameContext

	gradientPositive []color.RGB
	gradientNegative []color.RGB
	gradientNugget   []color.RGB
}

// NewCleanerRenderer creates cleaner renderer with gradient generation
func NewCleanerRenderer(gameCtx *engine.GameContext) *CleanerRenderer {
	r := &CleanerRenderer{
		gameCtx: gameCtx,
	}
	r.buildGradients()
	return r
}

// buildGradients builds gradients for cleaner trail rendering
func (r *CleanerRenderer) buildGradients() {
	length := int(parameter.CleanerTrailLength)

	r.gradientPositive = make([]color.RGB, length)
	r.gradientNegative = make([]color.RGB, length)
	r.gradientNugget = make([]color.RGB, length)

	for i := range length {
		t := 0.0
		if length > 1 {
			t = float64(i) / float64(length-1)
		}
		// Reversed smoothstep: a bright moving head with a soft, short tail.
		fade := 1.0 - t
		fade = fade * fade * (3.0 - 2.0*fade)
		intensity := parameter.CleanerTrailTailIntensity +
			(parameter.CleanerTrailHeadIntensity-parameter.CleanerTrailTailIntensity)*fade
		r.gradientPositive[i] = color.Scale(visual.RgbCleanerBasePositive, intensity)
		r.gradientNegative[i] = color.Scale(visual.RgbCleanerBaseNegative, intensity)
		r.gradientNugget[i] = color.Scale(visual.RgbCleanerBaseNugget, intensity)
	}
}

// Render draws cleaner animation using trail of grid points
func (r *CleanerRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	cleaners := r.gameCtx.World.Components.Cleaner
	if cleaners.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	gradientLen := len(r.gradientPositive)
	maxGradientIdx := gradientLen - 1

	cleaners.Each(func(_ core.Entity, cleaner *component.CleanerComponent) bool {
		// Select gradient based on color type
		var gradient []color.RGB
		switch cleaner.ColorType {
		case component.CleanerColorPositive:
			gradient = r.gradientPositive
		case component.CleanerColorNegative:
			gradient = r.gradientNegative
		case component.CleanerColorNugget:
			gradient = r.gradientNugget
		default:
			return true
		}

		visibleLen := cleanerVisibleTrailLen(cleaner)

		// Iterate trail ring buffer: index 0 is head (brightest), last is tail (faintest)
		for i := range visibleLen {
			// Walk backwards from head in the ring buffer
			idx := (cleaner.TrailHead - i + parameter.CleanerTrailLength) % parameter.CleanerTrailLength
			point := cleaner.TrailRing[idx]

			// Transform map coords to screen coords with visibility check
			screenX, screenY, visible := ctx.MapToScreen(point.X, point.Y)
			if !visible {
				continue
			}

			gradientIndex := min(i, maxGradientIdx)

			// Max background blending keeps overlapping auto-fire bounded and steady,
			// while rune/foreground channels remain untouched and readable.
			buf.Set(screenX, screenY, 0, visual.RgbBlack, gradient[gradientIndex],
				render.BlendMaxBg, 1.0, terminal.AttrNone)
		}
		return true
	})
}

func cleanerVisibleTrailLen(cleaner *component.CleanerComponent) int {
	visibleLen := cleaner.TrailLen
	if !cleaner.Blocked || cleaner.DrainTotal <= 0 {
		return visibleLen
	}

	ratio := cleaner.DrainRemaining / cleaner.DrainTotal
	if ratio <= 0 {
		return 0
	}
	if ratio > 1 {
		ratio = 1
	}
	visibleLen = int(math.Ceil(float64(cleaner.TrailLen) * ratio))
	return min(visibleLen, cleaner.TrailLen)
}
