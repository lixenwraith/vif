package renderer

import (
	"math"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// PulseRenderer draws disruptor pulse expanding ring effect
type PulseRenderer struct {
	gameCtx *engine.GameContext

	// Cached animation constants
	radiusMultMin   float64 // 0.3
	radiusMultRange float64 // 0.7
	alphaMax        float64 // 0.9
	alphaThreshold  float64 // 0.03
	ringCount       float64 // 6
}

func NewPulseRenderer(gameCtx *engine.GameContext) *PulseRenderer {
	return &PulseRenderer{
		gameCtx:         gameCtx,
		radiusMultMin:   0.3,
		radiusMultRange: 0.7,
		alphaMax:        0.9,
		alphaThreshold:  0.03,
		ringCount:       6.0,
	}
}

func (r *PulseRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	cursorEntity := r.gameCtx.World.Resources.Player.Entity

	pulseComp, ok := r.gameCtx.World.Components.Pulse.GetPtr(cursorEntity)
	if !ok {
		return
	}

	// Progress runs from zero at the start to one at the end.
	remainingNs := pulseComp.Remaining.Nanoseconds()
	durationNs := pulseComp.Duration.Nanoseconds()
	if durationNs == 0 {
		return
	}
	progress := 1.0 - float64(remainingNs)/float64(durationNs)
	if progress < 0.0 || progress > 1.0 {
		return
	}

	negativeEnergy := false
	if energyComp, ok := r.gameCtx.World.Components.Energy.GetPtr(cursorEntity); ok {
		negativeEnergy = energyComp.Current < 0
	}

	buf.SetWriteMask(visual.MaskTransient)
	r.renderPulse(ctx, buf, pulseComp.OriginX, pulseComp.OriginY, progress, negativeEnergy)
}

func (r *PulseRenderer) renderPulse(ctx render.RenderContext, buf *render.RenderBuffer,
	originX, originY int, progress float64, negativeEnergy bool) {

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

	var pulseColor color.RGB
	if negativeEnergy {
		pulseColor = visual.RgbPulseNegative
	} else {
		pulseColor = visual.RgbPulsePositive
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

			buf.Set(screenX, screenY, 0, visual.RgbBlack, pulseColor, render.BlendScreen, cellAlpha, terminal.AttrNone)
		}
	}
}
