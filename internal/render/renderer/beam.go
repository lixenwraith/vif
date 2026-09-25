package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// BeamRenderer draws every beam in flight from its BeamComponent: a white core with
// palette-coloured sides where the ray widens, brighter the more charges fired it
// and fading over its last quarter. A warning is the core line alone. Walls stay clear.
type BeamRenderer struct {
	gameCtx  *engine.GameContext
	drawCell beamCellRenderer
}

// beamCellRenderer draws one beam cell, core or side, in the colour mode chosen at construction
type beamCellRenderer func(buf *render.RenderBuffer, screenX, screenY int, alpha float64, palette component.WeaponPalette, core, warning bool)

var (
	beamSides = [component.PaletteCount]color.RGB{
		component.PalettePositive: visual.RgbBeamPositive,
		component.PaletteNegative: visual.RgbBeamNegative,
		component.PaletteHostile:  visual.RgbBeamHostile,
	}
	beamSides256 = [component.PaletteCount]uint8{
		component.PalettePositive: visual.Beam256Positive,
		component.PaletteNegative: visual.Beam256Negative,
		component.PaletteHostile:  visual.Beam256Hostile,
	}
)

func NewBeamRenderer(gameCtx *engine.GameContext) *BeamRenderer {
	r := &BeamRenderer{gameCtx: gameCtx, drawCell: beamCellTrueColor}
	if gameCtx.World.Resources.Config.ColorMode == terminal.ColorMode256 {
		r.drawCell = beamCell256
	}
	return r
}

func beamCellTrueColor(buf *render.RenderBuffer, screenX, screenY int, alpha float64, palette component.WeaponPalette, core, warning bool) {
	c := beamSides[palette]
	if core && !warning {
		c = visual.RgbBeamCore
	}
	buf.Set(screenX, screenY, 0, visual.RgbBlack, c, render.BlendScreen, alpha, terminal.AttrNone)
}

// beamCell256 fills a cell solid where its blend would show
func beamCell256(buf *render.RenderBuffer, screenX, screenY int, alpha float64, palette component.WeaponPalette, core, warning bool) {
	if alpha < visual.Effect256Threshold {
		return
	}
	switch {
	case warning:
		buf.SetBg256(screenX, screenY, visual.Beam256Warning)
	case core:
		buf.SetBg256(screenX, screenY, visual.Beam256Core)
	default:
		buf.SetBg256(screenX, screenY, beamSides256[palette])
	}
}

// Render draws every beam this instance holds: its own cursors' and every mount's
func (r *BeamRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	beams := r.gameCtx.World.Components.Beam
	if beams.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)
	gameTimeMs := r.gameCtx.World.Resources.Time.GameTime.UnixMilli()
	flicker := 0.85 + 0.15*vmath.SinF(float64(gameTimeMs%120)/120*vmath.TwoPi)
	beams.Each(func(_ core.Entity, b *component.BeamComponent) bool {
		if b.Duration <= 0 || b.Palette >= component.PaletteCount {
			return true
		}
		left := float64(b.Remaining) / float64(b.Duration)
		if b.Phase == component.BeamWarning {
			// Brightens toward the strike so the lane reads as a countdown
			r.drawRay(ctx, buf, b, (0.7-0.4*left)*flicker, 0, true)
			return true
		}
		sides := min(0.55+0.15*float64(b.Scale-1), 1)
		r.drawRay(ctx, buf, b, min(4*left, 1)*flicker, sides, false)
		return true
	})
}

// drawRay draws a beam's cells, a warning's core only, its sides at sides of the core's alpha
func (r *BeamRenderer) drawRay(ctx render.RenderContext, buf *render.RenderBuffer, b *component.BeamComponent, alpha, sides float64, warning bool) {
	positions := r.gameCtx.World.Positions
	for i := 1; i <= b.Ray.Length; i++ {
		half := b.Ray.Half(i)
		if warning {
			half = 0
		}
		for across := -half; across <= half; across++ {
			x, y := b.Ray.Cell(i, across)
			if x < 0 || y < 0 || x >= ctx.MapWidth || y >= ctx.MapHeight ||
				positions.HasBlockingWallAt(x, y, component.WallBlockKinetic) {
				continue
			}
			screenX, screenY, visible := ctx.MapToScreen(x, y)
			if !visible {
				continue
			}
			a := alpha
			if across != 0 {
				a *= sides
			}
			r.drawCell(buf, screenX, screenY, a, b.Palette, across == 0, warning)
		}
	}
}
