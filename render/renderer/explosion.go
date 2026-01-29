package renderer

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
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

	// Dirty rect for optimization (screen coordinates relative to GameX/Y)
	minX, maxX int
	minY, maxY int
}

func NewExplosionRenderer(ctx *engine.GameContext) *ExplosionRenderer {
	r := &ExplosionRenderer{
		gameCtx: ctx,
	}

	// Pre-allocate for initial game dimensions
	r.bufWidth = r.gameCtx.World.Resources.Config.GameWidth
	r.bufHeight = r.gameCtx.World.Resources.Config.GameHeight
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

	// If nothing was drawn to the buffer, skip rendering
	if r.maxX < r.minX || r.maxY < r.minY {
		return
	}

	// Color mapping pass
	buf.SetWriteMask(visual.MaskTransient)
	r.renderBuffer(ctx, buf)
}

func (r *ExplosionRenderer) accumulateCenter(c *system.ExplosionCenter, durationNano int64) {
	// Time decay via LUT: map age to 0-100 index range
	ageIndex := int(c.Age * 100 / durationNano)
	if ageIndex > 100 {
		ageIndex = 100
	}
	timeDecay := vmath.ExpDecay(ageIndex)

	// Bounding box (aspect-corrected)
	// Radius is horizontal cells. Vertical cells = radius / 2
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

	// Update dirty rect
	if minX < r.minX {
		r.minX = minX
	}
	if maxX > r.maxX {
		r.maxX = maxX
	}
	if minY < r.minY {
		r.minY = minY
	}
	if maxY > r.maxY {
		r.maxY = maxY
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

func (r *ExplosionRenderer) renderBuffer(ctx render.RenderContext, buf *render.RenderBuffer) {
	// Continuous Gradient Palette (Neon/Cyber)
	edgeColor := render.RgbExplosionEdge // Deep Indigo
	midColor := render.RgbExplosionMid   // Electric Cyan
	coreColor := render.RgbExplosionCore // White

	// Only iterate the dirty rectangle
	for y := r.minY; y <= r.maxY; y++ {
		rowOffset := y * r.bufWidth
		screenY := ctx.GameYOffset + y

		for x := r.minX; x <= r.maxX; x++ {
			intensity := r.accBuffer[rowOffset+x]

			// Skip near-zero values to save blend ops
			if intensity < parameter.ExplosionEdgeThreshold {
				continue
			}

			// Clamp intensity to 1.0 (Scale) for color calculations
			val := intensity
			if val > vmath.Scale {
				val = vmath.Scale
			}

			// Gradient Mapping (Fixed Point)
			// 0.0 -> Edge, Midpoint -> Mid, 1.0 -> Core
			var color render.RGB
			var tFixed int64

			if val < parameter.ExplosionGradientMidpoint {
				// Interpolate Edge -> Mid
				// t = val / Midpoint
				tFixed = vmath.Mul(val, parameter.ExplosionGradientFactor)
				color = render.LerpRGBFixed(edgeColor, midColor, tFixed)
			} else {
				// Interpolate Mid -> Core
				// t = (val - Midpoint) / (1.0 - Midpoint)
				// Assuming Midpoint is 0.5, denominator is 0.5, factor is 2.0
				base := val - parameter.ExplosionGradientMidpoint
				tFixed = vmath.Mul(base, parameter.ExplosionGradientFactor)
				color = render.LerpRGBFixed(midColor, coreColor, tFixed)
			}

			// Alpha mapping: val * AlphaMax
			alphaFixed := vmath.Mul(val, parameter.ExplosionAlphaMax)
			if alphaFixed < parameter.ExplosionAlphaMin {
				alphaFixed = parameter.ExplosionAlphaMin
			}

			screenX := ctx.GameXOffset + x

			// Convert alpha to float only at the boundary API call
			alphaFloat := vmath.ToFloat(alphaFixed)

			// Use Additive blend for neon glow effect
			buf.Set(screenX, screenY, 0, render.RGBBlack, color, render.BlendAdd, alphaFloat, terminal.AttrNone)
		}
	}
}