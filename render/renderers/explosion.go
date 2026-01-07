package renderers

import (
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/system"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// ExplosionRenderer draws explosion field VFX using intensity accumulation
type ExplosionRenderer struct {
	gameCtx *engine.GameContext

	// Pre-allocated accumulation buffer
	accBuffer   []int64
	bufWidth    int
	bufHeight   int
	bufCapacity int
}

func NewExplosionRenderer(ctx *engine.GameContext) *ExplosionRenderer {
	r := &ExplosionRenderer{
		gameCtx: ctx,
	}

	// Pre-allocate for initial game dimensions
	r.bufWidth = ctx.GameWidth
	r.bufHeight = ctx.GameHeight
	r.bufCapacity = r.bufWidth * r.bufHeight
	if r.bufCapacity < 1 {
		r.bufCapacity = 1
	}
	r.accBuffer = make([]int64, r.bufCapacity)

	return r
}

func (r *ExplosionRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	centers := system.ExplosionCenters
	if len(centers) == 0 {
		return
	}

	// Resize check
	requiredSize := ctx.GameWidth * ctx.GameHeight
	if requiredSize > r.bufCapacity {
		r.bufCapacity = requiredSize
		r.accBuffer = make([]int64, r.bufCapacity)
	}
	r.bufWidth = ctx.GameWidth
	r.bufHeight = ctx.GameHeight

	// Clear accumulation buffer
	clear(r.accBuffer[:requiredSize])

	// Accumulation pass: rasterize all centers into buffer
	durationNano := system.ExplosionDurationNano
	if durationNano <= 0 {
		durationNano = 1 // Prevent division by zero
	}

	for i := range centers {
		c := &centers[i]
		r.accumulateCenter(c, durationNano)
	}

	// Color mapping pass
	buf.SetWriteMask(constant.MaskTransient)
	r.renderBuffer(ctx, buf, requiredSize)
}

func (r *ExplosionRenderer) accumulateCenter(c *system.ExplosionCenter, durationNano int64) {
	// Time decay via LUT: map age to 0-100 index range
	ageIndex := int(c.Age * 100 / durationNano)
	if ageIndex > 100 {
		ageIndex = 100
	}
	timeDecay := vmath.ExpDecay(ageIndex)

	// Bounding box (aspect-corrected)
	radiusCells := vmath.ToInt(c.Radius)
	radiusCellsY := radiusCells / 2

	minX := c.X - radiusCells
	maxX := c.X + radiusCells
	minY := c.Y - radiusCellsY
	maxY := c.Y + radiusCellsY

	// Clamp to buffer bounds
	if minX < 0 {
		minX = 0
	}
	if maxX >= r.bufWidth {
		maxX = r.bufWidth - 1
	}
	if minY < 0 {
		minY = 0
	}
	if maxY >= r.bufHeight {
		maxY = r.bufHeight - 1
	}

	radiusSq := vmath.Mul(c.Radius, c.Radius)
	if radiusSq == 0 {
		return
	}

	centerXFixed := vmath.FromInt(c.X)
	centerYFixed := vmath.FromInt(c.Y)

	for y := minY; y <= maxY; y++ {
		rowOffset := y * r.bufWidth
		dy := vmath.FromInt(y) - centerYFixed
		dyCirc := vmath.ScaleToCircular(dy)
		dyCircSq := vmath.Mul(dyCirc, dyCirc)

		for x := minX; x <= maxX; x++ {
			dx := vmath.FromInt(x) - centerXFixed
			distSq := vmath.Mul(dx, dx) + dyCircSq

			if distSq > radiusSq {
				continue
			}

			// Quadratic falloff: 1.0 at center, 0.0 at edge
			distFalloff := vmath.Scale - vmath.Div(distSq, radiusSq)

			// cellIntensity = baseIntensity * timeDecay * distFalloff
			cellIntensity := vmath.Mul(vmath.Mul(c.Intensity, timeDecay), distFalloff)

			r.accBuffer[rowOffset+x] += cellIntensity
		}
	}
}

func (r *ExplosionRenderer) renderBuffer(ctx render.RenderContext, buf *render.RenderBuffer, size int) {
	for idx := 0; idx < size; idx++ {
		intensity := r.accBuffer[idx]
		if intensity < constant.ExplosionEdgeThreshold {
			continue
		}

		x := idx % r.bufWidth
		y := idx / r.bufWidth

		screenX := ctx.GameX + x
		screenY := ctx.GameY + y

		var color render.RGB
		var mode render.BlendMode
		var alpha float64

		if intensity >= constant.ExplosionCoreThreshold {
			// Core: white-yellow, full additive
			color = render.RgbExplosionCore
			mode = render.BlendAdd
			alpha = 1.0
		} else if intensity >= constant.ExplosionBodyThreshold {
			// Body: orange gradient
			t := vmath.ToFloat(vmath.Div(
				intensity-constant.ExplosionBodyThreshold,
				constant.ExplosionCoreThreshold-constant.ExplosionBodyThreshold,
			))
			color = render.Lerp(render.RgbExplosionMid, render.RgbExplosionCore, t)
			mode = render.BlendAdd
			alpha = 0.8
		} else {
			// Edge: red fade
			t := vmath.ToFloat(vmath.Div(
				intensity-constant.ExplosionEdgeThreshold,
				constant.ExplosionBodyThreshold-constant.ExplosionEdgeThreshold,
			))
			color = render.Lerp(render.RgbExplosionEdge, render.RgbExplosionMid, t)
			mode = render.BlendScreen
			alpha = 0.5
		}

		buf.Set(screenX, screenY, 0, render.RGBBlack, color, mode, alpha, terminal.AttrNone)
	}
}