package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
)

// CleanerRenderer draws cleaner entities with gradient trails
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
		opacity := 1.0 - (float64(i) / float64(length))
		if opacity < 0 {
			opacity = 0
		}
		r.gradientPositive[i] = color.Scale(visual.RgbCleanerBasePositive, opacity)
		r.gradientNegative[i] = color.Scale(visual.RgbCleanerBaseNegative, opacity)
		r.gradientNugget[i] = color.Scale(visual.RgbCleanerBaseNugget, opacity)
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

		// Determine visible trail length
		visibleLen := cleaner.TrailLen
		if cleaner.Blocked && cleaner.DrainTotal > 0 {
			// Shrink trail proportionally to drain progress
			ratio := cleaner.DrainRemaining / cleaner.DrainTotal
			if ratio < 0 {
				ratio = 0
			}
			visibleLen = max(int(float64(cleaner.TrailLen)*ratio), visibleLen)
		}

		// Iterate trail ring buffer: index 0 is head (brightest), last is tail (faintest)
		for i := range cleaner.TrailLen {
			// Walk backwards from head in the ring buffer
			idx := (cleaner.TrailHead - i + parameter.CleanerTrailLength) % parameter.CleanerTrailLength
			point := cleaner.TrailRing[idx]

			// Transform map coords to screen coords with visibility check
			screenX, screenY, visible := ctx.MapToScreen(point.X, point.Y)
			if !visible {
				continue
			}

			gradientIndex := min(i, maxGradientIdx)

			// Cleaners are opaque (solid background)
			buf.SetWithBg(screenX, screenY, cleaner.Rune, gradient[gradientIndex], visual.RgbBackground)
		}
		return true
	})
}
