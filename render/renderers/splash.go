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
	gameCtx     *engine.GameContext
	splashStore *engine.Store[components.SplashComponent]
}

// NewSplashRenderer creates a new splash renderer
func NewSplashRenderer(gameCtx *engine.GameContext) *SplashRenderer {
	return &SplashRenderer{
		gameCtx:     gameCtx,
		splashStore: engine.GetStore[components.SplashComponent](gameCtx.World),
	}
}

// Render draws all splash effects to background channel
func (r *SplashRenderer) Render(gameCtx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	buf.SetWriteMask(render.MaskEffect)

	entities := r.splashStore.All()
	for _, entity := range entities {
		splash, ok := r.splashStore.Get(entity)
		if !ok || splash.Length == 0 {
			continue
		}

		// Solid color - no fade effects
		displayColor := r.resolveSplashColor(splash.Color)

		if splash.Mode == components.SplashModePersistent {
			// Countdown timer: render digits from remaining time
			remainingSec := int(splash.Remaining.Seconds())
			if remainingSec < 0 {
				remainingSec = 0
			}

			digits := strconv.Itoa(remainingSec)
			for i, d := range digits {
				charX := splash.AnchorX + i*(constants.SplashCharWidth+constants.SplashCharSpacing)
				r.renderChar(gameCtx, buf, d, charX, splash.AnchorY, displayColor)
			}
		} else {
			// Transient: render content directly
			for i := 0; i < splash.Length; i++ {
				charX := splash.AnchorX + i*(constants.SplashCharWidth+constants.SplashCharSpacing)
				r.renderChar(gameCtx, buf, splash.Content[i], charX, splash.AnchorY, displayColor)
			}
		}
	}
}

// renderChar renders a single splash character bitmap
func (r *SplashRenderer) renderChar(gameCtx render.RenderContext, buf *render.RenderBuffer, char rune, gameX, gameY int, color render.RGB) {
	// Bounds check character
	if char < 32 || char > 126 {
		return
	}

	bitmap := assets.SplashFont[char-32]

	for row := 0; row < constants.SplashCharHeight; row++ {
		screenY := gameCtx.GameY + gameY + row
		if screenY < gameCtx.GameY || screenY >= gameCtx.GameY+gameCtx.GameHeight {
			continue
		}

		rowBits := bitmap[row]
		for col := 0; col < constants.SplashCharWidth; col++ {
			// MSB-first: bit 15 = column 0
			if rowBits&(1<<(15-col)) == 0 {
				continue
			}

			screenX := gameCtx.GameX + gameX + col
			if screenX < gameCtx.GameX || screenX >= gameCtx.GameX+gameCtx.GameWidth {
				continue
			}

			buf.SetBgOnly(screenX, screenY, color)
		}
	}
}

// TODO: refactor, this logic is defined twice to avoid a cyclic dependency
// resolveSplashColor maps the SplashColor enum to actual render.RGB
func (r *SplashRenderer) resolveSplashColor(c components.SplashColor) render.RGB {
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