package renderers

import (
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// TODO: refactor 2-palette color store
// 256-color palette indices for energy-based shield colors
const (
	shield256Positive = 226 // Bright yellow (matches RgbCleanerBasePositive)
	shield256Negative = 134 // Violet (matches RgbCleanerBaseNegative)
)

// Boost glow parameters (Q32.32)
var (
	// boostGlowRotationSpeed is 2 rotations/sec in Q32.32 (Scale = full rotation)
	boostGlowRotationSpeed int64 = vmath.Scale * 2

	// boostGlowEdgeThreshold is 0.6² in Q32.32 for rim detection
	boostGlowEdgeThreshold = vmath.FromFloat(0.36) // 0.6²

	// boostGlowIntensityFixed is 0.7 peak alpha in Q32.32
	boostGlowIntensityFixed = vmath.FromFloat(0.7)

	// cell256ThresholdSq is 0.8² in Q32.32 for rim detection
	cell256ThresholdSq = vmath.FromFloat(0.64) // 0.8²
)

// shieldCellRenderer is the callback type for per-cell shield rendering
// Defines the interface for rendering strategy (256-color vs TrueColor) selected at initialization
// normalizedDistSq is Q32.32 squared distance where Scale = edge of ellipse
type shieldCellRenderer func(buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int64)

// ShieldRenderer renders active shields with dynamic color from GameState
type ShieldRenderer struct {
	gameCtx *engine.GameContext

	renderCell shieldCellRenderer

	// Per-frame state (Q32.32 fixed-point for performance)
	frameColor           render.RGB
	framePalette         uint8
	frameMaxOpacityFixed int64 // Q32.32 max opacity for gradient calculation

	// Boost glow per-frame state
	boostGlowActive  bool
	rotDirX, rotDirY int64 // Rotation direction unit vector

	// TODO: find a better way to handle the state (no renderer arg)
	// Current cell state (set before renderCell callback)
	cellDx, cellDy int64
}

// NewShieldRenderer creates a new shield renderer
func NewShieldRenderer(gameCtx *engine.GameContext) *ShieldRenderer {
	r := &ShieldRenderer{
		gameCtx: gameCtx,
	}

	if r.gameCtx.World.Resource.Render.ColorMode == terminal.ColorMode256 {
		r.renderCell = r.cell256
	} else {
		r.renderCell = r.cellTrueColor
	}

	return r
}

// Render draws all active shields with quadratic falloff gradient
func (r *ShieldRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(constant.MaskField)
	shieldEntities := r.gameCtx.World.Component.Shield.AllEntity()
	if len(shieldEntities) == 0 {
		return
	}

	// Energy-based shield color: positive/zero → yellow, negative → purple
	r.frameColor = render.RgbCleanerBasePositive
	r.framePalette = shield256Positive
	if energyComp, ok := r.gameCtx.World.Component.Energy.GetComponent(r.gameCtx.CursorEntity); ok {
		if energyComp.Current.Load() < 0 {
			r.frameColor = render.RgbCleanerBaseNegative
			r.framePalette = shield256Negative
		}
	}

	// Boost glow frame state
	r.boostGlowActive = false
	if boost, ok := r.gameCtx.World.Component.Boost.GetComponent(r.gameCtx.CursorEntity); ok && boost.Active {
		r.boostGlowActive = true
		// 2 rotations/sec = 500ms period
		nanos := ctx.GameTime.UnixNano()
		period := int64(500_000_000) // TODO: magic number to constant
		phase := nanos % period
		angle := int64((phase * int64(vmath.Scale)) / period)
		r.rotDirX = vmath.Cos(angle)
		r.rotDirY = vmath.Sin(angle)
	}

	for _, shieldEntity := range shieldEntities {
		shieldComp, ok := r.gameCtx.World.Component.Shield.GetComponent(shieldEntity)
		if !ok || !shieldComp.Active {
			continue
		}

		shieldPos, ok := r.gameCtx.World.Position.Get(shieldEntity)
		if !ok {
			continue
		}

		// Cache max opacity as Q32.32 for fixed-point gradient calculation
		r.frameMaxOpacityFixed = vmath.FromFloat(shieldComp.MaxOpacity)

		// Bounding box - integer radii from Q32.32
		radiusXInt := vmath.ToInt(shieldComp.RadiusX)
		radiusYInt := vmath.ToInt(shieldComp.RadiusY)

		// Render area with OOB clamp
		startX := max(0, shieldPos.X-radiusXInt)
		endX := min(ctx.GameWidth-1, shieldPos.X+radiusXInt)
		startY := max(0, shieldPos.Y-radiusYInt)
		endY := min(ctx.GameHeight-1, shieldPos.Y+radiusYInt)

		for y := startY; y <= endY; y++ {
			for x := startX; x <= endX; x++ {
				// Skip cursor position
				if x == ctx.CursorX && y == ctx.CursorY {
					continue
				}

				dx := vmath.FromInt(x - shieldPos.X)
				dy := vmath.FromInt(y - shieldPos.Y)

				// Ellipse containment check (Q32.32)
				// Returns squared normalized distance: <= Scale means inside
				normalizedDistSq := vmath.EllipseDistSq(dx, dy, shieldComp.InvRxSq, shieldComp.InvRySq)

				if normalizedDistSq > vmath.Scale {
					continue
				}

				// Store for callback access
				r.cellDx = dx
				r.cellDy = dy

				// Pass Q32.32 distance squared directly to cell renderer
				r.renderCell(buf, ctx.GameX+x, ctx.GameY+y, normalizedDistSq)
			}
		}
	}
}

// cellTrueColor renders a single shield cell with smooth gradient (TrueColor mode)
// Uses pure fixed-point math until final alpha conversion for ~17% performance gain
func (r *ShieldRenderer) cellTrueColor(buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int64) {
	// Simple quadratic gradient: Dark center -> Bright edge
	// normalizedDistSq ranges from 0 (center) to Scale (edge) in Q32.32
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

	// Boost glow on outer rim
	if !r.boostGlowActive || normalizedDistSq <= boostGlowEdgeThreshold {
		return
	}

	cellDirX, cellDirY := vmath.Normalize2D(r.cellDx, r.cellDy)
	dot := vmath.DotProduct(cellDirX, cellDirY, r.rotDirX, r.rotDirY)

	if dot <= 0 {
		return
	}

	edgeFactor := vmath.Div(normalizedDistSq-boostGlowEdgeThreshold, vmath.Scale-boostGlowEdgeThreshold)
	intensity := vmath.Mul(vmath.Mul(dot, edgeFactor), boostGlowIntensityFixed)

	buf.Set(screenX, screenY, 0, render.RGBBlack, render.RgbBoostGlow, render.BlendSoftLight, vmath.ToFloat(intensity), terminal.AttrNone)
}

// cell256 renders a single shield cell with discrete zones (256-color mode)
func (r *ShieldRenderer) cell256(buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int64) {
	// In 256-color mode, center is transparent to ensure text legibility
	// normalizedDistSq ranges from 0 (center) to Scale (edge)
	// Threshold at 0.64 (0.8²) to match original dist < 0.8 check
	if normalizedDistSq < cell256ThresholdSq {
		return // Transparent center: Don't touch the buffer
	}

	// Draw rim using Screen blend
	buf.SetBg256(screenX, screenY, r.framePalette)
}