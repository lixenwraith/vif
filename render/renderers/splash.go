// FILE: render/renderers/splash.go
package renderers

import (
	"github.com/lixenwraith/vi-fighter/assets"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// SplashRenderer draws large block characters as background effect
type SplashRenderer struct {
	ctx *engine.GameContext
}

// NewSplashRenderer creates a new splash renderer
func NewSplashRenderer(ctx *engine.GameContext) *SplashRenderer {
	return &SplashRenderer{ctx: ctx}
}

// Render draws the splash effect to background channel
func (s *SplashRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	splash, ok := world.Splashes.Get(s.ctx.SplashEntity)
	if !ok || splash.Length == 0 {
		return
	}

	buf.SetWriteMask(render.MaskEffect)

	// Calculate opacity based on elapsed time
	elapsed := ctx.GameTime.UnixNano() - splash.StartNano
	duration := constants.SplashDuration.Nanoseconds()
	opacity := 1.0 - float64(elapsed)/float64(duration)
	if opacity < 0 {
		opacity = 0
	}
	if opacity > 1 {
		opacity = 1
	}

	// Scale color by opacity
	scaledColor := render.Scale(render.RGB(splash.Color), opacity)

	// Render each character
	for i := 0; i < splash.Length; i++ {
		charX := splash.AnchorX + i*(constants.SplashCharWidth+constants.SplashCharSpacing)
		s.renderChar(ctx, buf, splash.Content[i], charX, splash.AnchorY, scaledColor)
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
