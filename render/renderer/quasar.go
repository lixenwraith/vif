package renderer

import (
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
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

	if r.gameCtx.World.Resources.Render.ColorMode == terminal.ColorMode256 {
		r.shieldPadX = parameter.QuasarShieldPad256X
		r.shieldPadY = parameter.QuasarShieldPad256Y
		r.renderShieldCell = r.shieldCell256
	} else {
		r.shieldPadX = parameter.QuasarShieldPadTCX
		r.shieldPadY = parameter.QuasarShieldPadTCY
		r.renderShieldCell = r.shieldCellTrueColor
	}

	// Precompute ellipse inverse radii for containment checks
	rx := vmath.FromFloat(float64(parameter.QuasarWidth)/2.0 + float64(r.shieldPadX))
	ry := vmath.FromFloat(float64(parameter.QuasarHeight)/2.0 + float64(r.shieldPadY))
	r.shieldInvRxSq, r.shieldInvRySq = vmath.EllipseInvRadiiSq(rx, ry)

	return r
}

// Render draws quasar composite with shield halo when zapping
func (r *QuasarRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	headerEntities := r.gameCtx.World.Components.Quasar.GetAllEntities()
	if len(headerEntities) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskComposite)

	for _, headerEntity := range headerEntities {
		quasarComp, ok := r.gameCtx.World.Components.Quasar.GetComponent(headerEntity)
		if !ok {
			continue
		}

		headerComp, ok := r.gameCtx.World.Components.Header.GetComponent(headerEntity)
		if !ok {
			continue
		}

		headerPos, ok := r.gameCtx.World.Positions.GetPosition(headerEntity)
		if !ok {
			continue
		}

		combatComp, ok := r.gameCtx.World.Components.Combat.GetComponent(headerEntity)
		if !ok {
			continue
		}

		// Zap range border renders first (background layer)
		r.renderZapRange(ctx, buf, headerPos.X, headerPos.Y, &quasarComp)

		// Shield renders to background layer when active
		if quasarComp.IsShielded {
			r.renderShield(ctx, buf, headerPos.X, headerPos.Y)
		}

		// Member characters render to foreground layer
		r.renderMembers(ctx, buf, &headerComp, &combatComp)
	}
}

// renderZapRange renders zap range ellipse boundary
func (r *QuasarRenderer) renderZapRange(ctx render.RenderContext, buf *render.RenderBuffer, headerX, headerY int, quasar *component.QuasarComponent) {
	// Use same color as quasar entity state
	var borderColor terminal.RGB
	if quasar.IsCharging || quasar.IsZapping {
		borderColor = visual.RgbCombatEnraged
	} else {
		borderColor = visual.RgbDrain
	}

	// Adaptive threshold calculation for consistent visual border width, target visual width in cells
	borderHalfWidth := vmath.FromFloat(parameter.QuasarZapBorderWidthCells / 2.0)

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
	rxCells := vmath.ToInt(rVisual) + parameter.QuasarBorderPaddingCells
	ryCells := vmath.ToInt(rVisual)/2 + parameter.QuasarBorderPaddingCells // Visual Y is 2x, so grid cells = radius/2

	minX := headerX - rxCells
	maxX := headerX + rxCells
	minY := headerY - ryCells
	maxY := headerY + ryCells

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
			dx := vmath.FromInt(x - headerX)
			dy := vmath.FromInt(y - headerY)
			dyCirc := vmath.ScaleToCircular(dy)
			dist := vmath.MagnitudeEuclidean(dx, dyCirc)

			// Normalized distance for boundary detection
			normDist := vmath.Div(dist, quasar.ZapRadius)

			if normDist >= innerThreshold && normDist <= outerThreshold {
				screenX := ctx.GameXOffset + x
				screenY := ctx.GameYOffset + y

				buf.Set(screenX, screenY, 0, visual.RgbBlack, borderColor,
					render.BlendScreen, 0.4, terminal.AttrNone)
			}
		}
	}
}

