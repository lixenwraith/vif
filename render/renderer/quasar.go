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

// QuasarRenderer draws the quasar boss entity components
type QuasarRenderer struct {
	gameCtx *engine.GameContext
}

// NewQuasarRenderer creates the renderer
func NewQuasarRenderer(gameCtx *engine.GameContext) *QuasarRenderer {
	return &QuasarRenderer{
		gameCtx: gameCtx,
	}
}

// Render draws quasar composite parts (zap range and members)
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

		// Zap range border renders (background layer)
		r.renderZapRange(ctx, buf, headerPos.X, headerPos.Y, &quasarComp)

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
	if quasar.ZapRadius == 0 {
		return
	}
	borderDelta := vmath.Div(borderHalfWidth, quasar.ZapRadius)

	innerThreshold := vmath.Scale - borderDelta
	outerThreshold := vmath.Scale + borderDelta

	// Dynamic bounding box in grid cells
	rVisual := quasar.ZapRadius
	rxCells := vmath.ToInt(rVisual) + parameter.QuasarBorderPaddingCells
	ryCells := vmath.ToInt(rVisual)/2 + parameter.QuasarBorderPaddingCells

	minX := headerX - rxCells
	maxX := headerX + rxCells
	minY := headerY - ryCells
	maxY := headerY + ryCells

	// Clamp to screen
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
			dx := vmath.FromInt(x - headerX)
			dy := vmath.FromInt(y - headerY)
			dyCirc := vmath.ScaleToCircular(dy)
			dist := vmath.Magnitude(dx, dyCirc)

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

// renderMembers draws quasar character grid with state-based coloring
func (r *QuasarRenderer) renderMembers(ctx render.RenderContext, buf *render.RenderBuffer, headerComp *component.HeaderComponent, combatComp *component.CombatComponent) {
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

// calculateFlashColor returns yellow with pulse effect
func (r *QuasarRenderer) calculateFlashColor(remaining time.Duration) terminal.RGB {
	progress := float64(remaining) / float64(parameter.CombatHitFlashDuration)

	var intensity float64
	if progress > 0.67 {
		intensity = 0.6
	} else if progress > 0.33 {
		intensity = 1.0
	} else {
		intensity = 0.6
	}

	return render.Scale(visual.RgbCombatHitFlash, intensity)
}