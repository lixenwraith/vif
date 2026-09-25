package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// BeamRenderer draws beams: a pulsing warning line down the centre, then the full
// band fading over its firing window. Wall cells stay clear.
type BeamRenderer struct {
	gameCtx  *engine.GameContext
	drawCell beamCellRenderer
}

// beamCellRenderer draws one beam cell in the colour mode chosen at construction
type beamCellRenderer func(buf *render.RenderBuffer, screenX, screenY int, alpha float64, palette component.WeaponPalette, warning bool)

var (
	beamTrueColor = [component.PaletteCount]color.RGB{
		component.PalettePositive: visual.RgbBeamPositive,
		component.PaletteNegative: visual.RgbBeamNegative,
		component.PaletteHostile:  visual.RgbBeamHostile,
	}
	beam256 = [component.PaletteCount]uint8{
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

func beamCellTrueColor(buf *render.RenderBuffer, screenX, screenY int, alpha float64, palette component.WeaponPalette, _ bool) {
	buf.Set(screenX, screenY, 0, visual.RgbBlack, beamTrueColor[palette], render.BlendScreen, alpha, terminal.AttrNone)
}

// beamCell256 fills a cell solid where its blend would show
func beamCell256(buf *render.RenderBuffer, screenX, screenY int, alpha float64, palette component.WeaponPalette, warning bool) {
	if alpha < visual.Effect256Threshold {
		return
	}
	if warning {
		buf.SetBg256(screenX, screenY, visual.Beam256Warning)
		return
	}
	buf.SetBg256(screenX, screenY, beam256[palette])
}

// Render draws every beam this instance produced or derived
func (r *BeamRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	beams := r.gameCtx.World.Resources.Transient.BeamEffects()
	if len(beams) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)
	gameTimeMs := r.gameCtx.World.Resources.Time.GameTime.UnixMilli()
	flicker := 0.85 + 0.15*vmath.SinF(float64(gameTimeMs%120)/120*vmath.TwoPi)
	for i := range beams {
		b := &beams[i]
		if b.Age < b.WarningNano {
			// Brightens toward the strike so the lane reads as a countdown
			r.drawBand(ctx, buf, b, 0, (0.3+0.4*float64(b.Age)/float64(b.WarningNano))*flicker, true)
			continue
		}
		fade := 1 - float64(b.Age-b.WarningNano)/float64(b.DurNano-b.WarningNano)
		r.drawBand(ctx, buf, b, b.Band.Half, fade*flicker, false)
	}
}

// drawBand draws a beam's cells up to half cells across, its edges dimmer than its core
func (r *BeamRenderer) drawBand(ctx render.RenderContext, buf *render.RenderBuffer, b *engine.BeamEffect, half int, alpha float64, warning bool) {
	positions := r.gameCtx.World.Positions
	for along := 1; along <= b.Band.Length; along++ {
		for across := -half; across <= half; across++ {
			x, y := b.Band.Cell(along, across)
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
				a *= 0.6
			}
			r.drawCell(buf, screenX, screenY, a, b.Palette, warning)
		}
	}
}
