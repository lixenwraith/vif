package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// RayRenderer draws every ray in flight from its RayComponent: a white core with
// palette-coloured sides where the ray widens, brighter the more charges fired it
// and fading over its last quarter. A warning is the core line alone. Walls stay clear.
type RayRenderer struct {
	gameCtx  *engine.GameContext
	drawCell rayCellRenderer
}

// rayCellRenderer draws one ray cell, core or side, in the colour mode chosen at construction
type rayCellRenderer func(buf *render.RenderBuffer, screenX, screenY int, alpha float64, palette component.WeaponPalette, core, warning bool)

var (
	raySides = [component.PaletteCount]color.RGB{
		component.PalettePositive: visual.RgbRayPositive,
		component.PaletteNegative: visual.RgbRayNegative,
		component.PaletteHostile:  visual.RgbRayHostile,
	}
	raySides256 = [component.PaletteCount]uint8{
		component.PalettePositive: visual.Ray256Positive,
		component.PaletteNegative: visual.Ray256Negative,
		component.PaletteHostile:  visual.Ray256Hostile,
	}
)

func NewRayRenderer(gameCtx *engine.GameContext) *RayRenderer {
	r := &RayRenderer{gameCtx: gameCtx, drawCell: rayCellTrueColor}
	if gameCtx.World.Resources.Config.ColorMode == terminal.ColorMode256 {
		r.drawCell = rayCell256
	}
	return r
}

func rayCellTrueColor(buf *render.RenderBuffer, screenX, screenY int, alpha float64, palette component.WeaponPalette, core, warning bool) {
	c := raySides[palette]
	if core && !warning {
		c = visual.RgbRayCore
	}
	buf.Set(screenX, screenY, 0, visual.RgbBlack, c, render.BlendScreen, alpha, terminal.AttrNone)
}

// rayCell256 fills a cell solid where its blend would show
func rayCell256(buf *render.RenderBuffer, screenX, screenY int, alpha float64, palette component.WeaponPalette, core, warning bool) {
	if alpha < visual.Effect256Threshold {
		return
	}
	switch {
	case warning:
		buf.SetBg256(screenX, screenY, visual.Ray256Warning)
	case core:
		buf.SetBg256(screenX, screenY, visual.Ray256Core)
	default:
		buf.SetBg256(screenX, screenY, raySides256[palette])
	}
}

// Render draws every ray this instance holds: its own cursors' and every mount's
func (r *RayRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	rays := r.gameCtx.World.Components.Ray
	if rays.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)
	gameTimeMs := r.gameCtx.World.Resources.Time.GameTime.UnixMilli()
	flicker := 0.85 + 0.15*vmath.SinF(float64(gameTimeMs%120)/120*vmath.TwoPi)
	rays.Each(func(_ core.Entity, b *component.RayComponent) bool {
		if b.Duration <= 0 || b.Palette >= component.PaletteCount {
			return true
		}
		left := float64(b.Remaining) / float64(b.Duration)
		if b.Phase == component.RayWarning {
			// Brightens toward the strike so the lane reads as a countdown
			r.drawRay(ctx, buf, b, (0.7-0.4*left)*flicker, 0, true)
			return true
		}
		sides := min(0.55+0.15*float64(b.Scale-1), 1)
		r.drawRay(ctx, buf, b, min(4*left, 1)*flicker, sides, false)
		return true
	})
}

// drawRay draws a ray's cells, a warning's core only, its sides at sides of the core's alpha
func (r *RayRenderer) drawRay(ctx render.RenderContext, buf *render.RenderBuffer, b *component.RayComponent, alpha, sides float64, warning bool) {
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
