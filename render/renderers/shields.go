package renderers

import (
	"math"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// shieldCellRenderer is the callback type for per-cell shield rendering
// Defines the interface for rendering strategy (256-color vs TrueColor) selected initialization
type shieldCellRenderer func(buf *render.RenderBuffer, screenX, screenY int, dist float64, color render.RGB, maxOpacity float64)

// ShieldRenderer renders active shields with dynamic color from GameState
type ShieldRenderer struct {
	gameCtx     *engine.GameContext
	shieldStore *engine.Store[component.ShieldComponent]
	energyStore *engine.Store[component.EnergyComponent]

	renderCell shieldCellRenderer
}

// NewShieldRenderer creates a new shield renderer
func NewShieldRenderer(gameCtx *engine.GameContext) *ShieldRenderer {
	s := &ShieldRenderer{
		gameCtx:     gameCtx,
		shieldStore: engine.GetStore[component.ShieldComponent](gameCtx.World),
		energyStore: engine.GetStore[component.EnergyComponent](gameCtx.World),
	}

	// Access RenderConfig for display mode and select strategy once
	cfg := engine.MustGetResource[*engine.RenderConfig](gameCtx.World.Resources)

	if cfg.ColorMode == terminal.ColorMode256 {
		s.renderCell = s.cell256
	} else {
		s.renderCell = s.cellTrueColor
	}

	return s
}

// Render draws all active shields with quadratic falloff gradient
func (r *ShieldRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(render.MaskShield)
	shields := r.shieldStore.All()
	if len(shields) == 0 {
		return
	}

	for _, entity := range shields {
		shield, okS := r.shieldStore.Get(entity)
		pos, okP := r.gameCtx.World.Positions.Get(entity)

		if !okS || !okP {
			continue
		}

		if !shield.Active {
			continue
		}

		// Resolve shield color
		shieldRGB := r.resolveShieldColor(shield)

		// Bounding box - integer radii from Q16.16
		radiusXInt := vmath.ToInt(shield.RadiusX)
		radiusYInt := vmath.ToInt(shield.RadiusY)

		startX := pos.X - radiusXInt
		endX := pos.X + radiusXInt
		startY := pos.Y - radiusYInt
		endY := pos.Y + radiusYInt

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

		for y := startY; y <= endY; y++ {
			for x := startX; x <= endX; x++ {
				// Skip cursor position
				if x == ctx.CursorX && y == ctx.CursorY {
					continue
				}

				dx := vmath.FromInt(x - pos.X)
				dy := vmath.FromInt(y - pos.Y)
				dxSq := vmath.Mul(dx, dx)
				dySq := vmath.Mul(dy, dy)

				// Ellipse containment check in Q16.16
				normalizedDistSq := vmath.Mul(dxSq, shield.InvRxSq) + vmath.Mul(dySq, shield.InvRySq)
				if normalizedDistSq > vmath.Scale {
					continue
				}

				screenX := ctx.GameX + x
				screenY := ctx.GameY + y

				// Use pre-selected strategy
				dist := math.Sqrt(vmath.ToFloat(normalizedDistSq))
				r.renderCell(buf, screenX, screenY, dist, shieldRGB, shield.MaxOpacity)
			}
		}
	}
}

// cellTrueColor renders a single shield cell with smooth gradient (TrueColor mode)
func (r *ShieldRenderer) cellTrueColor(buf *render.RenderBuffer, screenX, screenY int, dist float64, color render.RGB, maxOpacity float64) {
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
func (r *ShieldRenderer) cell256(buf *render.RenderBuffer, screenX, screenY int, dist float64, color render.RGB, maxOpacity float64) {
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
func (r *ShieldRenderer) resolveShieldColor(shield component.ShieldComponent) render.RGB {
	if shield.OverrideColor != component.ColorNone {
		return r.colorClassToRGB(shield.OverrideColor)
	}

	// Query energy component for blink type
	energyComp, ok := r.energyStore.Get(r.gameCtx.CursorEntity)
	if !ok {
		return render.RgbShieldNone
	}
	blinkType := energyComp.BlinkType.Load()
	return r.getColorFromBlinkType(blinkType)
}

// getColorFromBlinkType maps blink type to shield RGB
// 0=error/gray, 1=blue, 2=green, 3=red, 4=gold
func (r *ShieldRenderer) getColorFromBlinkType(blinkType uint32) render.RGB {
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
func (r *ShieldRenderer) colorClassToRGB(color component.ColorClass) render.RGB {
	switch color {
	case component.ColorShield:
		return render.RgbShieldBase
	case component.ColorNugget:
		return render.RgbNuggetOrange
	default:
		return render.RgbShieldBase
	}
}