package renderer

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// PulseRenderer draws disruptor pulse expanding ring effect
type PulseRenderer struct {
	gameCtx *engine.GameContext

	// Cached Q32.32 constants (avoid FromFloat in hot path)
	radiusMultMin   int64 // 0.3
	radiusMultRange int64 // 0.7
	alphaMax        int64 // 0.9
	alphaThreshold  int64 // 0.03
	ringCount       int64 // 6
}

func NewPulseRenderer(gameCtx *engine.GameContext) *PulseRenderer {
	return &PulseRenderer{
		gameCtx:         gameCtx,
		radiusMultMin:   vmath.FromFloat(0.3),
		radiusMultRange: vmath.FromFloat(0.7),
		alphaMax:        vmath.FromFloat(0.9),
		alphaThreshold:  vmath.FromFloat(0.03),
		ringCount:       vmath.FromInt(6),
	}
}

func (r *PulseRenderer) Priority() render.RenderPriority {
	return render.PriorityField
}

func (r *PulseRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	cursorEntity := r.gameCtx.World.Resources.Player.Entity

	pulseComp, ok := r.gameCtx.World.Components.Pulse.GetComponent(cursorEntity)
	if !ok {
		return
	}

	// Progress: 0 (start) -> Scale (end) in Q32.32
	remainingNs := pulseComp.Remaining.Nanoseconds()
	durationNs := pulseComp.Duration.Nanoseconds()
	if durationNs == 0 {
		return
	}
	progress := vmath.Scale - vmath.MulDiv(remainingNs, vmath.Scale, durationNs)
	if progress < 0 || progress > vmath.Scale {
		return
	}

	negativeEnergy := false
	if energyComp, ok := r.gameCtx.World.Components.Energy.GetComponent(cursorEntity); ok {
		negativeEnergy = energyComp.Current < 0
	}

	buf.SetWriteMask(visual.MaskTransient)
	r.renderPulse(ctx, buf, pulseComp.OriginX, pulseComp.OriginY, progress, negativeEnergy)
}

func (r *PulseRenderer) renderPulse(ctx render.RenderContext, buf *render.RenderBuffer,
	originX, originY int, progress int64, negativeEnergy bool) {

	// Two-phase animation: expand (0-0.5) then fade (0.5-1.0)
	pulsePhase := progress << 1

	var radiusMult, baseAlpha int64
	if pulsePhase < vmath.Scale {
		// radiusMult = 0.3 + 0.7 * phase
		radiusMult = r.radiusMultMin + vmath.Mul(r.radiusMultRange, pulsePhase)
		// baseAlpha = 0.9 * phase
		baseAlpha = vmath.Mul(r.alphaMax, pulsePhase)
	} else {
		radiusMult = vmath.Scale
		// baseAlpha = 0.9 * (2.0 - phase)
		baseAlpha = vmath.Mul(r.alphaMax, (vmath.Scale<<1)-pulsePhase)
	}

	if baseAlpha <= r.alphaThreshold {
		return
	}

	var pulseColor terminal.RGB
	if negativeEnergy {
		pulseColor = visual.RgbPulseNegative
	} else {
		pulseColor = visual.RgbPulsePositive
	}

	// Scale precomputed inverse radii by 1/radiusMult²
	// invRxSq_scaled = invRxSq_base / radiusMult²
	radiusMultSq := vmath.Mul(radiusMult, radiusMult)
	invRxSq := vmath.Div(parameter.PulseRadiusInvRxSq, radiusMultSq)
	invRySq := vmath.Div(parameter.PulseRadiusInvRySq, radiusMultSq)

	// Integer bounds from scaled radii
	intRadiusX := vmath.ToInt(vmath.Mul(parameter.PulseRadiusX, radiusMult)) + 1
	intRadiusY := vmath.ToInt(vmath.Mul(parameter.PulseRadiusY, radiusMult)) + 1

	mapStartX := max(0, originX-intRadiusX)
	mapEndX := min(ctx.MapWidth-1, originX+intRadiusX)
	mapStartY := max(0, originY-intRadiusY)
	mapEndY := min(ctx.MapHeight-1, originY+intRadiusY)

	// Ripple phase offset: progress * 2 rotations (Scale = 2π)
	phaseOffset := progress << 1

	for mapY := mapStartY; mapY <= mapEndY; mapY++ {
		dy := vmath.FromInt(mapY - originY)

		for mapX := mapStartX; mapX <= mapEndX; mapX++ {
			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			dx := vmath.FromInt(mapX - originX)

			// Normalized squared distance (<=Scale means inside ellipse)
			distSq := vmath.EllipseDistSq(dx, dy, invRxSq, invRySq)
			if distSq > vmath.Scale {
				continue
			}

			// Normalized distance [0, Scale]
			dist := vmath.Sqrt(distSq)

			// Concentric ripples: sin(dist * ringCount - phaseOffset)
			angle := vmath.Mul(dist, r.ringCount) - phaseOffset
			rippleSin := vmath.Sin(angle)

			// rippleIntensity = 0.5 + 0.5 * sin = (Scale + sin) / 2
			rippleIntensity := (vmath.Scale + rippleSin) >> 1

			// Edge falloff: 1.0 - dist
			edgeFalloff := vmath.Scale - dist

			// Final alpha
			cellAlpha := vmath.Mul(vmath.Mul(baseAlpha, rippleIntensity), edgeFalloff)
			if cellAlpha < r.alphaThreshold {
				continue
			}

			buf.Set(screenX, screenY, 0, visual.RgbBlack, pulseColor, render.BlendScreen, vmath.ToFloat(cellAlpha), terminal.AttrNone)
		}
	}
}