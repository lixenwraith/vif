package renderers

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// STUB: Legacy blink-type color selection
// // shield256Colors maps sequence blink type to 256-palette index
// // Using xterm 6×6×6 cube for medium-brightness colors
// var shield256Colors = map[uint32]uint8{
// 	0: 245, // None/error: light gray
// 	1: 75,  // Blue (1,3,5)
// 	2: 41,  // Green (0,4,1)
// 	3: 167, // Red (4,1,1)
// 	4: 178, // Gold (4,4,0)
// }
//
// // shieldRGBColors maps sequence blink type to RGB
// var shieldRGBColors = [5]render.RGB{
// 	render.RgbShieldNone,
// 	render.RgbShieldBlue,
// 	render.RgbShieldGreen,
// 	render.RgbShieldRed,
// 	render.RgbShieldGold,
// }

// TODO: refactor 2-palette color store
// 256-color palette indices for energy-based shield colors
const (
	shield256Positive = 226 // Bright yellow (matches RgbCleanerBasePositive)
	shield256Negative = 134 // Violet (matches RgbCleanerBaseNegative)
)

// shieldCellRenderer is the callback type for per-cell shield rendering
// Defines the interface for rendering strategy (256-color vs TrueColor) selected at initialization
// normalizedDistSq is Q16.16 squared distance where Scale = edge of ellipse
type shieldCellRenderer func(buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int32)

// ShieldRenderer renders active shields with dynamic color from GameState
type ShieldRenderer struct {
	gameCtx     *engine.GameContext
	shieldStore *engine.Store[component.ShieldComponent]
	energyStore *engine.Store[component.EnergyComponent]

	renderCell shieldCellRenderer

	// Per-frame state (Q16.16 fixed-point for performance)
	frameColor           render.RGB
	framePalette         uint8
	frameMaxOpacityFixed int32 // Q16.16 max opacity for gradient calculation
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
	buf.SetWriteMask(constant.MaskField)
	shields := r.shieldStore.All()
	if len(shields) == 0 {
		return
	}

	// Energy-based shield color: positive/zero → yellow, negative → purple
	r.frameColor = render.RgbCleanerBasePositive
	r.framePalette = shield256Positive
	if energyComp, ok := r.energyStore.Get(r.gameCtx.CursorEntity); ok {
		if energyComp.Current.Load() < 0 {
			r.frameColor = render.RgbCleanerBaseNegative
			r.framePalette = shield256Negative
		}
	}

	// STUB: Legacy blink-type color selection (glyph-based, may restore for future use)
	// // Resolve blink type (shield color)
	// blinkType := uint32(0)
	// if energyComp, ok := r.energyStore.Get(r.gameCtx.CursorEntity); ok {
	// 	blinkType = energyComp.BlinkType.Load()
	// }
	// if blinkType > 4 {
	// 	blinkType = 0
	// }
	//
	// // Set frame state for callbacks
	// r.frameColor = shieldRGBColors[blinkType]
	// r.framePalette = shield256Colors[blinkType]

	for _, entity := range shields {
		shield, okS := r.shieldStore.Get(entity)
		pos, okP := r.gameCtx.World.Positions.Get(entity)

		if !okS || !okP || !shield.Active {
			continue
		}

		// Cache max opacity as Q16.16 for fixed-point gradient calculation
		r.frameMaxOpacityFixed = vmath.FromFloat(shield.MaxOpacity)

		// Bounding box - integer radii from Q16.16
		radiusXInt := vmath.ToInt(shield.RadiusX)
		radiusYInt := vmath.ToInt(shield.RadiusY)

		// Render area with OOB clamp
		startX := max(0, pos.X-radiusXInt)
		endX := min(ctx.GameWidth-1, pos.X+radiusXInt)
		startY := max(0, pos.Y-radiusYInt)
		endY := min(ctx.GameHeight-1, pos.Y+radiusYInt)

		for y := startY; y <= endY; y++ {
			for x := startX; x <= endX; x++ {
				// Skip cursor position
				if x == ctx.CursorX && y == ctx.CursorY {
					continue
				}

				dx := vmath.FromInt(x - pos.X)
				dy := vmath.FromInt(y - pos.Y)

				// Ellipse containment check (Q16.16)
				// Returns squared normalized distance: <= Scale means inside
				normalizedDistSq := vmath.EllipseDistSq(dx, dy, shield.InvRxSq, shield.InvRySq)

				if normalizedDistSq > vmath.Scale {
					continue
				}

				// Pass Q16.16 distance squared directly to cell renderer
				r.renderCell(buf, ctx.GameX+x, ctx.GameY+y, normalizedDistSq)
			}
		}
	}
}

// cellTrueColor renders a single shield cell with smooth gradient (TrueColor mode)
// Uses pure fixed-point math until final alpha conversion for ~17% performance gain
func (r *ShieldRenderer) cellTrueColor(buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int32) {
	// Simple quadratic gradient: Dark center -> Bright edge
	// normalizedDistSq ranges from 0 (center) to Scale (edge) in Q16.16
	// Squared curve keeps the center transparent/dark for text visibility,
	// while ramping up smoothly to maximum intensity at the very edge
	// This eliminates the "blocky" fade-out and ensures the rim is the brightest part

	// Pure fixed-point: alpha = (distSq / Scale) * maxOpacity
	// Since distSq is already normalized to Scale, just multiply directly
	alphaFixed := vmath.Mul(normalizedDistSq, r.frameMaxOpacityFixed)

	// Single float conversion at final blend step only
	alpha := vmath.ToFloat(alphaFixed)

	// Use BlendScreen for glowing effect on dark backgrounds
	buf.Set(screenX, screenY, 0, render.RGBBlack, r.frameColor, render.BlendScreen, alpha, terminal.AttrNone)
}

// cell256ThresholdSq is 0.8² in Q16.16 for rim detection (0.64 * 65536 = 41943)
const cell256ThresholdSq int32 = 41943

// cell256 renders a single shield cell with discrete zones (256-color mode)
func (r *ShieldRenderer) cell256(buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int32) {
	// In 256-color mode, center is transparent to ensure text legibility
	// normalizedDistSq ranges from 0 (center) to Scale (edge)
	// Threshold at 0.64 (0.8²) to match original dist < 0.8 check
	if normalizedDistSq < cell256ThresholdSq {
		return // Transparent center: Don't touch the buffer
	}

	// Draw rim using Screen blend
	buf.SetBg256(screenX, screenY, r.framePalette)
}