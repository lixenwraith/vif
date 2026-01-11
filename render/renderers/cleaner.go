package renderers

import (
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// CleanerRenderer draws cleaner entities with gradient trails
type CleanerRenderer struct {
	gameCtx *engine.GameContext

	gradientPositive []render.RGB
	gradientNegative []render.RGB
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
	length := constant.CleanerTrailLength

	r.gradientPositive = make([]render.RGB, length)
	r.gradientNegative = make([]render.RGB, length)

	for i := 0; i < length; i++ {
		opacity := 1.0 - (float64(i) / float64(length))
		if opacity < 0 {
			opacity = 0
		}
		r.gradientPositive[i] = render.Scale(render.RgbCleanerBasePositive, opacity)
		r.gradientNegative[i] = render.Scale(render.RgbCleanerBaseNegative, opacity)
	}
}

// Render draws cleaner animation using trail of grid points
// Cleaners are opaque and render ON TOP of everything (occlude shield)
func (r *CleanerRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	entities := r.gameCtx.World.Components.Cleaner.AllEntities()
	if len(entities) == 0 {
		return
	}

	buf.SetWriteMask(constant.MaskTransient)

	gradientLen := len(r.gradientPositive)
	maxGradientIdx := gradientLen - 1

	for _, entity := range entities {
		cleaner, ok := r.gameCtx.World.Components.Cleaner.GetComponent(entity)
		if !ok {
			continue
		}

		// Select gradient based on energy polarity at spawn
		gradient := r.gradientPositive
		if cleaner.NegativeEnergy {
			gradient = r.gradientNegative
		}

		// Iterate trail ring buffer: index 0 is head (brightest), last is tail (faintest)
		for i := 0; i < cleaner.TrailLen; i++ {
			// Walk backwards from head in the ring buffer
			idx := (cleaner.TrailHead - i + constant.CleanerTrailLength) % constant.CleanerTrailLength
			point := cleaner.TrailRing[idx]

			if point.X < 0 || point.X >= ctx.GameWidth || point.Y < 0 || point.Y >= ctx.GameHeight {
				continue
			}

			screenX := ctx.GameX + point.X
			screenY := ctx.GameY + point.Y

			gradientIndex := i
			if gradientIndex > maxGradientIdx {
				gradientIndex = maxGradientIdx
			}

			// Cleaners are opaque (solid background)
			buf.SetWithBg(screenX, screenY, cleaner.Char, gradient[gradientIndex], render.RgbBackground)
		}
	}
}