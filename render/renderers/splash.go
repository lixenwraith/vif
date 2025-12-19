package renderers

import (
	"strconv"

	"github.com/lixenwraith/vi-fighter/asset"
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// SplashRenderer draws large block characters as background effect
// Supports multiple concurrent splash entities
type SplashRenderer struct {
	gameCtx     *engine.GameContext
	splashStore *engine.Store[component.SplashComponent]
}

// NewSplashRenderer creates a new splash renderer
func NewSplashRenderer(gameCtx *engine.GameContext) *SplashRenderer {
	return &SplashRenderer{
		gameCtx:     gameCtx,
		splashStore: engine.GetStore[component.SplashComponent](gameCtx.World),
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

		if splash.Mode == component.SplashModePersistent {
			// Countdown timer: render digits from remaining time
			remainingSec := int(splash.Remaining.Seconds())
			if remainingSec < 0 {
				remainingSec = 0
			}

			digits := strconv.Itoa(remainingSec)
			for i, d := range digits {
				charX := splash.AnchorX + i*(constant.SplashCharWidth+constant.SplashCharSpacing)
				r.renderChar(gameCtx, buf, d, charX, splash.AnchorY, displayColor)
			}
		} else {
			// Transient: render content directly
			for i := 0; i < splash.Length; i++ {
				charX := splash.AnchorX + i*(constant.SplashCharWidth+constant.SplashCharSpacing)
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

	bitmap := asset.SplashFont[char-32]

	for row := 0; row < constant.SplashCharHeight; row++ {
		screenY := gameCtx.GameY + gameY + row
		if screenY < gameCtx.GameY || screenY >= gameCtx.GameY+gameCtx.GameHeight {
			continue
		}

		rowBits := bitmap[row]
		for col := 0; col < constant.SplashCharWidth; col++ {
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
func (r *SplashRenderer) resolveSplashColor(c component.SplashColor) render.RGB {
	switch c {
	case component.SplashColorNormal:
		return render.RgbSplashNormal
	case component.SplashColorInsert:
		return render.RgbSplashInsert
	case component.SplashColorGreen:
		return render.RgbSequenceGreenNormal
	case component.SplashColorBlue:
		return render.RgbSequenceBlueNormal
	case component.SplashColorRed:
		return render.RgbSequenceRedNormal
	case component.SplashColorGold:
		return render.RgbSequenceGold
	case component.SplashColorNugget:
		return render.RgbNuggetOrange
	default:
		return render.RgbSplashNormal
	}
}