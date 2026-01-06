package renderers

import (
	"time"

	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// Zone thresholds in Q32.32
var (
	explosionCoreThreshold = vmath.FromFloat(constant.ExplosionCoreFraction)
	explosionBodyThreshold = vmath.FromFloat(constant.ExplosionBodyFraction)
)

// ExplosionRenderer draws expanding circular explosion effects
type ExplosionRenderer struct {
	gameCtx *engine.GameContext
}

func NewExplosionRenderer(ctx *engine.GameContext) *ExplosionRenderer {
	return &ExplosionRenderer{
		gameCtx: ctx,
	}
}

func (r *ExplosionRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	entities := r.gameCtx.World.Component.Explosion.All()
	if len(entities) == 0 {
		return
	}

	buf.SetWriteMask(constant.MaskTransient)

	for _, entity := range entities {
		exp, ok := r.gameCtx.World.Component.Explosion.Get(entity)
		if !ok || exp.CurrentRadius <= 0 {
			continue
		}

		r.renderExplosion(ctx, buf, exp.CenterX, exp.CenterY, exp.CurrentRadius, exp.MaxRadius, exp.Age, exp.Duration)
	}
}

func (r *ExplosionRenderer) renderExplosion(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	centerX, centerY int,
	currentRadius, maxRadius int64,
	age, duration time.Duration,
) {
	// Global fade based on age (1.0 at start, 0.0 at end)
	ageProgress := float64(age) / float64(duration)
	globalAlpha := 1.0 - ageProgress

	// Intensity pulse: bright flash at start, then decay
	// Peak at 0-20% age, then linear decay
	var intensityMult float64
	if ageProgress < 0.2 {
		intensityMult = 1.0
	} else {
		intensityMult = 1.0 - (ageProgress-0.2)/0.8
	}

	// Bounding box (aspect-corrected)
	radiusCells := vmath.ToInt(currentRadius)
	radiusCellsY := radiusCells / 2

	minX := centerX - radiusCells
	maxX := centerX + radiusCells
	minY := centerY - radiusCellsY
	maxY := centerY + radiusCellsY

	// Clamp to game area
	if minX < 0 {
		minX = 0
	}
	if maxX >= ctx.GameWidth {
		maxX = ctx.GameWidth - 1
	}
	if minY < 0 {
		minY = 0
	}
	if maxY >= ctx.GameHeight {
		maxY = ctx.GameHeight - 1
	}

	radiusSq := vmath.Mul(currentRadius, currentRadius)
	if radiusSq == 0 {
		return
	}

	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			dx := vmath.FromInt(x - centerX)
			dy := vmath.FromInt(y - centerY)
			dyCirc := vmath.ScaleToCircular(dy)
			distSq := vmath.CircleDistSq(dx, dyCirc)

			if distSq > radiusSq {
				continue
			}

			// Normalized distance (0 = center, Scale = edge)
			normDistSq := vmath.Div(distSq, radiusSq)
			normDist := vmath.Sqrt(normDistSq)

			// Determine zone and calculate color
			var color render.RGB
			var mode render.BlendMode
			var zoneAlpha float64

			if normDist < explosionCoreThreshold {
				// Core zone: bright flash, additive
				color = render.RgbExplosionCore
				mode = render.BlendAdd
				zoneAlpha = 1.0
			} else if normDist < explosionBodyThreshold {
				// Body zone: orange gradient, screen blend
				t := vmath.ToFloat(vmath.Div(normDist-explosionCoreThreshold, explosionBodyThreshold-explosionCoreThreshold))
				color = render.Lerp(render.RgbExplosionCore, render.RgbExplosionMid, t)
				mode = render.BlendScreen
				zoneAlpha = 1.0 - t*0.3 // Slight fade toward edge
			} else {
				// Edge zone: red fade, screen blend
				t := vmath.ToFloat(vmath.Div(normDist-explosionBodyThreshold, vmath.Scale-explosionBodyThreshold))
				color = render.Lerp(render.RgbExplosionMid, render.RgbExplosionEdge, t)
				mode = render.BlendScreen
				zoneAlpha = 1.0 - t // Full fade at edge
			}

			// Composite alpha
			alpha := globalAlpha * zoneAlpha * intensityMult

			screenX := ctx.GameX + x
			screenY := ctx.GameY + y

			buf.Set(screenX, screenY, 0, render.RGBBlack, color, mode, alpha, terminal.AttrNone)
		}
	}
}