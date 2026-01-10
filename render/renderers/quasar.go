package renderers

import (
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// quasarShieldCellRenderer callback for per-cell shield rendering (256 vs TrueColor)
type quasarShieldCellRenderer func(buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int64)

// QuasarRenderer draws the quasar boss entity with optional shield halo
type QuasarRenderer struct {
	gameCtx *engine.GameContext

	// Shield rendering strategy selected at init
	renderShieldCell quasarShieldCellRenderer

	// Precomputed ellipse params for shield containment (Q32.32)
	shieldInvRxSq int64
	shieldInvRySq int64
	shieldPadX    int
	shieldPadY    int
}

// NewQuasarRenderer creates renderer with color-mode-specific shield strategy
func NewQuasarRenderer(gameCtx *engine.GameContext) *QuasarRenderer {
	r := &QuasarRenderer{
		gameCtx: gameCtx,
	}

	if r.gameCtx.World.Resource.Render.ColorMode == terminal.ColorMode256 {
		r.shieldPadX = constant.QuasarShieldPad256X
		r.shieldPadY = constant.QuasarShieldPad256Y
		r.renderShieldCell = r.shieldCell256
	} else {
		r.shieldPadX = constant.QuasarShieldPadTCX
		r.shieldPadY = constant.QuasarShieldPadTCY
		r.renderShieldCell = r.shieldCellTrueColor
	}

	// Precompute ellipse inverse radii for containment checks
	rx := vmath.FromFloat(float64(constant.QuasarWidth)/2.0 + float64(r.shieldPadX))
	ry := vmath.FromFloat(float64(constant.QuasarHeight)/2.0 + float64(r.shieldPadY))
	r.shieldInvRxSq, r.shieldInvRySq = vmath.EllipseInvRadiiSq(rx, ry)

	return r
}

// Render draws quasar composite with shield halo when zapping
func (r *QuasarRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	anchors := r.gameCtx.World.Component.Quasar.AllEntity()
	if len(anchors) == 0 {
		return
	}

	buf.SetWriteMask(constant.MaskComposite)

	for _, anchor := range anchors {
		quasar, ok := r.gameCtx.World.Component.Quasar.GetComponent(anchor)
		if !ok {
			continue
		}

		header, ok := r.gameCtx.World.Component.Header.GetComponent(anchor)
		if !ok {
			continue
		}

		anchorPos, ok := r.gameCtx.World.Position.Get(anchor)
		if !ok {
			continue
		}

		// Zap range border renders first (background layer)
		r.renderZapRange(ctx, buf, anchorPos.X, anchorPos.Y, &quasar)

		// Shield renders to background layer when active
		if quasar.ShieldActive {
			r.renderShield(ctx, buf, anchorPos.X, anchorPos.Y)
		}

		// Member characters render to foreground layer
		r.renderMembers(ctx, buf, &header, &quasar)
	}
}

// renderZapRange renders zap range ellipse boundary
func (r *QuasarRenderer) renderZapRange(ctx render.RenderContext, buf *render.RenderBuffer, anchorX, anchorY int, quasar *component.QuasarComponent) {
	// Use same color as quasar entity state
	var borderColor render.RGB
	if quasar.IsCharging || quasar.IsZapping {
		borderColor = render.RgbQuasarEnraged
	} else {
		borderColor = render.RgbDrain
	}

	// Adaptive threshold calculation for consistent visual border width
	// Target visual width in cells
	borderHalfWidth := vmath.FromFloat(constant.QuasarZapBorderWidthCells / 2.0)

	// Calculate delta in normalized space: visual_width / radius
	// This ensures border stays same physical size regardless of window/radius size
	if quasar.ZapRadius == 0 {
		return
	}
	borderDelta := vmath.Div(borderHalfWidth, quasar.ZapRadius)

	innerThreshold := vmath.Scale - borderDelta
	outerThreshold := vmath.Scale + borderDelta

	// Dynamic bounding box in grid cells (circle in visual space = ellipse in grid): radius (fixed) -> int cells
	rVisual := quasar.ZapRadius
	// Add padding to ensure coverage
	rxCells := vmath.ToInt(rVisual) + constant.QuasarBorderPaddingCells
	ryCells := vmath.ToInt(rVisual)/2 + constant.QuasarBorderPaddingCells // Visual Y is 2x, so grid cells = radius/2

	minX := anchorX - rxCells
	maxX := anchorX + rxCells
	minY := anchorY - ryCells
	maxY := anchorY + ryCells

	if minX < 0 {
		minX = 0
	}
	if maxX >= ctx.GameWidth {
		maxX = ctx.GameWidth - 1
	}
	if minY < 0 {
		minY = 0
	}
	if maxY >= ctx.GameHeight {
		maxY = ctx.GameHeight - 1
	}

	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			// CRITICAL: Must match QuasarSystem.isCursorInZapRange exactly
			dx := vmath.FromInt(x - anchorX)
			dy := vmath.FromInt(y - anchorY)
			dyCirc := vmath.ScaleToCircular(dy)
			dist := vmath.MagnitudeEuclidean(dx, dyCirc)

			// Normalized distance for boundary detection
			normDist := vmath.Div(dist, quasar.ZapRadius)

			if normDist >= innerThreshold && normDist <= outerThreshold {
				screenX := ctx.GameX + x
				screenY := ctx.GameY + y

				buf.Set(screenX, screenY, 0, render.RGBBlack, borderColor,
					render.BlendScreen, 0.4, terminal.AttrNone)
			}
		}
	}
}

