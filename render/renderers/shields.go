package renderers

import (
	"math"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// ShieldRenderer renders active shields by blending their color with the existing background
type ShieldRenderer struct{}

// NewShieldRenderer creates a new shield renderer
func NewShieldRenderer() *ShieldRenderer {
	return &ShieldRenderer{}
}

// Render draws all active shields
func (s *ShieldRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	shields := world.Shields.All()

	for _, entity := range shields {
		shield, okS := world.Shields.Get(entity)
		pos, okP := world.Positions.Get(entity)

		if !okS || !okP {
			continue
		}

		// Shield only renders if Sources != 0 AND Energy > 0
		if shield.Sources == 0 || ctx.Energy <= 0 {
			continue
		}

		// Bounding box
		startX := int(float64(pos.X) - shield.RadiusX)
		endX := int(float64(pos.X) + shield.RadiusX)
		startY := int(float64(pos.Y) - shield.RadiusY)
		endY := int(float64(pos.Y) + shield.RadiusY)

		// Clamp to screen bounds
		if startX < 0 {
			startX = 0
		}
		if endX >= ctx.GameWidth {
			endX = ctx.GameWidth - 1
		}
		if startY < 0 {
			startY = 0
		}
		if endY >= ctx.GameHeight {
			endY = ctx.GameHeight - 1
		}

		// Resolve shield color from semantic ColorClass
		shieldRGB := resolveShieldColor(shield.Color)

		for y := startY; y <= endY; y++ {
			for x := startX; x <= endX; x++ {
				screenX := ctx.GameX + x
				screenY := ctx.GameY + y

				dx := float64(x - pos.X)
				dy := float64(y - pos.Y)

				// Elliptical distance: (dx/rx)^2 + (dy/ry)^2
				normalizedDistSq := (dx*dx)/(shield.RadiusX*shield.RadiusX) + (dy*dy)/(shield.RadiusY*shield.RadiusY)

				if normalizedDistSq > 1.0 {
					continue // Outside shield
				}

				dist := math.Sqrt(normalizedDistSq)

				// Alpha: 1.0 at center, 0.0 at edge, scaled by MaxOpacity
				alpha := (1.0 - dist) * shield.MaxOpacity

				// BlendAlpha on background only, rune=0 preserves existing text
				buf.Set(screenX, screenY, 0, render.RGBBlack, shieldRGB, render.BlendAlpha, alpha, tcell.AttrNone)
			}
		}
	}
}

// resolveShieldColor maps semantic color info to concrete RGB
func resolveShieldColor(color components.ColorClass) render.RGB {
	switch color {
	case components.ColorShield:
		return render.RgbShieldBase
	default:
		return render.RgbShieldBase
	}
}