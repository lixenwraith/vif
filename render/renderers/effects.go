package renderers

import (
	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// EffectsRenderer draws decay, cleaners, removal flashes, and materializers
type EffectsRenderer struct {
	gameCtx             *engine.GameContext
	cleanerGradient     []tcell.Color
	materializeGradient []tcell.Color
}

// NewEffectsRenderer creates a new effects renderer with gradient generation
func NewEffectsRenderer(gameCtx *engine.GameContext) *EffectsRenderer {
	e := &EffectsRenderer{
		gameCtx: gameCtx,
	}
	e.buildCleanerGradient()
	e.buildMaterializeGradient()
	return e
}

// buildCleanerGradient builds the gradient for cleaner trail rendering
func (e *EffectsRenderer) buildCleanerGradient() {
	length := constants.CleanerTrailLength

	e.cleanerGradient = make([]tcell.Color, length)
	red, green, blue := render.RgbCleanerBase.RGB()

	for i := 0; i < length; i++ {
		// Opacity fade from 1.0 to 0.0
		opacity := 1.0 - (float64(i) / float64(length))
		if opacity < 0 {
			opacity = 0
		}

		rVal := int32(float64(red) * opacity)
		gVal := int32(float64(green) * opacity)
		bVal := int32(float64(blue) * opacity)

		e.cleanerGradient[i] = tcell.NewRGBColor(rVal, gVal, bVal)
	}
}

// buildMaterializeGradient builds the gradient for materialize trail rendering
func (e *EffectsRenderer) buildMaterializeGradient() {
	length := constants.MaterializeTrailLength

	e.materializeGradient = make([]tcell.Color, length)
	red, green, blue := render.RgbMaterialize.RGB()

	for i := 0; i < length; i++ {
		// Opacity fade from 1.0 to 0.0
		opacity := 1.0 - (float64(i) / float64(length))
		if opacity < 0 {
			opacity = 0
		}

		rVal := int32(float64(red) * opacity)
		gVal := int32(float64(green) * opacity)
		bVal := int32(float64(blue) * opacity)

		e.materializeGradient[i] = tcell.NewRGBColor(rVal, gVal, bVal)
	}
}

// Render draws all visual effects
func (e *EffectsRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	defaultStyle := tcell.StyleDefault.Background(render.RgbBackground)

	// Draw decay (only if there are decay entities)
	e.drawDecay(ctx, world, buf, defaultStyle)

	// Draw cleaners
	e.drawCleaners(ctx, world, buf, defaultStyle)

	// Draw removal flashes
	e.drawRemovalFlashes(ctx, world, buf, defaultStyle)

	// Draw materializers
	e.drawMaterializers(ctx, world, buf, defaultStyle)
}

// drawDecay draws the falling decay characters
func (e *EffectsRenderer) drawDecay(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer, defaultStyle tcell.Style) {
	// Direct store access - single component query
	decayEntities := world.Decays.All()

	fgColor := render.RgbDecay

	for _, decayEntity := range decayEntities {
		decay, exists := world.Decays.Get(decayEntity)
		if !exists {
			continue
		}

		// Calculate screen position
		y := int(decay.YPosition)
		if y < 0 || y >= ctx.GameHeight {
			continue
		}

		screenX := ctx.GameX + decay.Column
		screenY := ctx.GameY + y

		if screenX < ctx.GameX || screenX >= ctx.Width || screenY < ctx.GameY || screenY >= ctx.GameY+ctx.GameHeight {
			continue
		}

		// Preserve existing background (e.g., Shield)
		_, bg, _ := buf.DecomposeAt(screenX, screenY)

		if bg == tcell.ColorDefault {
			bg = render.RgbBackground
		}

		// Combine decay foreground with existing background
		decayStyle := defaultStyle.Foreground(fgColor).Background(bg)

		buf.Set(screenX, screenY, decay.Char, decayStyle)
	}
}

// drawCleaners draws the cleaner animation using the trail of grid points
// Cleaners are opaque and render ON TOP of everything (occlude shield)
func (e *EffectsRenderer) drawCleaners(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer, defaultStyle tcell.Style) {
	cleanerEntities := world.Cleaners.All()

	// Calculate gradient length
	gradientLen := len(e.cleanerGradient)
	maxGradientIdx := gradientLen - 1

	for _, cleanerEntity := range cleanerEntities {
		cleaner, ok := world.Cleaners.Get(cleanerEntity)
		if !ok {
			continue
		}

		// Deep copy trail to avoid race conditions during rendering
		trailCopy := make([]core.Point, len(cleaner.Trail))
		copy(trailCopy, cleaner.Trail)

		// Iterate through the trail
		// Index 0 is the head (brightest), last index is the tail (faintest)
		for i, point := range trailCopy {
			// Bounds check both X and Y
			if point.X < 0 || point.X >= ctx.GameWidth || point.Y < 0 || point.Y >= ctx.GameHeight {
				continue
			}

			screenX := ctx.GameX + point.X
			screenY := ctx.GameY + point.Y

			// Use gradient based on index (clamped to valid range)
			gradientIndex := i
			if gradientIndex > maxGradientIdx {
				gradientIndex = maxGradientIdx
			}

			// Apply color from gradient - cleaners are OPAQUE (solid background)
			color := e.cleanerGradient[gradientIndex]
			style := defaultStyle.Foreground(color).Background(render.RgbBackground)

			buf.Set(screenX, screenY, cleaner.Char, style)
		}
	}
}

// drawRemovalFlashes draws the brief flash effects when red characters are removed
func (e *EffectsRenderer) drawRemovalFlashes(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer, defaultStyle tcell.Style) {
	// Use world for direct store access
	entities := world.Flashes.All()

	for _, entity := range entities {
		flash, ok := world.Flashes.Get(entity)
		if !ok {
			continue
		}

		// Check if position is in bounds
		if flash.Y < 0 || flash.Y >= ctx.GameHeight || flash.X < 0 || flash.X >= ctx.GameWidth {
			continue
		}

		// Calculate elapsed time for fade effect
		elapsed := ctx.GameTime.Sub(flash.StartTime).Milliseconds()

		// Skip if flash has expired (cleanup will handle removal)
		if elapsed >= int64(flash.Duration) {
			continue
		}

		// Calculate opacity based on elapsed time (fade from bright to transparent)
		opacity := 1.0 - (float64(elapsed) / float64(flash.Duration))
		if opacity < 0.0 {
			opacity = 0.0
		}

		// Flash color: bright yellow-white fading to yellow
		red := int32(255)
		green := int32(255)
		blue := int32(200 * opacity)

		flashColor := tcell.NewRGBColor(red, green, blue)

		screenX := ctx.GameX + flash.X
		screenY := ctx.GameY + flash.Y

		// Preserve existing background
		_, bg, _ := buf.DecomposeAt(screenX, screenY)

		if bg == tcell.ColorDefault {
			bg = render.RgbBackground
		}

		flashStyle := defaultStyle.Foreground(flashColor).Background(bg)
		buf.Set(screenX, screenY, flash.Char, flashStyle)
	}
}

// drawMaterializers draws the materialize animation using the trail of grid points
func (e *EffectsRenderer) drawMaterializers(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer, defaultStyle tcell.Style) {
	entities := world.Materializers.All()
	if len(entities) == 0 {
		return
	}

	// Pre-calculate gradient length outside loop for performance
	gradientLen := len(e.materializeGradient)
	maxGradientIdx := gradientLen - 1

	for _, entity := range entities {
		mat, ok := world.Materializers.Get(entity)
		if !ok {
			continue
		}

		// Deep copy trail to avoid race conditions during rendering
		trailCopy := make([]core.Point, len(mat.Trail))
		copy(trailCopy, mat.Trail)

		// Iterate through the trail
		// Index 0 is the head (brightest), last index is the tail (faintest)
		for i, point := range trailCopy {
			// Skip if out of bounds
			if point.X < 0 || point.X >= ctx.GameWidth || point.Y < 0 || point.Y >= ctx.GameHeight {
				continue
			}

			screenX := ctx.GameX + point.X
			screenY := ctx.GameY + point.Y

			// Use pre-calculated gradient based on index (clamped to valid range)
			gradientIndex := i
			if gradientIndex > maxGradientIdx {
				gradientIndex = maxGradientIdx
			}

			// Apply color from gradient
			color := e.materializeGradient[gradientIndex]

			// Preserve existing background (e.g., Shield color)
			_, bg, _ := buf.DecomposeAt(screenX, screenY)

			if bg == tcell.ColorDefault {
				bg = render.RgbBackground
			}

			style := defaultStyle.Foreground(color).Background(bg)
			buf.Set(screenX, screenY, mat.Char, style)
		}
	}
}