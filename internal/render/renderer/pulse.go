package renderer

import (
	"math"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// PulseRenderer draws the pulses of one style at their centres, in their palettes:
// the ring over what it strikes, the glow under it. A hidden pulse draws nowhere.
type PulseRenderer struct {
	gameCtx *engine.GameContext
	style   component.PulseStyle
	mask    uint8
	draw    func(r *PulseRenderer, ctx render.RenderContext, buf *render.RenderBuffer, p *component.PulseComponent)
	cell    pulseCellRenderer
	colors  *[component.PaletteCount]color.RGB
	console *[component.PaletteCount]uint8
}

// pulseCellRenderer draws one cell of a pulse in the colour mode chosen at construction
type pulseCellRenderer func(buf *render.RenderBuffer, screenX, screenY int, alpha float64, c color.RGB, entry uint8)

// The ring ripples out from three tenths of its pulse's size to all of it over the
// first half of its life and fades over the second; the glow fills the size its
// pulse has swollen to, brightening then fading the same way
const (
	ringFrontStart = 0.3
	ringAlphaMax   = 0.9
	ringCount      = 6.0
	glowAlphaMax   = 0.7
	pulseAlphaMin  = 0.03
)

var (
	ringTrueColor = [component.PaletteCount]color.RGB{
		component.PalettePositive: visual.RgbPulsePositive,
		component.PaletteNegative: visual.RgbPulseNegative,
		component.PaletteHostile:  visual.RgbPulseHostile,
	}
	ring256 = [component.PaletteCount]uint8{
		component.PalettePositive: visual.Pulse256Positive,
		component.PaletteNegative: visual.Pulse256Negative,
		component.PaletteHostile:  visual.Pulse256Hostile,
	}
	// A hostile glow is the storm's green, whose circle fires it
	glowTrueColor = [component.PaletteCount]color.RGB{
		component.PalettePositive: visual.RgbPulsePositive,
		component.PaletteNegative: visual.RgbPulseNegative,
		component.PaletteHostile:  visual.RgbStormGreenPulse,
	}
	glow256 = [component.PaletteCount]uint8{
		component.PalettePositive: visual.Pulse256Positive,
		component.PaletteNegative: visual.Pulse256Negative,
		component.PaletteHostile:  visual.Storm256GreenPulse,
	}
)

// NewPulseRenderer draws ring pulses, over the species they strike
func NewPulseRenderer(gameCtx *engine.GameContext) *PulseRenderer {
	r := &PulseRenderer{gameCtx: gameCtx, style: component.PulseRing, mask: visual.MaskTransient,
		draw: (*PulseRenderer).renderRing, cell: ringCellTrueColor, colors: &ringTrueColor, console: &ring256}
	if gameCtx.World.Resources.Config.ColorMode == terminal.ColorMode256 {
		r.cell = pulseCell256
	}
	return r
}

// NewPulseGlowRenderer draws glowing pulses, under the species that carry them
func NewPulseGlowRenderer(gameCtx *engine.GameContext) *PulseRenderer {
	r := &PulseRenderer{gameCtx: gameCtx, style: component.PulseGlow, mask: visual.MaskComposite,
		draw: (*PulseRenderer).renderGlow, cell: glowCellTrueColor, colors: &glowTrueColor, console: &glow256}
	if gameCtx.World.Resources.Config.ColorMode == terminal.ColorMode256 {
		r.cell = pulseCell256
	}
	return r
}

func ringCellTrueColor(buf *render.RenderBuffer, screenX, screenY int, alpha float64, c color.RGB, _ uint8) {
	buf.Set(screenX, screenY, 0, visual.RgbBlack, c, render.BlendScreen, alpha, terminal.AttrNone)
}

func glowCellTrueColor(buf *render.RenderBuffer, screenX, screenY int, alpha float64, c color.RGB, _ uint8) {
	buf.Set(screenX, screenY, 0, visual.RgbBlack, c, render.BlendAdd, alpha, terminal.AttrNone)
}

// pulseCell256 draws a cell solid where its blend would show, so the rings stay rings
func pulseCell256(buf *render.RenderBuffer, screenX, screenY int, alpha float64, _ color.RGB, entry uint8) {
	if alpha < visual.Effect256Threshold {
		return
	}
	buf.SetBg256(screenX, screenY, entry)
}

// Render draws every pulse of this renderer's style this instance holds
func (r *PulseRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	pulses := r.gameCtx.World.Components.Pulse
	if pulses.CountEntities() == 0 {
		return
	}
	buf.SetWriteMask(r.mask)
	pulses.Each(func(_ core.Entity, p *component.PulseComponent) bool {
		if p.Style == r.style && p.Palette < component.PaletteCount && p.Radius > 0 {
			r.draw(r, ctx, buf, p)
		}
		return true
	})
}

// envelope rises over the first half of a pulse's life and falls over the second
func envelope(progress float64) float64 {
	return 1 - math.Abs(2*progress-1)
}

// renderRing draws expanding concentric ripples whose front reaches the pulse's size
// halfway through its life
func (r *PulseRenderer) renderRing(ctx render.RenderContext, buf *render.RenderBuffer, p *component.PulseComponent) {
	progress := p.Progress()
	alpha := ringAlphaMax * envelope(progress)
	if alpha <= pulseAlphaMin {
		return
	}
	front := p.Size() * min(ringFrontStart+(1-ringFrontStart)*2*progress, 1)
	radiusX, radiusY := p.Radius*front, ctx.CellAspect.Y(p.Radius/2)*front
	invRxSq, invRySq := vmath.EllipseInvRadiiSq(radiusX, radiusY)

	startX, endX, startY, endY, ok := effectBounds(ctx, p.X, p.Y, radiusX, radiusY)
	if !ok {
		return
	}
	// The ripples turn twice over the pulse's life
	phaseOffset := progress * 2 * vmath.TwoPi
	c, entry := r.colors[p.Palette], r.console[p.Palette]
	for mapY := startY; mapY <= endY; mapY++ {
		dy := float64(mapY - p.Y)
		for mapX := startX; mapX <= endX; mapX++ {
			distSq := vmath.EllipseDistSq(float64(mapX-p.X), dy, invRxSq, invRySq)
			if distSq > 1 {
				continue
			}
			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}
			dist := math.Sqrt(distSq)
			ripple := (1 + vmath.Sin(dist*ringCount*vmath.TwoPi-phaseOffset)) / 2
			if cellAlpha := alpha * ripple * (1 - dist); cellAlpha >= pulseAlphaMin {
				r.cell(buf, screenX, screenY, cellAlpha, c, entry)
			}
		}
	}
}

// renderGlow fills the size the pulse has swollen to, brightest at its centre
func (r *PulseRenderer) renderGlow(ctx render.RenderContext, buf *render.RenderBuffer, p *component.PulseComponent) {
	alpha := glowAlphaMax * envelope(p.Progress())
	if alpha <= pulseAlphaMin {
		return
	}
	size := p.Size()
	radiusX, radiusY := p.Radius*size, ctx.CellAspect.Y(p.Radius/2)*size
	invRx, invRy := 1/radiusX, 1/radiusY

	startX, endX, startY, endY, ok := effectBounds(ctx, p.X, p.Y, radiusX, radiusY)
	if !ok {
		return
	}
	c, entry := r.colors[p.Palette], r.console[p.Palette]
	for mapY := startY; mapY <= endY; mapY++ {
		ny := float64(mapY-p.Y) * invRy
		for mapX := startX; mapX <= endX; mapX++ {
			nx := float64(mapX-p.X) * invRx
			distSq := nx*nx + ny*ny
			if distSq > 1 {
				continue
			}
			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}
			if cellAlpha := alpha * (1 - distSq); cellAlpha >= pulseAlphaMin {
				r.cell(buf, screenX, screenY, cellAlpha, c, entry)
			}
		}
	}
}
