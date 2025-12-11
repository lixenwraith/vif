// @focus: #render { ui } #vfx { splash }
package renderers

import (
	"strconv"

	"github.com/lixenwraith/vi-fighter/assets"
	"github.com/lixenwraith/vi-fighter/components"
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

		// Solid color - no fade effects
		displayColor := s.resolveSplashColor(splash.Color)

		if splash.Mode == components.SplashModePersistent {
			// Countdown timer: render digits from remaining time
			remainingSec := int(splash.Remaining.Seconds())
			if remainingSec < 0 {
				remainingSec = 0
			}

			digits := strconv.Itoa(remainingSec)
			for i, r := range digits {
				charX := splash.AnchorX + i*(constants.SplashCharWidth+constants.SplashCharSpacing)
				s.renderChar(ctx, buf, r, charX, splash.AnchorY, displayColor)
			}
		} else {
			// Transient: render content directly
			for i := 0; i < splash.Length; i++ {
				charX := splash.AnchorX + i*(constants.SplashCharWidth+constants.SplashCharSpacing)
				s.renderChar(ctx, buf, splash.Content[i], charX, splash.AnchorY, displayColor)
			}
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

// TODO: refactor, this logic is defined twice to avoid a cyclic dependency
// resolveSplashColor maps the SplashColor enum to actual render.RGB
func (s *SplashRenderer) resolveSplashColor(c components.SplashColor) render.RGB {
	switch c {
	case components.SplashColorNormal:
		return render.RgbSplashNormal
	case components.SplashColorInsert:
		return render.RgbSplashInsert
	case components.SplashColorGreen:
		return render.RgbSequenceGreenNormal
	case components.SplashColorBlue:
		return render.RgbSequenceBlueNormal
	case components.SplashColorRed:
		return render.RgbSequenceRedNormal
	case components.SplashColorGold:
		return render.RgbSequenceGold
	case components.SplashColorNugget:
		return render.RgbNuggetOrange
	default:
		return render.RgbSplashNormal
	}
}