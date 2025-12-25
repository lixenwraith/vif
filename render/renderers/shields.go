package renderers

import (
	"math"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// shield256Colors maps sequence blink type to 256-palette index
// Using xterm 6×6×6 cube for medium-brightness colors
var shield256Colors = map[uint32]uint8{
	0: 245, // None/error: light gray
	1: 75,  // Blue (1,3,5)
	2: 41,  // Green (0,4,1)
	3: 167, // Red (4,1,1)
	4: 178, // Gold (4,4,0)
}

// TODO: this is a clutch, to be refactored
// shieldRGBColors maps sequence blink type to RGB
var shieldRGBColors = [5]render.RGB{
	render.RgbShieldNone,
	render.RgbShieldBlue,
	render.RgbShieldGreen,
	render.RgbShieldRed,
	render.RgbShieldGold,
}

// shieldCellRenderer is the callback type for per-cell shield rendering
// Defines the interface for rendering strategy (256-color vs TrueColor) selected initialization
type shieldCellRenderer func(buf *render.RenderBuffer, screenX, screenY int, dist float64)

// ShieldRenderer renders active shields with dynamic color from GameState
type ShieldRenderer struct {
	gameCtx     *engine.GameContext
	shieldStore *engine.Store[component.ShieldComponent]
	energyStore *engine.Store[component.EnergyComponent]

	renderCell shieldCellRenderer

	// Per-frame state
	frameColor      render.RGB
	framePalette    uint8
	frameMaxOpacity float64
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

	// Resolve blink type (shield color)
	blinkType := uint32(0)
	if energyComp, ok := r.energyStore.Get(r.gameCtx.CursorEntity); ok {
		blinkType = energyComp.BlinkType.Load()
	}
	if blinkType > 4 {
		blinkType = 0
	}

	// Set frame state for callbacks
	r.frameColor = shieldRGBColors[blinkType]
	r.framePalette = shield256Colors[blinkType]

	for _, entity := range shields {
		shield, okS := r.shieldStore.Get(entity)
		pos, okP := r.gameCtx.World.Positions.Get(entity)

		if !okS || !okP {
			continue
		}

		if !shield.Active {
			continue
		}

		r.frameMaxOpacity = shield.MaxOpacity

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
				dxSq := vmath.Mul(dx, dx)
				dySq := vmath.Mul(dy, dy)

				// Ellipse containment check in Q16.16
				normalizedDistSq := vmath.Mul(dxSq, shield.InvRxSq) + vmath.Mul(dySq, shield.InvRySq)

				if normalizedDistSq > vmath.Scale {
					continue
				}

				// Use pre-selected strategy
				dist := math.Sqrt(vmath.ToFloat(normalizedDistSq))
				r.renderCell(buf, ctx.GameX+x, ctx.GameY+y, dist)
			}
		}
	}
}

// cellTrueColor renders a single shield cell with smooth gradient (TrueColor mode)
func (r *ShieldRenderer) cellTrueColor(buf *render.RenderBuffer, screenX, screenY int, dist float64) {
	// Simple quadratic gradient: Dark center -> Bright edge
	// dist ranges from 0.0 (center) to 1.0 (edge)
	// Squared curve (dist^2) keeps the center transparent/dark for text visibility,
	// while ramping up smoothly to maximum intensity at the very edge
	// This eliminates the "blocky" fade-out and ensures the rim is the brightest part
	alpha := (dist * dist) * r.frameMaxOpacity

	// Use BlendScreen for glowing effect on dark backgrounds
	buf.Set(screenX, screenY, 0, render.RGBBlack, r.frameColor, render.BlendScreen, alpha, terminal.AttrNone)
}

// cell256 renders a single shield cell with discrete zones (256-color mode)
func (r *ShieldRenderer) cell256(buf *render.RenderBuffer, screenX, screenY int, dist float64) {
	// In 256-color mode, center is transparent to ensure text legibility
	// dist ranges from 0.0 (center) to 1.0 (edge)
	if dist < 0.8 {
		return // Transparent center: Don't touch the buffer
	}

	// Draw rim using Screen blend
	buf.SetBg256(screenX, screenY, r.framePalette)
	// buf.Set(screenX, screenY, 0, render.RGBBlack, color, render.BlendScreen, 0.6, terminal.AttrNone)
}