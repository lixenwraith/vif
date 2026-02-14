package renderer

import (
	"math"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// HealthBarRenderer draws health indicators above enemy entities
type HealthBarRenderer struct {
	gameCtx  *engine.GameContext
	position visual.HealthBarPosition

	// Rendering strategy selected at init
	renderBar healthBarCellRenderer
}

// healthBarCellRenderer callback for color mode specific rendering
type healthBarCellRenderer func(buf *render.RenderBuffer, x, y int, ch rune, ratio float64)

// NewHealthBarRenderer creates a health bar renderer
func NewHealthBarRenderer(gameCtx *engine.GameContext) *HealthBarRenderer {
	r := &HealthBarRenderer{
		gameCtx:  gameCtx,
		position: visual.HealthBarPosDefault,
	}

	if gameCtx.World.Resources.Render.ColorMode == terminal.ColorMode256 {
		r.renderBar = r.renderCell256
	} else {
		r.renderBar = r.renderCellTrueColor
	}

	return r
}

// getOppositePosition returns the opposite bar position for OOB fallback
func getOppositePosition(pos visual.HealthBarPosition) visual.HealthBarPosition {
	switch pos {
	case visual.HealthBarAbove:
		return visual.HealthBarBelow
	case visual.HealthBarBelow:
		return visual.HealthBarAbove
	case visual.HealthBarLeft:
		return visual.HealthBarRight
	case visual.HealthBarRight:
		return visual.HealthBarLeft
	default:
		return visual.HealthBarBelow
	}
}

// Render draws health bars for all enemy combat entities
func (r *HealthBarRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	if !visual.HealthBarEnabled {
		return
	}

	buf.SetWriteMask(visual.MaskHealthBar)

	entities := r.gameCtx.World.Components.Combat.GetAllEntities()
	for _, entity := range entities {
		combatComp, ok := r.gameCtx.World.Components.Combat.GetComponent(entity)
		if !ok {
			continue
		}

		// Filter: only Drain, Swarm, Quasar
		var width, height, maxHP, offsetX, offsetY int
		switch combatComp.CombatEntityType {
		case component.CombatEntityDrain:
			width, height, maxHP = 1, 1, parameter.CombatInitialHPDrain
			offsetX, offsetY = 0, 0
		case component.CombatEntitySwarm:
			width, height, maxHP = parameter.SwarmWidth, parameter.SwarmHeight, parameter.CombatInitialHPSwarm
			offsetX, offsetY = parameter.SwarmHeaderOffsetX, parameter.SwarmHeaderOffsetY
		case component.CombatEntityQuasar:
			width, height, maxHP = parameter.QuasarWidth, parameter.QuasarHeight, parameter.CombatInitialHPQuasar
			offsetX, offsetY = parameter.QuasarHeaderOffsetX, parameter.QuasarHeaderOffsetY
		default:
			continue
		}

		pos, ok := r.gameCtx.World.Positions.GetPosition(entity)
		if !ok {
			continue
		}

		// Health ratio clamped to [0, 1]
		ratio := float64(combatComp.HitPoints) / float64(maxHP)
		if ratio > 1.0 {
			ratio = 1.0
		}
		if ratio < 0 {
			ratio = 0
		}

		// Entity top-left corner (accounting for header offset)
		entityX := pos.X - offsetX
		entityY := pos.Y - offsetY

		// Try primary position, fallback to opposite if OOB
		position := r.position
		barX, barY, barLength := r.calculateBar(entityX, entityY, width, height, ratio, position)

		if r.isBarOOB(ctx, barX, barY, barLength, position) {
			position = getOppositePosition(position)
			barX, barY, barLength = r.calculateBar(entityX, entityY, width, height, ratio, position)
		}

		r.renderHealthBar(ctx, buf, barX, barY, barLength, ratio, position)
	}
}

// calculateBar computes bar parameters for a given position
func (r *HealthBarRenderer) calculateBar(entityX, entityY, width, height int, ratio float64, position visual.HealthBarPosition) (barX, barY, barLength int) {
	// Determine bar dimension based on direction
	var barDimension int
	if position == visual.HealthBarLeft || position == visual.HealthBarRight {
		barDimension = height
	} else {
		barDimension = width
	}

	// Calculate bar length (proportional for multi-cell entities)
	if barDimension > 1 && visual.HealthBarProportional {
		barLength = int(math.Ceil(float64(barDimension) * ratio))
	} else {
		barLength = 1
	}

	// Guarantee minimum visibility
	if barLength < visual.HealthBarMinLength {
		barLength = visual.HealthBarMinLength
	}

	// Calculate bar start position
	switch position {
	case visual.HealthBarAbove:
		barX, barY = entityX, entityY-1
	case visual.HealthBarBelow:
		barX, barY = entityX, entityY+height
	case visual.HealthBarLeft:
		barX, barY = entityX-1, entityY
	case visual.HealthBarRight:
		barX, barY = entityX+width, entityY
	default:
		barX, barY = entityX, entityY-1
	}

	return
}

// isBarOOB checks if any part of the health bar is out of bounds
func (r *HealthBarRenderer) isBarOOB(ctx render.RenderContext, barX, barY, barLength int, position visual.HealthBarPosition) bool {
	isVertical := position == visual.HealthBarLeft || position == visual.HealthBarRight

	// Check start position visibility
	_, _, startVisible := ctx.MapToScreen(barX, barY)
	if !startVisible {
		return true
	}

	// Check end position visibility
	if isVertical {
		_, _, endVisible := ctx.MapToScreen(barX, barY+barLength-1)
		if !endVisible {
			return true
		}
	} else {
		_, _, endVisible := ctx.MapToScreen(barX+barLength-1, barY)
		if !endVisible {
			return true
		}
	}

	return false
}

// renderHealthBar draws the health bar cells
func (r *HealthBarRenderer) renderHealthBar(ctx render.RenderContext, buf *render.RenderBuffer, startX, startY, length int, ratio float64, position visual.HealthBarPosition) {
	isVertical := position == visual.HealthBarLeft || position == visual.HealthBarRight

	for i := 0; i < length; i++ {
		var mapX, mapY int
		if isVertical {
			mapX = startX
			mapY = startY + i
		} else {
			mapX = startX + i
			mapY = startY
		}

		screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
		if !visible {
			continue
		}

		r.renderBar(buf, screenX, screenY, visual.HealthBarChar, ratio)
	}
}

// renderCellTrueColor renders with smooth gradient from HeatGradientLUT
func (r *HealthBarRenderer) renderCellTrueColor(buf *render.RenderBuffer, x, y int, ch rune, ratio float64) {
	lutIdx := visual.HealthLUTMin + int(ratio*float64(visual.HealthLUTMax-visual.HealthLUTMin))
	if lutIdx > visual.HealthLUTMax {
		lutIdx = visual.HealthLUTMax
	}
	color := render.HeatGradientLUT[lutIdx]

	buf.SetFgOnly(x, y, ch, color, terminal.AttrNone)
}

// renderCell256 renders with segmented 256-color palette
func (r *HealthBarRenderer) renderCell256(buf *render.RenderBuffer, x, y int, ch rune, ratio float64) {
	// Map ratio to 5 segments
	segment := int(ratio * 5)
	if segment > 4 {
		segment = 4
	}
	if segment < 0 {
		segment = 0
	}

	paletteIdx := visual.Health256LUT[segment]

	// Use 256-color foreground attribute
	buf.Set(x, y, ch, terminal.RGB{R: paletteIdx}, visual.RgbBlack,
		render.BlendFgOnly, 1.0, terminal.AttrFg256)
}