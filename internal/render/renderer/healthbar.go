package renderer

import (
	"math"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// HealthBarRenderer draws health indicators above combat entities
type HealthBarRenderer struct {
	gameCtx  *engine.GameContext
	position visual.HealthBarPosition
	palette  bool

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

	if gameCtx.World.Resources.Config.ColorMode == terminal.ColorMode256 {
		r.renderBar, r.palette = r.renderCell256, true
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

// healthBarPlace is where a kind's bar stands: beside its footprint, on the side
// the renderer prefers or the other where that leaves the view; centered on its
// header; or pinned to the view's top row, for a body as large as the view
type healthBarPlace uint8

const (
	barBeside healthBarPlace = iota
	barCentered
	barPinned
)

var healthBarSpecs = [component.CombatEntityCount]struct {
	foot  vmath.Footprint
	maxHP int
	place healthBarPlace
}{
	component.CombatEntityDrain:    {component.Footprints[component.SpeciesDrain], parameter.CombatInitialHPDrain, barBeside},
	component.CombatEntitySwarm:    {component.Footprints[component.SpeciesSwarm], parameter.CombatInitialHPSwarm, barBeside},
	component.CombatEntityQuasar:   {component.Footprints[component.SpeciesQuasar], parameter.CombatInitialHPQuasar, barBeside},
	component.CombatEntityKraken:   {vmath.Footprint{Width: visual.KrakenHealthBarWidth, Height: 1}, parameter.KrakenInitialHP, barCentered},
	component.CombatEntityDevourer: {vmath.Footprint{}, parameter.DevourerInitialHP, barPinned},
}

// Render draws health bars for all applicable combat entities
func (r *HealthBarRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	if !visual.HealthBarEnabled {
		return
	}

	combats := r.gameCtx.World.Components.Combat
	if combats.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskHealthBar)

	combats.Each(func(entity core.Entity, combatComp *component.CombatComponent) bool {
		if uint(combatComp.CombatEntityType) >= uint(len(healthBarSpecs)) {
			return true
		}
		spec := healthBarSpecs[combatComp.CombatEntityType]
		// A data species fights as a code row, but its body and hit points are its own
		if b, ok := r.gameCtx.World.Components.Behavior.GetComponent(entity); ok && int(b.Program) < len(r.gameCtx.World.Resources.Behaviors) {
			p := &r.gameCtx.World.Resources.Behaviors[b.Program]
			spec.foot, spec.maxHP = p.Footprint, p.HitPoints
		}
		// A Kraken's and a Devourer's full bar is the hit points it rose with
		if k, ok := r.gameCtx.World.Components.Kraken.GetComponent(entity); ok && k.MaxHitPoints > 0 {
			spec.maxHP = k.MaxHitPoints
		}
		if d, ok := r.gameCtx.World.Components.Devourer.GetComponent(entity); ok && d.MaxHitPoints > 0 {
			spec.maxHP = d.MaxHitPoints
		}
		if spec.maxHP == 0 {
			return true
		}
		maxHP := spec.maxHP
		pos, ok := r.gameCtx.World.Positions.GetPosition(entity)
		if !ok {
			return true
		}

		// Health ratio clamped to [0, 1]
		ratio := float64(combatComp.HitPoints) / float64(maxHP)
		if ratio > 1.0 {
			ratio = 1.0
		}
		if ratio < 0 {
			ratio = 0
		}

		switch spec.place {
		case barPinned:
			r.renderPinned(ctx, buf, ratio)
			return true
		case barCentered:
			length := max(visual.HealthBarMinLength, int(math.Ceil(float64(spec.foot.Width)*ratio)))
			x, y := pos.X-length/2, pos.Y
			if _, _, visible := ctx.MapToScreen(pos.X, pos.Y); visible {
				x, y, length = r.slide(ctx, x, y, length, visual.HealthBarAbove)
			}
			r.renderHealthBar(ctx, buf, x, y, length, ratio, visual.HealthBarAbove)
			return true
		}

		// Try primary position, fallback to opposite if OOB; a body that shows but
		// leaves neither side whole, as tall as the view or as wide, slides its bar in
		body := spec.foot.At(pos.X, pos.Y)
		position := r.position
		barX, barY, barLength := r.calculateBar(body.X, body.Y, body.Width, body.Height, ratio, position)

		if r.isBarOOB(ctx, barX, barY, barLength, position) {
			opposite := getOppositePosition(position)
			x, y, length := r.calculateBar(body.X, body.Y, body.Width, body.Height, ratio, opposite)
			switch {
			case !r.isBarOOB(ctx, x, y, length, opposite):
				barX, barY, barLength, position = x, y, length, opposite
			case bodyShows(ctx, body):
				barX, barY, barLength = r.slide(ctx, barX, barY, barLength, position)
			}
		}

		r.renderHealthBar(ctx, buf, barX, barY, barLength, ratio, position)
		return true
	})
}

// bodyShows reports whether any cell of a body lies in the visible map
func bodyShows(ctx render.RenderContext, body vmath.Area) bool {
	minX, minY, maxX, maxY := ctx.VisibleMapBounds()
	return body.X <= maxX && body.X+body.Width > minX && body.Y <= maxY && body.Y+body.Height > minY
}

// slide moves a bar into the visible map, cut to its width or its height
func (r *HealthBarRenderer) slide(ctx render.RenderContext, x, y, length int, position visual.HealthBarPosition) (int, int, int) {
	minX, minY, maxX, maxY := ctx.VisibleMapBounds()
	if position == visual.HealthBarLeft || position == visual.HealthBarRight {
		length = min(length, maxY-minY+1)
		return min(max(x, minX), maxX), min(max(y, minY), maxY-length+1), length
	}
	length = min(length, maxX-minX+1)
	return min(max(x, minX), maxX-length+1), min(max(y, minY), maxY), length
}

// renderPinned draws a bar on the view's top row, centered across a share of its
// width, on a backing that reads over the body under it, the hit points lost dotted
func (r *HealthBarRenderer) renderPinned(ctx render.RenderContext, buf *render.RenderBuffer, ratio float64) {
	minX, minY, maxX, _ := ctx.VisibleMapBounds()
	width := max(visual.HealthBarMinLength, int(float64(maxX-minX+1)*visual.HealthBarPinnedShare))
	filled := max(visual.HealthBarMinLength, int(math.Ceil(float64(width)*ratio)))
	x0 := minX + (maxX-minX+1-width)/2
	for i := range width {
		sx, sy, visible := ctx.MapToScreen(x0+i, minY)
		if !visible {
			continue
		}
		if r.palette {
			buf.SetBg256(sx, sy, visual.ConBlack)
		} else {
			buf.SetBgOnly(sx, sy, color.Lerp(buf.BackgroundAt(sx, sy, visual.RgbBackground), visual.RgbBlack, visual.HealthBarPinnedBacking))
		}
		switch {
		case i < filled:
			r.renderBar(buf, sx, sy, visual.HealthBarChar, ratio)
		case r.palette:
			buf.SetFgOnly(sx, sy, visual.HealthBarLostChar, color.RGB{R: visual.HealthBar256Lost}, terminal.AttrFg256)
		default:
			buf.SetFgOnly(sx, sy, visual.HealthBarLostChar, visual.RgbHealthBarLost, terminal.AttrNone)
		}
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

	for i := range length {
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
	c := render.HeatGradientLUT[lutIdx]

	buf.SetFgOnly(x, y, ch, c, terminal.AttrNone)
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
	buf.Set(x, y, ch, color.RGB{R: paletteIdx}, visual.RgbBlack,
		render.BlendFgOnly, 1.0, terminal.AttrFg256)
}
