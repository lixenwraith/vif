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

		// Shield renders to background layer when active
		if quasar.ShieldActive {
			r.renderShield(ctx, buf, anchorPos.X, anchorPos.Y)
		}

		// Member characters render to foreground layer
		r.renderMembers(ctx, buf, &header, &quasar)
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