package renderer

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
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

	// Per-type accumulation buffers
	accBufferDust    []int64
	accBufferMissile []int64
	bufWidth         int
	bufHeight        int
	bufCapacity      int

	// Dirty rects per type (screen coordinates relative to GameX/Y)
	dustMinX, dustMaxX, dustMinY, dustMaxY             int
	missileMinX, missileMaxX, missileMinY, missileMaxY int
}

// explosionPalette holds gradient colors for an explosion type
type explosionPalette struct {
	Edge, Mid, Core terminal.RGB
}

// Palette lookup indexed by ExplosionType
var explosionPalettes = [2]explosionPalette{
	// ExplosionTypeDust (cyan/neon theme)
	{visual.RgbExplosionEdge, visual.RgbExplosionMid, visual.RgbExplosionCore},
	// ExplosionTypeMissile (warm theme)
	{visual.RgbMissileExplosionEdge, visual.RgbMissileExplosionMid, visual.RgbMissileExplosionCore},
}

func NewExplosionRenderer(ctx *engine.GameContext) *ExplosionRenderer {
	r := &ExplosionRenderer{
		gameCtx: ctx,
	}

	r.bufWidth = r.gameCtx.World.Resources.Config.GameWidth
	r.bufHeight = r.gameCtx.World.Resources.Config.GameHeight
	r.bufCapacity = r.bufWidth * r.bufHeight
	if r.bufCapacity < 1 {
		r.bufCapacity = 1
	}
	r.accBufferDust = make([]int64, r.bufCapacity)
	r.accBufferMissile = make([]int64, r.bufCapacity)

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
		r.accBufferDust = make([]int64, r.bufCapacity)
		r.accBufferMissile = make([]int64, r.bufCapacity)
	}
	r.bufWidth = ctx.GameWidth
	r.bufHeight = ctx.GameHeight

	// Clear buffers and reset dirty rects
	clear(r.accBufferDust[:requiredSize])
	clear(r.accBufferMissile[:requiredSize])
	r.resetDirtyRects()

	// Accumulation pass: rasterize centers into type-specific buffers
	durationNano := system.ExplosionDurationNano
	if durationNano <= 0 {
		durationNano = 1
	}

	for i := range centers {
		c := &centers[i]
		r.accumulateCenter(c, durationNano)
	}

	// Render both types
	buf.SetWriteMask(visual.MaskTransient)

	if r.dustMaxX >= r.dustMinX && r.dustMaxY >= r.dustMinY {
		r.renderTypeBuffer(ctx, buf, r.accBufferDust, event.ExplosionTypeDust,
			r.dustMinX, r.dustMaxX, r.dustMinY, r.dustMaxY)
	}

	if r.missileMaxX >= r.missileMinX && r.missileMaxY >= r.missileMinY {
		r.renderTypeBuffer(ctx, buf, r.accBufferMissile, event.ExplosionTypeMissile,
			r.missileMinX, r.missileMaxX, r.missileMinY, r.missileMaxY)
	}
}

func (r *ExplosionRenderer) resetDirtyRects() {
	r.dustMinX, r.dustMinY = r.bufWidth, r.bufHeight
	r.dustMaxX, r.dustMaxY = -1, -1
	r.missileMinX, r.missileMinY = r.bufWidth, r.bufHeight
	r.missileMaxX, r.missileMaxY = -1, -1
}

func (r *ExplosionRenderer) accumulateCenter(c *system.ExplosionCenter, durationNano int64) {
	// Time decay via LUT
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

	// Select buffer and update dirty rect based on type
	var accBuffer []int64
	if c.Type == event.ExplosionTypeMissile {
		accBuffer = r.accBufferMissile
		if minX < r.missileMinX {
			r.missileMinX = minX
		}
		if maxX > r.missileMaxX {
			r.missileMaxX = maxX
		}
		if minY < r.missileMinY {
			r.missileMinY = minY
		}
		if maxY > r.missileMaxY {
			r.missileMaxY = maxY
		}
	} else {
		accBuffer = r.accBufferDust
		if minX < r.dustMinX {
			r.dustMinX = minX
		}
		if maxX > r.dustMaxX {
			r.dustMaxX = maxX
		}
		if minY < r.dustMinY {
			r.dustMinY = minY
		}
		if maxY > r.dustMaxY {
			r.dustMaxY = maxY
		}
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

			// Falloff calculation differs by type
			var distFalloff int64
			if c.Type == event.ExplosionTypeMissile {
				// Quadratic falloff for sharper edge
				linearFalloff := vmath.Scale - vmath.Div(distSq, radiusSq)
				distFalloff = vmath.Mul(linearFalloff, linearFalloff)
			} else {
				// Linear falloff for dust (softer, more diffuse)
				distFalloff = vmath.Scale - vmath.Div(distSq, radiusSq)
			}

			cellIntensity := vmath.Mul(vmath.Mul(c.Intensity, timeDecay), distFalloff)
			accBuffer[rowOffset+x] += cellIntensity
		}
	}
}

func (r *ExplosionRenderer) renderTypeBuffer(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	accBuffer []int64,
	explosionType event.ExplosionType,
	minX, maxX, minY, maxY int,
) {
	palette := explosionPalettes[explosionType]

	// Missile uses Screen blend for brighter flash, dust uses AddEntityAt for glow buildup
	blendMode := render.BlendAdd
	if explosionType == event.ExplosionTypeMissile {
		blendMode = render.BlendScreen
	}

	for y := minY; y <= maxY; y++ {
		rowOffset := y * r.bufWidth
		screenY := ctx.GameYOffset + y

		for x := minX; x <= maxX; x++ {
			intensity := accBuffer[rowOffset+x]

			if intensity < parameter.ExplosionEdgeThreshold {
				continue
			}

			val := intensity
			if val > vmath.Scale {
				val = vmath.Scale
			}

			// Gradient mapping
			var color terminal.RGB
			var tFixed int64

			if val < parameter.ExplosionGradientMidpoint {
				tFixed = vmath.Mul(val, parameter.ExplosionGradientFactor)
				color = render.LerpRGBFixed(palette.Edge, palette.Mid, tFixed)
			} else {
				base := val - parameter.ExplosionGradientMidpoint
				tFixed = vmath.Mul(base, parameter.ExplosionGradientFactor)
				color = render.LerpRGBFixed(palette.Mid, palette.Core, tFixed)
			}

			// Alpha mapping
			alphaFixed := vmath.Mul(val, parameter.ExplosionAlphaMax)
			if alphaFixed < parameter.ExplosionAlphaMin {
				alphaFixed = parameter.ExplosionAlphaMin
			}

			screenX := ctx.GameXOffset + x
			alphaFloat := vmath.ToFloat(alphaFixed)

			buf.Set(screenX, screenY, 0, visual.RgbBlack, color, blendMode, alphaFloat, terminal.AttrNone)
		}
	}
}