package renderers

// @lixen: #dev{feature[dust(render,system)],feature[quasar(render,system)]}

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
type quasarShieldCellRenderer func(buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int32)

// QuasarRenderer draws the quasar boss entity with optional shield halo
type QuasarRenderer struct {
	gameCtx *engine.GameContext

	quasarStore *engine.Store[component.QuasarComponent]
	headerStore *engine.Store[component.CompositeHeaderComponent]

	// Shield rendering strategy selected at init
	renderShieldCell quasarShieldCellRenderer

	// Precomputed ellipse params for shield containment (Q16.16)
	shieldInvRxSq int32
	shieldInvRySq int32
	shieldPadX    int
	shieldPadY    int

	// Zap range visual circle - mirrors QuasarSystem.updateZapRadius exactly
	zapRadius int32 // Visual circle radius (Q16.16)
	zapRCells int   // Bounding box (grid cells)
}

// NewQuasarRenderer creates renderer with color-mode-specific shield strategy
func NewQuasarRenderer(gameCtx *engine.GameContext) *QuasarRenderer {
	r := &QuasarRenderer{
		gameCtx: gameCtx,

		quasarStore: engine.GetStore[component.QuasarComponent](gameCtx.World),
		headerStore: engine.GetStore[component.CompositeHeaderComponent](gameCtx.World),
	}

	// Select shield strategy based on terminal color capability
	cfg := engine.MustGetResource[*engine.RenderConfig](gameCtx.World.Resources)

	if cfg.ColorMode == terminal.ColorMode256 {
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

	// TODO: needs recompute on resize
	// Precompute zap range - MUST match QuasarSystem.updateZapRadiu
	width := gameCtx.GameWidth
	height := gameCtx.GameHeight
	r.zapRadius = vmath.FromInt(max(width/2, height))
	r.zapRCells = max(width, height) + 2

	return r
}

// Render draws quasar composite with shield halo when zapping
func (r *QuasarRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	anchors := r.quasarStore.All()
	if len(anchors) == 0 {
		return
	}

	buf.SetWriteMask(constant.MaskComposite)

	for _, anchor := range anchors {
		quasar, ok := r.quasarStore.Get(anchor)
		if !ok {
			continue
		}

		header, ok := r.headerStore.Get(anchor)
		if !ok {
			continue
		}

		anchorPos, ok := r.gameCtx.World.Positions.Get(anchor)
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

	// Boundary band thresholds (~1 cell width ring at ellipse edge)
	// EllipseDistSq == Scale means exactly on boundary
	// TODO: this needs to be screen size dependents, it has holes in small panes, ok for 150x50 ish
	innerThreshold := vmath.FromFloat(0.99)
	outerThreshold := vmath.FromFloat(1.01)

	// Bounding box in grid cells (circle in visual space = ellipse in grid)
	rxCells := r.zapRCells
	ryCells := r.zapRCells/2 + 2 // Visual Y is 2x, so grid cells = radius/2

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
			normDist := vmath.Div(dist, r.zapRadius)

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
func (r *QuasarRenderer) shieldCellTrueColor(buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int32) {
	// Quadratic gradient: transparent center → max opacity at edge
	alphaFixed := vmath.Mul(normalizedDistSq, vmath.FromFloat(constant.QuasarShieldMaxOpacity))
	alpha := vmath.ToFloat(alphaFixed)

	buf.Set(screenX, screenY, 0, render.RGBBlack, render.RgbQuasarShield, render.BlendScreen, alpha, terminal.AttrNone)
}

// quasarShield256ThresholdSq defines inner edge of solid rim (0.4 in Q16.16 squared)
const quasarShield256ThresholdSq int32 = 26214

// shieldCell256 renders solid rim for outer portion of ellipse
func (r *QuasarRenderer) shieldCell256(buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int32) {
	// Skip center region, only render outer rim
	if normalizedDistSq < quasarShield256ThresholdSq {
		return
	}

	buf.SetBg256(screenX, screenY, constant.QuasarShield256Palette)
}

// renderMembers draws quasar character grid with state-based coloring
func (r *QuasarRenderer) renderMembers(ctx render.RenderContext, buf *render.RenderBuffer, header *component.CompositeHeaderComponent, quasar *component.QuasarComponent) {
	// Determine color: flash > enraged > normal
	var color render.RGB
	if quasar.HitFlashRemaining > 0 {
		color = r.calculateFlashColor(quasar.HitFlashRemaining)
	} else if quasar.IsCharging || quasar.IsZapping {
		color = render.RgbQuasarEnraged
	} else {
		color = render.RgbDrain
	}

	for _, member := range header.Members {
		if member.Entity == 0 {
			continue
		}

		pos, hasPos := r.gameCtx.World.Positions.Get(member.Entity)
		if !hasPos {
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