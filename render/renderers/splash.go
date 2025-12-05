package renderers

import (
	"github.com/lixenwraith/vi-fighter/assets"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// SplashRenderer draws large block characters as background effect
// Supports multiple concurrent splash entities
type SplashRenderer struct {
	ctx *engine.GameContext
}

// NewSplashRenderer creates a new splash renderer
func NewSplashRenderer(ctx *engine.GameContext) *SplashRenderer {
	return &SplashRenderer{ctx: ctx}
}

// Render draws all splash effects to background channel
func (s *SplashRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	buf.SetWriteMask(render.MaskEffect)

	entities := world.Splashes.All()
	for _, entity := range entities {
		splash, ok := world.Splashes.Get(entity)
		if !ok || splash.Length == 0 {
			continue
		}

		// Calculate opacity based on elapsed time relative to StartNano and Duration
		elapsed := ctx.GameTime.UnixNano() - splash.StartNano

		// Safe divide check
		if splash.Duration <= 0 {
			continue
		}

		ratio := float64(elapsed) / float64(splash.Duration)

		// Animation curve: Linear fade out (1.0 -> 0.0)
		opacity := 1.0 - ratio

		if opacity < 0 {
			opacity = 0
		}
		if opacity > 1 {
			opacity = 1
		}

		// Scale color by opacity
		scaledColor := render.Scale(render.RGB(splash.Color), opacity)

		// Render each character in the splash
		for i := 0; i < splash.Length; i++ {
			charX := splash.AnchorX + i*(constants.SplashCharWidth+constants.SplashCharSpacing)
			s.renderChar(ctx, buf, splash.Content[i], charX, splash.AnchorY, scaledColor)
		}
	}
}

// renderChar renders a single splash character bitmap
func (s *SplashRenderer) renderChar(ctx render.RenderContext, buf *render.RenderBuffer, char rune, gameX, gameY int, color render.RGB) {
	// Bounds check character
	if char < 32 || char > 126 {
		return
	}

	bitmap := assets.SplashFont[char-32]

	for row := 0; row < constants.SplashCharHeight; row++ {
		screenY := ctx.GameY + gameY + row
		if screenY < ctx.GameY || screenY >= ctx.GameY+ctx.GameHeight {
			continue
		}

		rowBits := bitmap[row]
		for col := 0; col < constants.SplashCharWidth; col++ {
			// MSB-first: bit 15 = column 0
			if rowBits&(1<<(15-col)) == 0 {
				continue
			}

			screenX := ctx.GameX + gameX + col
			if screenX < ctx.GameX || screenX >= ctx.GameX+ctx.GameWidth {
				continue
			}

			buf.SetBgOnly(screenX, screenY, color)
		}
	}
}