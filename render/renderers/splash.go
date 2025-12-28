package renderers

import (
	"math"
	"strconv"

	"github.com/lixenwraith/vi-fighter/asset"
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// SplashRenderer draws large block characters as background effect
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
func (r *SplashRenderer) Render(gameCtx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(constant.MaskTransient)

	entities := r.splashStore.All()
	for _, entity := range entities {
		splash, ok := r.splashStore.Get(entity)
		if !ok || splash.Length == 0 {
			continue
		}

		// Resolve anchor position
		anchorX, anchorY := r.resolveAnchor(&splash)

		displayColor := r.resolveSplashColor(splash.Color)

		// Use Slot for countdown detection
		if splash.Slot == component.SlotTimer {
			// Timer: render digits from remaining time (ceiling)
			remainingSec := int(math.Ceil(splash.Remaining.Seconds()))

			digits := strconv.Itoa(remainingSec)
			for i, d := range digits {
				charX := anchorX + i*constant.SplashCharWidth
				r.renderChar(gameCtx, buf, d, charX, anchorY, displayColor)
			}
		} else {
			// Transient: render content directly
			for i := 0; i < splash.Length; i++ {
				charX := anchorX + i*constant.SplashCharWidth
				r.renderChar(gameCtx, buf, splash.Content[i], charX, anchorY, displayColor)
			}
		}
	}
}

// resolveAnchor determines the screen position for a splash
func (r *SplashRenderer) resolveAnchor(splash *component.SplashComponent) (int, int) {
	baseX, baseY := splash.AnchorX, splash.AnchorY

	if splash.AnchorEntity != 0 {
		if pos, ok := r.gameCtx.World.Positions.Get(splash.AnchorEntity); ok {
			baseX = pos.X + splash.OffsetX
			baseY = pos.Y + splash.OffsetY
		}
	}

	// Dynamic centering for timer slot based on current digit count
	if splash.Slot == component.SlotTimer {
		// Recalculate centering based on actual length
		// OffsetX was calculated for initial digit count, adjust for current
		currentWidth := splash.Length * constant.SplashCharWidth
		initialWidth := len(strconv.Itoa(int(math.Ceil(splash.Duration.Seconds())))) * constant.SplashCharWidth
		adjustment := (initialWidth - currentWidth) / 2
		baseX += adjustment
	}

	return baseX, baseY
}

// renderChar renders a single splash character bitmap
func (r *SplashRenderer) renderChar(gameCtx render.RenderContext, buf *render.RenderBuffer, char rune, gameX, gameY int, color render.RGB) {
	var bitmap [12]uint16
	// Bounds check and fallback for missing glyphs
	if char < 32 || char > 126 {
		bitmap = asset.SplashFontFallback
	} else {
		bitmap = asset.SplashFont[char-32]
	}

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
	case component.SplashColorWhite:
		return render.RgbTimerWhite
	case component.SplashColorBlossom:
		return render.RgbBlossom
	case component.SplashColorDecay:
		return render.RgbDecay
	default:
		return render.RgbSplashNormal
	}
}