// renderShield draws elliptical halo centered on header position
func (r *QuasarRenderer) renderShield(ctx render.RenderContext, buf *render.RenderBuffer, headerX, headerY int) {
	// Bounding box relative to header (which is at center of quasar grid)
	minDX := -r.shieldPadX - parameter.QuasarHeaderOffsetX
	maxDX := parameter.QuasarWidth - parameter.QuasarHeaderOffsetX + r.shieldPadX - 1
	minDY := -r.shieldPadY - parameter.QuasarHeaderOffsetY
	maxDY := parameter.QuasarHeight - parameter.QuasarHeaderOffsetY + r.shieldPadY - 1

	for dy := minDY; dy <= maxDY; dy++ {
		screenY := ctx.GameYOffset + headerY + dy
		if screenY < ctx.GameYOffset || screenY >= ctx.GameYOffset+ctx.GameHeight {
			continue
		}

		for dx := minDX; dx <= maxDX; dx++ {
			screenX := ctx.GameXOffset + headerX + dx
			if screenX < ctx.GameXOffset || screenX >= ctx.ScreenWidth {
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
	alphaFixed := vmath.Mul(normalizedDistSq, vmath.FromFloat(parameter.QuasarShieldMaxOpacity))
	alpha := vmath.ToFloat(alphaFixed)

	buf.Set(screenX, screenY, 0, visual.RgbBlack, visual.RgbQuasarShield, render.BlendScreen, alpha, terminal.AttrNone)
}

// quasarShield256ThresholdSq defines inner edge of solid rim (0.4 in Q32.32 squared)
var quasarShield256ThresholdSq = vmath.FromFloat(0.16)

// shieldCell256 renders solid rim for outer portion of ellipse
func (r *QuasarRenderer) shieldCell256(buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int64) {
	// Skip center region, only render outer rim
	if normalizedDistSq < quasarShield256ThresholdSq {
		return
	}

	buf.SetBg256(screenX, screenY, parameter.QuasarShield256Palette)
}

// renderMembers draws quasar character grid with state-based coloring
func (r *QuasarRenderer) renderMembers(ctx render.RenderContext, buf *render.RenderBuffer, headerComp *component.HeaderComponent, combatComp *component.CombatComponent) {

	// Determine color: flash > enraged > normal
	var color terminal.RGB
	if combatComp.RemainingHitFlash > 0 {
		color = r.calculateFlashColor(combatComp.RemainingHitFlash)
	} else if combatComp.IsEnraged {
		color = visual.RgbCombatEnraged
	} else {
		color = visual.RgbDrain
	}

	for _, member := range headerComp.MemberEntries {
		if member.Entity == 0 {
			continue
		}

		pos, ok := r.gameCtx.World.Positions.GetPosition(member.Entity)
		if !ok {
			continue
		}

		// Resolve rune from QuasarChars using member offset
		row := int(member.OffsetY) + parameter.QuasarHeaderOffsetY
		col := int(member.OffsetX) + parameter.QuasarHeaderOffsetX
		ch := visual.QuasarChars[row][col]

		screenX := ctx.GameXOffset + pos.X
		screenY := ctx.GameYOffset + pos.Y

		if screenX < ctx.GameXOffset || screenX >= ctx.ScreenWidth ||
			screenY < ctx.GameYOffset || screenY >= ctx.GameYOffset+ctx.GameHeight {
			continue
		}

		buf.SetFgOnly(screenX, screenY, ch, color, terminal.AttrNone)
	}
}

// calculateFlashColor returns yellow with pulse effect (low→high→low)
func (r *QuasarRenderer) calculateFlashColor(remaining time.Duration) terminal.RGB {
	progress := float64(remaining) / float64(parameter.CombatHitFlashDuration)

	var intensity float64
	if progress > 0.67 {
		intensity = 0.6 // Phase 1: low
	} else if progress > 0.33 {
		intensity = 1.0 // Phase 2: high
	} else {
		intensity = 0.6 // Phase 3: low
	}

	return render.Scale(visual.RgbCombatHitFlash, intensity)
}