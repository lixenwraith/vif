package renderers

import (
	"math"

	"github.com/gdamore/tcell/v2"
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
		if shield.Sources == 0 {
			continue
		}

		// Check Energy via RenderContext
		if ctx.Energy <= 0 {
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

		// Convert shield color to RGB once
		shieldRGB := render.TcellToRGB(shield.Color)

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

				// Use compositor alpha blending

				// mainRune=0 preserves existing rune
				// BlendAlpha blends only the background
				buf.SetPixel(screenX, screenY, 0, render.RGBBlack, shieldRGB, render.BlendAlpha, alpha, tcell.AttrNone)
			}
		}
	}
}

// blendColors blends two colors based on alpha
// alpha is 0.0 (fully background) to 1.0 (fully foreground)
func (s *ShieldRenderer) blendColors(bg, fg tcell.Color, alpha float64) tcell.Color {
	if alpha <= 0 {
		return bg
	}
	if alpha >= 1 {
		return fg
	}

	// Safeguard: treat ColorDefault as RgbBackground to prevent negative RGB math
	if bg == tcell.ColorDefault {
		bg = render.RgbBackground
	}
	if fg == tcell.ColorDefault {
		fg = render.RgbBackground
	}

	r1, g1, b1 := bg.RGB()
	r2, g2, b2 := fg.RGB()

	rOut := int32(float64(r1)*(1.0-alpha) + float64(r2)*alpha)
	gOut := int32(float64(g1)*(1.0-alpha) + float64(g2)*alpha)
	bOut := int32(float64(b1)*(1.0-alpha) + float64(b2)*alpha)

	return tcell.NewRGBColor(rOut, gOut, bOut)
}