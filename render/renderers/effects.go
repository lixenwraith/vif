package renderers

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// EffectsRenderer draws decay, cleaners, removal flashes
type EffectsRenderer struct {
	gameCtx *engine.GameContext

	decayStore   *engine.Store[component.DecayComponent]
	flashStore   *engine.Store[component.FlashComponent]
	cleanerStore *engine.Store[component.CleanerComponent]

	cleanerGradient []render.RGB
}

// NewEffectsRenderer creates a new effects renderer with gradient generation
func NewEffectsRenderer(gameCtx *engine.GameContext) *EffectsRenderer {
	e := &EffectsRenderer{
		gameCtx:      gameCtx,
		decayStore:   engine.GetStore[component.DecayComponent](gameCtx.World),
		flashStore:   engine.GetStore[component.FlashComponent](gameCtx.World),
		cleanerStore: engine.GetStore[component.CleanerComponent](gameCtx.World),
	}
	e.buildCleanerGradient()
	return e
}

// buildCleanerGradient builds the gradient for cleaner trail rendering
func (r *EffectsRenderer) buildCleanerGradient() {
	length := constant.CleanerTrailLength

	r.cleanerGradient = make([]render.RGB, length)

	for i := 0; i < length; i++ {
		// Opacity fade from 1.0 to 0.0
		opacity := 1.0 - (float64(i) / float64(length))
		if opacity < 0 {
			opacity = 0
		}
		r.cleanerGradient[i] = render.Scale(render.RgbCleanerBase, opacity)
	}
}

// Render draws all visual effects
func (r *EffectsRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(constant.MaskEffect)
	// Draw decay (only if there are decay entities)
	r.drawDecay(ctx, buf)

	// Draw cleaners
	r.drawCleaners(ctx, buf)

	// Draw removal flashes
	r.drawRemovalFlashes(ctx, buf)
}

// drawDecay draws the decay characters
func (r *EffectsRenderer) drawDecay(ctx render.RenderContext, buf *render.RenderBuffer) {
	decayEntities := r.decayStore.All()

	for _, decayEntity := range decayEntities {
		decay, exists := r.decayStore.Get(decayEntity)
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
func (r *EffectsRenderer) drawCleaners(ctx render.RenderContext, buf *render.RenderBuffer) {
	cleanerEntities := r.cleanerStore.All()

	gradientLen := len(r.cleanerGradient)
	maxGradientIdx := gradientLen - 1

	for _, cleanerEntity := range cleanerEntities {
		cleaner, ok := r.cleanerStore.Get(cleanerEntity)
		if !ok {
			continue
		}

		cl := constant.CleanerTrailLength

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
			color := r.cleanerGradient[gradientIndex]
			buf.SetWithBg(screenX, screenY, cleaner.Char, color, render.RgbBackground)
		}
	}
}

// drawRemovalFlashes draws the brief flash effects when characters are removed
func (r *EffectsRenderer) drawRemovalFlashes(ctx render.RenderContext, buf *render.RenderBuffer) {
	entities := r.flashStore.All()

	for _, entity := range entities {
		flash, ok := r.flashStore.Get(entity)
		if !ok {
			continue
		}

		if flash.Y < 0 || flash.Y >= ctx.GameHeight || flash.X < 0 || flash.X >= ctx.GameWidth {
			continue
		}

		remaining := flash.Remaining
		if remaining <= 0 {
			continue
		}

		// Calculate opacity based on elapsed time (fade from bright to transparent)
		opacity := 1.0 - (float64(remaining) / float64(flash.Duration))
		if opacity < 0.0 {
			opacity = 0.0
		}

		// Flash contribution for additive blending
		flashColor := render.Scale(render.RgbRemovalFlash, opacity)

		screenX := ctx.GameX + flash.X
		screenY := ctx.GameY + flash.Y

		// Use BlendAddFg to brighten the character ONLY (no background wash)
		// BlendAddFg = OpAdd | FlagFg -  Maintains background transparency/color
		buf.Set(screenX, screenY, flash.Char, flashColor, render.RGBBlack, render.BlendAddFg, 1.0, terminal.AttrNone)
	}
}