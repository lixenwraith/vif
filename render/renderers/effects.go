package renderers

import (
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// EffectsRenderer draws decay, cleaners, removal flashes, and materializers
type EffectsRenderer struct {
	gameCtx             *engine.GameContext
	cleanerGradient     []render.RGB
	materializeGradient []render.RGB
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

	e.cleanerGradient = make([]render.RGB, length)

	for i := 0; i < length; i++ {
		// Opacity fade from 1.0 to 0.0
		opacity := 1.0 - (float64(i) / float64(length))
		if opacity < 0 {
			opacity = 0
		}
		e.cleanerGradient[i] = render.Scale(render.RgbCleanerBase, opacity)
	}
}

// buildMaterializeGradient builds the gradient for materialize trail rendering
func (e *EffectsRenderer) buildMaterializeGradient() {
	length := constants.MaterializeTrailLength

	e.materializeGradient = make([]render.RGB, length)

	for i := 0; i < length; i++ {
		// Opacity fade from 1.0 to 0.0
		opacity := 1.0 - (float64(i) / float64(length))
		if opacity < 0 {
			opacity = 0
		}
		e.materializeGradient[i] = render.Scale(render.RgbMaterialize, opacity)
	}
}

// Render draws all visual effects
func (e *EffectsRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	// Draw decay (only if there are decay entities)
	e.drawDecay(ctx, world, buf)

	// Draw cleaners
	e.drawCleaners(ctx, world, buf)

	// Draw removal flashes
	e.drawRemovalFlashes(ctx, world, buf)

	// Draw materializers
	e.drawMaterializers(ctx, world, buf)
}

// drawDecay draws the decay characters
func (e *EffectsRenderer) drawDecay(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	decayEntities := world.Decays.All()

	for _, decayEntity := range decayEntities {
		decay, exists := world.Decays.Get(decayEntity)
		if !exists {
			continue
		}

		// Convert PreciseX/Y (float overlay) to screen coordinates for rendering
		screenCol := int(decay.PreciseX)
		screenRow := int(decay.PreciseY)

		// Bounds check: game area coordinates
		if screenRow < 0 || screenRow >= ctx.GameHeight {
			continue
		}
		if screenCol < 0 || screenCol >= ctx.GameWidth {
			continue
		}

		// Translate game coordinates to screen coordinates (apply game area offset)
		screenX := ctx.GameX + screenCol
		screenY := ctx.GameY + screenRow

		// Bounds check: screen coordinates
		if screenX < ctx.GameX || screenX >= ctx.Width || screenY < ctx.GameY || screenY >= ctx.GameY+ctx.GameHeight {
			continue
		}

		buf.SetFgOnly(screenX, screenY, decay.Char, render.RgbDecay, terminal.AttrNone)
	}
}

// drawCleaners draws the cleaner animation using the trail of grid points
// Cleaners are opaque and render ON TOP of everything (occlude shield)
func (e *EffectsRenderer) drawCleaners(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	cleanerEntities := world.Cleaners.All()

	gradientLen := len(e.cleanerGradient)
	maxGradientIdx := gradientLen - 1

	for _, cleanerEntity := range cleanerEntities {
		cleaner, ok := world.Cleaners.Get(cleanerEntity)
		if !ok {
			continue
		}

		cl := constants.CleanerTrailLength

		// Iterate through the trail
		// Index 0 is the head (brightest), last index is the tail (faintest)
		for i := 0; i < cleaner.TrailLen; i++ {
			// Walk backwards from head in the ring buffer
			idx := (cleaner.TrailHead - i + cl) % cl
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

			// Cleaners are OPAQUE (solid background)
			color := e.cleanerGradient[gradientIndex]
			buf.SetWithBg(screenX, screenY, cleaner.Char, color, render.RgbBackground)
		}
	}
}

// drawRemovalFlashes draws the brief flash effects when red characters are removed
func (e *EffectsRenderer) drawRemovalFlashes(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	entities := world.Flashes.All()

	for _, entity := range entities {
		flash, ok := world.Flashes.Get(entity)
		if !ok {
			continue
		}

		if flash.Y < 0 || flash.Y >= ctx.GameHeight || flash.X < 0 || flash.X >= ctx.GameWidth {
			continue
		}

		elapsed := ctx.GameTime.Sub(flash.StartTime)
		if elapsed >= flash.Duration {
			continue
		}

		// Calculate opacity based on elapsed time (fade from bright to transparent)
		opacity := 1.0 - (float64(elapsed) / float64(flash.Duration))
		if opacity < 0.0 {
			opacity = 0.0
		}

		// Flash contribution for additive blending
		// TODO: remove hard-coded colors
		flashColor := render.RGB{
			R: uint8(255 * opacity),
			G: uint8(255 * opacity),
			B: uint8(200 * opacity),
		}

		screenX := ctx.GameX + flash.X
		screenY := ctx.GameY + flash.Y

		// Use BlendAdd to brighten underlying pixels (write-only)
		buf.Set(screenX, screenY, flash.Char, flashColor, flashColor, render.BlendAdd, 1.0, terminal.AttrNone)
	}
}

// drawMaterializers draws the materialize animation using the trail of grid points
func (e *EffectsRenderer) drawMaterializers(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	entities := world.Materializers.All()
	if len(entities) == 0 {
		return
	}

	gradientLen := len(e.materializeGradient)
	maxGradientIdx := gradientLen - 1

	for _, entity := range entities {
		mat, ok := world.Materializers.Get(entity)
		if !ok {
			continue
		}

		ml := constants.MaterializeTrailLength

		// Iterate through the trail
		// Index 0 is the head (brightest), last index is the tail (faintest)
		for i := 0; i < mat.TrailLen; i++ {
			// Walk backwards from head in the ring buffer
			idx := (mat.TrailHead - i + ml) % ml
			point := mat.TrailRing[idx]

			if point.X < 0 || point.X >= ctx.GameWidth || point.Y < 0 || point.Y >= ctx.GameHeight {
				continue
			}

			screenX := ctx.GameX + point.X
			screenY := ctx.GameY + point.Y

			gradientIndex := i
			if gradientIndex > maxGradientIdx {
				gradientIndex = maxGradientIdx
			}

			color := e.materializeGradient[gradientIndex]

			// BlendMax with SrcBg=Black preserves destination bg: Max(Dst, 0) = Dst
			buf.Set(screenX, screenY, mat.Char, color, render.RGBBlack, render.BlendMax, 1.0, terminal.AttrNone)
		}
	}
}