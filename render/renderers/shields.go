package renderers

import (
	"math"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// shieldCellRenderer is the callback type for per-cell shield rendering
// Defines the interface for rendering strategy (256-color vs TrueColor) selected initialization
type shieldCellRenderer func(buf *render.RenderBuffer, screenX, screenY int, dist float64, color render.RGB, maxOpacity float64)

// ShieldRenderer renders active shields with dynamic color from GameState
type ShieldRenderer struct {
	gameCtx    *engine.GameContext
	renderCell shieldCellRenderer
}

// NewShieldRenderer creates a new shield renderer
func NewShieldRenderer(gameCtx *engine.GameContext) *ShieldRenderer {
	s := &ShieldRenderer{gameCtx: gameCtx}

	// Access RenderConfig for display mode and select strategy once
	cfg := engine.MustGetResource[*engine.RenderConfig](gameCtx.World.Resources)

	if cfg.ColorMode == uint8(terminal.ColorMode256) {
		s.renderCell = s.cell256
	} else {
		s.renderCell = s.cellTrueColor
	}

	return s
}

// Render draws all active shields with quadratic falloff gradient
func (s *ShieldRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	buf.SetWriteMask(render.MaskShield)
	shields := world.Shields.All()
	if len(shields) == 0 {
		return
	}

	for _, entity := range shields {
		shield, okS := world.Shields.Get(entity)
		pos, okP := world.Positions.Get(entity)

		if !okS || !okP {
			continue
		}

		if !shield.Active {
			continue
		}

		// Resolve shield color
		shieldRGB := s.resolveShieldColor(shield)

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

		// Precompute inverse radii squared for ellipse calculation
		invRxSq := 1.0 / (shield.RadiusX * shield.RadiusX)
		invRySq := 1.0 / (shield.RadiusY * shield.RadiusY)

		for y := startY; y <= endY; y++ {
			for x := startX; x <= endX; x++ {
				// Skip cursor position
				if x == ctx.CursorX && y == ctx.CursorY {
					continue
				}

				dx := float64(x - pos.X)
				dy := float64(y - pos.Y)

				// Elliptical normalized distance squared
				normalizedDistSq := dx*dx*invRxSq + dy*dy*invRySq
				if normalizedDistSq > 1.0 {
					continue
				}

				dist := math.Sqrt(normalizedDistSq)
				screenX := ctx.GameX + x
				screenY := ctx.GameY + y

				// Use pre-selected strategy
				s.renderCell(buf, screenX, screenY, dist, shieldRGB, shield.MaxOpacity)
			}
		}
	}
}

// cellTrueColor renders a single shield cell with smooth gradient (TrueColor mode)
func (s *ShieldRenderer) cellTrueColor(buf *render.RenderBuffer, screenX, screenY int, dist float64, color render.RGB, maxOpacity float64) {
	// Simple quadratic gradient: Dark center -> Bright edge
	// dist ranges from 0.0 (center) to 1.0 (edge)
	// Squared curve (dist^2) keeps the center transparent/dark for text visibility,
	// while ramping up smoothly to maximum intensity at the very edge
	// This eliminates the "blocky" fade-out and ensures the rim is the brightest part
	alpha := (dist * dist) * maxOpacity

	// Use BlendScreen for glowing effect on dark backgrounds
	buf.Set(screenX, screenY, 0, render.RGBBlack, color, render.BlendScreen, alpha, terminal.AttrNone)
}

// cell256 renders a single shield cell with discrete zones (256-color mode)
func (s *ShieldRenderer) cell256(buf *render.RenderBuffer, screenX, screenY int, dist float64, color render.RGB, maxOpacity float64) {
	// In 256-color mode, center is transparent to ensure text legibility
	// dist ranges from 0.0 (center) to 1.0 (edge)
	if dist < 0.6 {
		return // Transparent center: Don't touch the buffer
	}

	// Draw rim using Screen blend
	// Tint background color (e.g. Black -> Shield Color) if empty, or lightens existing background if occupied, ensuring text remains visible
	// High alpha (0.6) to ensure the color registers clearly in the 256-color palette lookup
	buf.Set(screenX, screenY, 0, render.RGBBlack, color, render.BlendScreen, 0.6, terminal.AttrNone)
}

// resolveShieldColor determines the shield color from override or EnergyBlinkType
func (s *ShieldRenderer) resolveShieldColor(shield components.ShieldComponent) render.RGB {
	if shield.OverrideColor != components.ColorNone {
		return s.colorClassToRGB(shield.OverrideColor)
	}

	blinkType := s.gameCtx.State.GetEnergyBlinkType()
	return s.getColorFromBlinkType(blinkType)
}

// getColorFromBlinkType maps blink type to shield RGB
// 0=error/gray, 1=blue, 2=green, 3=red, 4=gold
func (s *ShieldRenderer) getColorFromBlinkType(blinkType uint32) render.RGB {
	switch blinkType {
	case 1: // Blue
		return render.RgbShieldBlue
	case 2: // Green
		return render.RgbShieldGreen
	case 3: // Red
		return render.RgbShieldRed
	case 4: // Gold
		return render.RgbShieldGold
	}
	// 0 (error) or unknown: neutral gray
	return render.RgbShieldNone
}

// colorClassToRGB maps ColorClass overrides to RGB
func (s *ShieldRenderer) colorClassToRGB(color components.ColorClass) render.RGB {
	switch color {
	case components.ColorShield:
		return render.RgbShieldBase
	case components.ColorNugget:
		return render.RgbNuggetOrange
	default:
		return render.RgbShieldBase
	}
}