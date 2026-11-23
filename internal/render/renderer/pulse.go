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

// PulseRenderer draws the pulses of one layer at their centers, in their palettes:
// a ring and a wave over what they strike, a glow under it. A hidden pulse draws
// nowhere.
type PulseRenderer struct {
	gameCtx *engine.GameContext
	mask    uint8
	looks   [component.PulseStyleCount]*pulseLook // nil for a style another layer draws
}

// pulseLook is how one style draws: its shape, its cell's blend and its colors
type pulseLook struct {
	draw    func(ctx render.RenderContext, buf *render.RenderBuffer, p *component.PulseComponent, look *pulseLook)
	cell    pulseCellRenderer
	colors  [component.PaletteCount]color.RGB
	console [component.PaletteCount]uint8
}

// pulseCellRenderer draws one cell of a pulse in the color mode chosen at construction
type pulseCellRenderer func(buf *render.RenderBuffer, screenX, screenY int, alpha float64, c color.RGB, entry uint8)

// The ring ripples out from three tenths of its pulse's size to all of it over the
// first half of its life and fades over the second; the glow fills its pulse's
// size, brightening then fading the same way. A wave is a flat band behind its
// pulse's edge, waveRim columns of rim and waveWake of wake, each one tone, so a
// cell changes only as a tone reaches or leaves it; a console draws the rim alone.
const (
	ringFrontStart = 0.3
	ringAlphaMax   = 0.9
	ringCount      = 6.0
	glowAlphaMax   = 0.7
	pulseAlphaMin  = 0.03
	waveRim        = 2.0
	waveWake       = 4.0
	waveRimAlpha   = 0.8
	waveWakeAlpha  = 0.2
)

// The ring's colors are the wave's; a hostile glow is the storm's green, whose
// circle fires it
var (
	ringLook = pulseLook{draw: renderRing, cell: ringCellTrueColor,
		colors:  [component.PaletteCount]color.RGB{visual.RgbPulsePositive, visual.RgbPulseNegative, visual.RgbPulseHostile},
		console: [component.PaletteCount]uint8{visual.Pulse256Positive, visual.Pulse256Negative, visual.Pulse256Hostile}}
	waveLook = pulseLook{draw: renderWave, cell: waveCellTrueColor, colors: ringLook.colors, console: ringLook.console}
	glowLook = pulseLook{draw: renderGlow, cell: glowCellTrueColor,
		colors:  [component.PaletteCount]color.RGB{visual.RgbPulsePositive, visual.RgbPulseNegative, visual.RgbStormGreenPulse},
		console: [component.PaletteCount]uint8{visual.Pulse256Positive, visual.Pulse256Negative, visual.Storm256GreenPulse}}
)

// NewPulseRenderer draws ring and wave pulses, over the species they strike
func NewPulseRenderer(gameCtx *engine.GameContext) *PulseRenderer {
	return newPulseRenderer(gameCtx, visual.MaskTransient, map[component.PulseStyle]pulseLook{
		component.PulseRing: ringLook, component.PulseWave: waveLook,
	})
}

// NewPulseGlowRenderer draws glowing pulses, under the species that carry them
func NewPulseGlowRenderer(gameCtx *engine.GameContext) *PulseRenderer {
	return newPulseRenderer(gameCtx, visual.MaskComposite, map[component.PulseStyle]pulseLook{component.PulseGlow: glowLook})
}

func newPulseRenderer(gameCtx *engine.GameContext, mask uint8, looks map[component.PulseStyle]pulseLook) *PulseRenderer {
	r := &PulseRenderer{gameCtx: gameCtx, mask: mask}
	for style, look := range looks {
		if gameCtx.World.Resources.Config.ColorMode == terminal.ColorMode256 {
			look.cell = pulseCell256
		}
		r.looks[style] = &look
	}
	return r
}

func ringCellTrueColor(buf *render.RenderBuffer, screenX, screenY int, alpha float64, c color.RGB, _ uint8) {
	buf.Set(screenX, screenY, 0, visual.RgbBlack, c, render.BlendScreen, alpha, terminal.AttrNone)
}

func glowCellTrueColor(buf *render.RenderBuffer, screenX, screenY int, alpha float64, c color.RGB, _ uint8) {
	buf.Set(screenX, screenY, 0, visual.RgbBlack, c, render.BlendAdd, alpha, terminal.AttrNone)
}

func waveCellTrueColor(buf *render.RenderBuffer, screenX, screenY int, alpha float64, c color.RGB, _ uint8) {
	buf.SetBgScreen(screenX, screenY, c, visual.RgbBackground, alpha)
}

// pulseCell256 draws a cell solid where its blend would show, so the rings stay rings
func pulseCell256(buf *render.RenderBuffer, screenX, screenY int, alpha float64, _ color.RGB, entry uint8) {
	if alpha < visual.Effect256Threshold {
		return
	}
	buf.SetBg256(screenX, screenY, entry)
}

// Render draws every pulse this instance holds of a style this layer draws
func (r *PulseRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	pulses := r.gameCtx.World.Components.Pulse
	if pulses.CountEntities() == 0 {
		return
	}
	buf.SetWriteMask(r.mask)
	pulses.Each(func(_ core.Entity, p *component.PulseComponent) bool {
		if p.Style < component.PulseStyleCount && p.Palette < component.PaletteCount && p.Radius > 0 {
			if look := r.looks[p.Style]; look != nil {
				look.draw(ctx, buf, p, look)
			}
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
func renderRing(ctx render.RenderContext, buf *render.RenderBuffer, p *component.PulseComponent, look *pulseLook) {
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
	c, entry := look.colors[p.Palette], look.console[p.Palette]
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
				look.cell(buf, screenX, screenY, cellAlpha, c, entry)
			}
		}
	}
}

// renderGlow fills the size the pulse has swollen to, brightest at its center
func renderGlow(ctx render.RenderContext, buf *render.RenderBuffer, p *component.PulseComponent, look *pulseLook) {
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
	c, entry := look.colors[p.Palette], look.console[p.Palette]
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
				look.cell(buf, screenX, screenY, cellAlpha, c, entry)
			}
		}
	}
}

// renderWave draws the band behind the pulse's edge, row by row over the columns
// the band covers either side of its center
func renderWave(ctx render.RenderContext, buf *render.RenderBuffer, p *component.PulseComponent, look *pulseLook) {
	front := p.Radius * p.Size()
	rim, back := max(front-waveRim, 0), max(front-waveRim-waveWake, 0)
	startX, endX, startY, endY, ok := effectBounds(ctx, p.X, p.Y, front, ctx.CellAspect.FromCircular(front))
	if !ok {
		return
	}
	c, entry := look.colors[p.Palette], look.console[p.Palette]
	for mapY := startY; mapY <= endY; mapY++ {
		dy := ctx.CellAspect.Circular(float64(mapY - p.Y))
		reach := front*front - dy*dy
		if reach < 0 {
			continue
		}
		far, near := int(math.Sqrt(reach)), 0
		if inner := back*back - dy*dy; inner > 0 {
			near = int(math.Sqrt(inner)) + 1
		}
		for dx := -far; dx <= far; dx++ {
			if dx > -near && dx < near {
				dx = near
			}
			mapX := p.X + dx
			if mapX < startX || mapX > endX {
				continue
			}
			alpha := waveWakeAlpha
			if float64(dx*dx)+dy*dy > rim*rim {
				alpha = waveRimAlpha
			}
			if screenX, screenY, visible := ctx.MapToScreen(mapX, mapY); visible {
				look.cell(buf, screenX, screenY, alpha, c, entry)
			}
		}
	}
}
