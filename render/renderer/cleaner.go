package renderer

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// CleanerRenderer draws cleaner entities with gradient trails
type CleanerRenderer struct {
	gameCtx *engine.GameContext

	gradientPositive []terminal.RGB
	gradientNegative []terminal.RGB
	gradientNugget   []terminal.RGB
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
	length := parameter.CleanerTrailLength

	r.gradientPositive = make([]terminal.RGB, length)
	r.gradientNegative = make([]terminal.RGB, length)
	r.gradientNugget = make([]terminal.RGB, length)

	for i := 0; i < length; i++ {
		opacity := 1.0 - (float64(i) / float64(length))
		if opacity < 0 {
			opacity = 0
		}
		r.gradientPositive[i] = render.Scale(visual.RgbCleanerBasePositive, opacity)
		r.gradientNegative[i] = render.Scale(visual.RgbCleanerBaseNegative, opacity)
		r.gradientNugget[i] = render.Scale(visual.RgbCleanerBaseNugget, opacity)
	}
}

// Render draws cleaner animation using trail of grid points
func (r *CleanerRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	entities := r.gameCtx.World.Components.Cleaner.GetAllEntities()
	if len(entities) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	gradientLen := len(r.gradientPositive)
	maxGradientIdx := gradientLen - 1

	for _, entity := range entities {
		cleaner, ok := r.gameCtx.World.Components.Cleaner.GetComponent(entity)
		if !ok {
			continue
		}

		// Select gradient based on color type
		var gradient []terminal.RGB
		switch cleaner.ColorType {
		case component.CleanerColorPositive:
			gradient = r.gradientPositive
		case component.CleanerColorNegative:
			gradient = r.gradientNegative
		case component.CleanerColorNugget:
			gradient = r.gradientNugget
		default:
			continue
		}

		// Iterate trail ring buffer: index 0 is head (brightest), last is tail (faintest)
		for i := 0; i < cleaner.TrailLen; i++ {
			// Walk backwards from head in the ring buffer
			idx := (cleaner.TrailHead - i + parameter.CleanerTrailLength) % parameter.CleanerTrailLength
			point := cleaner.TrailRing[idx]

			// Transform map coords to screen coords with visibility check
			screenX, screenY, visible := ctx.MapToScreen(point.X, point.Y)
			if !visible {
				continue
			}

			gradientIndex := i
			if gradientIndex > maxGradientIdx {
				gradientIndex = maxGradientIdx
			}

			// Cleaners are opaque (solid background)
			buf.SetWithBg(screenX, screenY, cleaner.Rune, gradient[gradientIndex], visual.RgbBackground)
		}
	}
}