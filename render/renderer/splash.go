package renderer

import (
	"math"
	"strconv"

	"github.com/lixenwraith/vi-fighter/asset"
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// SplashRenderer draws large block characters as background effect
type SplashRenderer struct {
	gameCtx *engine.GameContext
}

// NewSplashRenderer creates a new splash renderer
func NewSplashRenderer(gameCtx *engine.GameContext) *SplashRenderer {
	return &SplashRenderer{
		gameCtx: gameCtx,
	}
}

// Render draws all splash effects to background channel
func (r *SplashRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(visual.MaskTransient)

	entities := r.gameCtx.World.Components.Splash.GetAllEntities()
	for _, entity := range entities {
		splash, ok := r.gameCtx.World.Components.Splash.GetComponent(entity)
		if !ok || splash.Length == 0 {
			continue
		}

		// Resolve anchor position (map coords)
		anchorX, anchorY := r.resolveAnchor(&splash)

		// Use Slot for countdown detection
		if splash.Slot == component.SlotTimer {
			// Timer: render digits from remaining time (ceiling)
			remainingSec := int(math.Ceil(splash.Remaining.Seconds()))

			digits := strconv.Itoa(remainingSec)
			for i, d := range digits {
				charX := anchorX + i*parameter.SplashCharWidth
				r.renderChar(ctx, buf, d, charX, anchorY, splash.Color)
			}
		} else {
			// Transient: render content directly
			for i := 0; i < splash.Length; i++ {
				charX := anchorX + i*parameter.SplashCharWidth
				r.renderChar(ctx, buf, splash.Content[i], charX, anchorY, splash.Color)
			}
		}
	}
}

// resolveAnchor determines the screen position for a splash
func (r *SplashRenderer) resolveAnchor(splashComp *component.SplashComponent) (int, int) {
	baseX, baseY := splashComp.AnchorX, splashComp.AnchorY

	if splashComp.AnchorEntity != 0 {
		if anchorPos, ok := r.gameCtx.World.Positions.GetPosition(splashComp.AnchorEntity); ok {
			baseX = anchorPos.X + splashComp.OffsetX
			baseY = anchorPos.Y + splashComp.OffsetY
		}
	} else {
		baseX = splashComp.AnchorX
		baseY = splashComp.AnchorY
	}

	return baseX, baseY
}

// renderChar renders a single splash character bitmap
func (r *SplashRenderer) renderChar(ctx render.RenderContext, buf *render.RenderBuffer, char rune, gameX, gameY int, color terminal.RGB) {
	var bitmap [12]uint16
	// Bounds check and fallback for missing glyphs
	if char < 32 || char > 126 {
		bitmap = asset.SplashFontFallback
	} else {
		bitmap = asset.SplashFont[char-32]
	}

	for row := 0; row < parameter.SplashCharHeight; row++ {
		// Map coord for this row
		mapY := gameY + row

		rowBits := bitmap[row]
		for col := 0; col < parameter.SplashCharWidth; col++ {
			// MSB-first: bit 15 = column 0
			if rowBits&(1<<(15-col)) == 0 {
				continue
			}

			// Map coord for this column
			mapX := gameX + col

			// Transform to screen with visibility check
			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			buf.SetBgOnly(screenX, screenY, color)
		}
	}
}