// renderShield draws elliptical halo centered on anchor position
func (r *QuasarRenderer) renderShield(ctx render.RenderContext, buf *render.RenderBuffer, anchorX, anchorY int) {
	// Bounding box relative to anchor (which is at center of quasar grid)
	minDX := -r.shieldPadX - constant.QuasarAnchorOffsetX
	maxDX := constant.QuasarWidth - constant.QuasarAnchorOffsetX + r.shieldPadX - 1
	minDY := -r.shieldPadY - constant.QuasarAnchorOffsetY
	maxDY := constant.QuasarHeight - constant.QuasarAnchorOffsetY + r.shieldPadY - 1

	for dy := minDY; dy <= maxDY; dy++ {
		screenY := ctx.GameY + anchorY + dy
		if screenY < ctx.GameY || screenY >= ctx.GameY+ctx.GameHeight {
			continue
		}

		for dx := minDX; dx <= maxDX; dx++ {
			screenX := ctx.GameX + anchorX + dx
			if screenX < ctx.GameX || screenX >= ctx.Width {
				continue
			}

			// Ellipse containment via normalized squared distance
			dxFixed := vmath.FromInt(dx)
			dyFixed := vmath.FromInt(dy)
			normalizedDistSq := vmath.EllipseDistSq(dxFixed, dyFixed, r.shieldInvRxSq, r.shieldInvRySq)

			if normalizedDistSq > vmath.Scale {
				continue
			}

			r.renderShieldCell(buf, screenX, screenY, normalizedDistSq)
		}
	}
}

// shieldCellTrueColor renders gradient blend from center to edge
func (r *QuasarRenderer) shieldCellTrueColor(buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int64) {
	// Quadratic gradient: transparent center → max opacity at edge
	alphaFixed := vmath.Mul(normalizedDistSq, vmath.FromFloat(constant.QuasarShieldMaxOpacity))
	alpha := vmath.ToFloat(alphaFixed)

	buf.Set(screenX, screenY, 0, render.RGBBlack, render.RgbQuasarShield, render.BlendScreen, alpha, terminal.AttrNone)
}

// quasarShield256ThresholdSq defines inner edge of solid rim (0.4 in Q32.32 squared)
var quasarShield256ThresholdSq = vmath.FromFloat(0.16)

// shieldCell256 renders solid rim for outer portion of ellipse
func (r *QuasarRenderer) shieldCell256(buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int64) {
	// Skip center region, only render outer rim
	if normalizedDistSq < quasarShield256ThresholdSq {
		return
	}

	buf.SetBg256(screenX, screenY, constant.QuasarShield256Palette)
}

// renderMembers draws quasar character grid with state-based coloring
func (r *QuasarRenderer) renderMembers(ctx render.RenderContext, buf *render.RenderBuffer, header *component.HeaderComponent, quasar *component.QuasarComponent) {
	// Determine color: flash > enraged > normal
	var color render.RGB
	if quasar.HitFlashRemaining > 0 {
		color = r.calculateFlashColor(quasar.HitFlashRemaining)
	} else if quasar.IsCharging || quasar.IsZapping {
		color = render.RgbQuasarEnraged
	} else {
		color = render.RgbDrain
	}

	for _, member := range header.MemberEntries {
		if member.Entity == 0 {
			continue
		}

		pos, ok := r.gameCtx.World.Position.Get(member.Entity)
		if !ok {
			continue
		}

		// Resolve rune from QuasarChars using member offset
		row := int(member.OffsetY) + constant.QuasarAnchorOffsetY
		col := int(member.OffsetX) + constant.QuasarAnchorOffsetX
		ch := component.QuasarChars[row][col]

		screenX := ctx.GameX + pos.X
		screenY := ctx.GameY + pos.Y

		if screenX < ctx.GameX || screenX >= ctx.Width ||
			screenY < ctx.GameY || screenY >= ctx.GameY+ctx.GameHeight {
			continue
		}

		buf.SetFgOnly(screenX, screenY, ch, color, terminal.AttrNone)
	}
}

// calculateFlashColor returns yellow with pulse effect (low→high→low)
func (r *QuasarRenderer) calculateFlashColor(remaining time.Duration) render.RGB {
	progress := float64(remaining) / float64(constant.QuasarHitFlashDuration)

	var intensity float64
	if progress > 0.67 {
		intensity = 0.6 // Phase 1: low
	} else if progress > 0.33 {
		intensity = 1.0 // Phase 2: high
	} else {
		intensity = 0.6 // Phase 3: low
	}

	return render.Scale(render.RgbQuasarFlash, intensity)
}