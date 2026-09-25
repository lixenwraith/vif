package renderer

import (
	"math"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// PulseRenderer draws disruptor pulse expanding ring effect
type PulseRenderer struct {
	gameCtx    *engine.GameContext
	renderCell pulseCellRenderer

	// Cached animation constants
	radiusMultMin   float64 // 0.3
	radiusMultRange float64 // 0.7
	alphaMax        float64 // 0.9
	alphaThreshold  float64 // 0.03
	ringCount       float64 // 6
}

// pulseCellRenderer draws one ripple cell in the colour mode chosen at construction
type pulseCellRenderer func(buf *render.RenderBuffer, screenX, screenY int, alpha float64, palette component.WeaponPalette)

var (
	pulseTrueColor = [component.PaletteCount]color.RGB{
		component.PalettePositive: visual.RgbPulsePositive,
		component.PaletteNegative: visual.RgbPulseNegative,
		component.PaletteHostile:  visual.RgbPulseHostile,
	}
	pulse256 = [component.PaletteCount]uint8{
		component.PalettePositive: visual.Pulse256Positive,
		component.PaletteNegative: visual.Pulse256Negative,
		component.PaletteHostile:  visual.Pulse256Hostile,
	}
)

func NewPulseRenderer(gameCtx *engine.GameContext) *PulseRenderer {
	r := &PulseRenderer{
		gameCtx:         gameCtx,
		renderCell:      pulseCellTrueColor,
		radiusMultMin:   0.3,
		radiusMultRange: 0.7,
		alphaMax:        0.9,
		alphaThreshold:  0.03,
		ringCount:       6.0,
	}
	if gameCtx.World.Resources.Config.ColorMode == terminal.ColorMode256 {
		r.renderCell = pulseCell256
	}
	return r
}

func pulseCellTrueColor(buf *render.RenderBuffer, screenX, screenY int, alpha float64, palette component.WeaponPalette) {
	buf.Set(screenX, screenY, 0, visual.RgbBlack, pulseTrueColor[palette], render.BlendScreen, alpha, terminal.AttrNone)
}

// pulseCell256 draws a ripple cell solid where its blend would show, so the rings stay rings
func pulseCell256(buf *render.RenderBuffer, screenX, screenY int, alpha float64, palette component.WeaponPalette) {
	if alpha < visual.Effect256Threshold {
		return
	}
	buf.SetBg256(screenX, screenY, pulse256[palette])
}

// Render draws every pulse ring this instance produced
func (r *PulseRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	pulses := r.gameCtx.World.Resources.Transient.PulseEffects()
	if len(pulses) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)
	for i := range pulses {
		p := &pulses[i]
		if p.DurNano <= 0 {
			continue
		}
		// Progress runs from zero at the start to one at the end.
		progress := float64(p.Age) / float64(p.DurNano)
		if progress < 0.0 || progress > 1.0 {
			continue
		}
		r.renderPulse(ctx, buf, p.X, p.Y, progress, p.Palette)
	}
}

func (r *PulseRenderer) renderPulse(ctx render.RenderContext, buf *render.RenderBuffer,
	originX, originY int, progress float64, palette component.WeaponPalette) {

	// Two-phase animation: expand (0-0.5) then fade (0.5-1.0)
	pulsePhase := progress * 2.0

	var radiusMult, baseAlpha float64
	if pulsePhase < 1.0 {
		// radiusMult = 0.3 + 0.7 * phase
		radiusMult = r.radiusMultMin + r.radiusMultRange*pulsePhase
		// baseAlpha = 0.9 * phase
		baseAlpha = r.alphaMax * pulsePhase
	} else {
		radiusMult = 1.0
		// baseAlpha = 0.9 * (2.0 - phase)
		baseAlpha = r.alphaMax * (2.0 - pulsePhase)
	}

	if baseAlpha <= r.alphaThreshold {
		return
	}

	// Scale precomputed inverse radii by 1/radiusMult²
	// invRxSq_scaled = invRxSq_base / radiusMult²
	radiusMultSq := radiusMult * radiusMult
	if radiusMultSq == 0.0 {
		return
	}
	invRxSq := parameter.PulseRadiusInvRxSq / radiusMultSq
	invRySq := parameter.PulseRadiusInvRySq / radiusMultSq

	// Integer bounds from scaled radii
	intRadiusX := int(math.Floor(parameter.PulseRadiusX*radiusMult)) + 1
	intRadiusY := int(math.Floor(parameter.PulseRadiusY*radiusMult)) + 1

	mapStartX := max(0, originX-intRadiusX)
	mapEndX := min(ctx.MapWidth-1, originX+intRadiusX)
	mapStartY := max(0, originY-intRadiusY)
	mapEndY := min(ctx.MapHeight-1, originY+intRadiusY)

	// Ripple phase offset advances by two rotations over the effect.
	phaseOffset := progress * 2.0 * vmath.TwoPi

	for mapY := mapStartY; mapY <= mapEndY; mapY++ {
		dy := float64(mapY - originY)

		for mapX := mapStartX; mapX <= mapEndX; mapX++ {
			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			dx := float64(mapX - originX)

			// Normalized squared distance (<=1 means inside ellipse)
			distSq := vmath.EllipseDistSqF(dx, dy, invRxSq, invRySq)
			if distSq > 1.0 {
				continue
			}

			// Normalized distance [0, 1]
			dist := math.Sqrt(distSq)

			// Concentric ripples: sin(dist * ringCount - phaseOffset)
			angle := dist*r.ringCount*vmath.TwoPi - phaseOffset
			rippleSin := vmath.SinF(angle)

			// rippleIntensity = 0.5 + 0.5 * sin
			rippleIntensity := (1.0 + rippleSin) / 2.0

			// Edge falloff: 1.0 - dist
			edgeFalloff := 1.0 - dist

			// Final alpha
			cellAlpha := baseAlpha * rippleIntensity * edgeFalloff
			if cellAlpha < r.alphaThreshold {
				continue
			}

			r.renderCell(buf, screenX, screenY, cellAlpha, palette)
		}
	}
}
