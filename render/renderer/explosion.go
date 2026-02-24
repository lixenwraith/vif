package renderer

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// ExplosionRenderer draws explosion field VFX using intensity accumulation
type ExplosionRenderer struct {
	gameCtx *engine.GameContext

	// Per-type accumulation buffers
	accBufferDust    []int64
	accBufferMissile []int64
	accBufferEye     []int64
	bufWidth         int
	bufHeight        int
	bufCapacity      int

	// Dirty rects per type (screen coordinates relative to GameX/Y)
	dustMinX, dustMaxX, dustMinY, dustMaxY             int
	missileMinX, missileMaxX, missileMinY, missileMaxY int
	eyeMinX, eyeMaxX, eyeMinY, eyeMaxY                 int
}

// explosionPalette holds gradient colors for an explosion type
type explosionPalette struct {
	Edge, Mid, Core terminal.RGB
}

// Block element characters ordered by visual density for intensity-based selection
var eyeBlockChars = [4]rune{'░', '▒', '▓', '█'}

// Palette lookup indexed by ExplosionType
var explosionPalettes = [3]explosionPalette{
	// ExplosionTypeDust (cyan/neon theme)
	{visual.RgbExplosionEdge, visual.RgbExplosionMid, visual.RgbExplosionCore},
	// ExplosionTypeMissile (warm theme)
	{visual.RgbMissileExplosionEdge, visual.RgbMissileExplosionMid, visual.RgbMissileExplosionCore},
	// ExplosionTypeEye (void purple theme)
	{visual.RgbEyeExplosionEdge, visual.RgbEyeExplosionMid, visual.RgbEyeExplosionCore},
}

func NewExplosionRenderer(ctx *engine.GameContext) *ExplosionRenderer {
	r := &ExplosionRenderer{
		gameCtx: ctx,
	}

	r.bufWidth = r.gameCtx.World.Resources.Config.MapWidth
	r.bufHeight = r.gameCtx.World.Resources.Config.MapHeight
	r.bufCapacity = r.bufWidth * r.bufHeight
	if r.bufCapacity < 1 {
		r.bufCapacity = 1
	}
	r.accBufferDust = make([]int64, r.bufCapacity)
	r.accBufferMissile = make([]int64, r.bufCapacity)
	r.accBufferEye = make([]int64, r.bufCapacity)

	return r
}

func (r *ExplosionRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	transRes := r.gameCtx.World.Resources.Transient
	centers := transRes.ExplosionCenters()
	if len(centers) == 0 {
		return
	}

	// Resize check
	requiredSize := ctx.ViewportWidth * ctx.ViewportHeight
	if requiredSize > r.bufCapacity {
		r.bufCapacity = requiredSize
		r.accBufferDust = make([]int64, r.bufCapacity)
		r.accBufferMissile = make([]int64, r.bufCapacity)
		r.accBufferEye = make([]int64, r.bufCapacity)
	}
	r.bufWidth = ctx.ViewportWidth
	r.bufHeight = ctx.ViewportHeight

	// Clear buffers and reset dirty rects
	clear(r.accBufferDust[:requiredSize])
	clear(r.accBufferMissile[:requiredSize])
	clear(r.accBufferEye[:requiredSize])
	r.resetDirtyRects()

	// Accumulation pass: rasterize centers into type-specific buffers
	durationNano := transRes.ExplosionDurNano
	if durationNano <= 0 {
		durationNano = 1
	}

	for i := range centers {
		c := &centers[i]
		r.accumulateCenter(ctx, c, durationNano)
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

	if r.eyeMaxX >= r.eyeMinX && r.eyeMaxY >= r.eyeMinY {
		r.renderEyeBuffer(ctx, buf, r.accBufferEye,
			r.eyeMinX, r.eyeMaxX, r.eyeMinY, r.eyeMaxY)
	}
}

func (r *ExplosionRenderer) resetDirtyRects() {
	r.dustMinX, r.dustMinY = r.bufWidth, r.bufHeight
	r.dustMaxX, r.dustMaxY = -1, -1
	r.missileMinX, r.missileMinY = r.bufWidth, r.bufHeight
	r.missileMaxX, r.missileMaxY = -1, -1
	r.eyeMinX, r.eyeMinY = r.bufWidth, r.bufHeight
	r.eyeMaxX, r.eyeMaxY = -1, -1
}

func (r *ExplosionRenderer) accumulateCenter(ctx render.RenderContext, c *engine.ExplosionCenter, durationNano int64) {
	// Transform center from map coords to viewport coords
	centerVX, centerVY, visible := ctx.MapToViewport(c.X, c.Y)
	if !visible {
		// Center off-screen but explosion might still be visible at edges
		// Continue with clamped bounds
	}

	// Time decay via LUT
	ageIndex := int(c.Age * 100 / durationNano)
	if ageIndex > 100 {
		ageIndex = 100
	}
	timeDecay := vmath.ExpDecay(ageIndex)

	// Bounding box (aspect-corrected)
	radiusCells := vmath.ToInt(c.Radius)
	radiusCellsY := radiusCells / 2

	minX := centerVX - radiusCells
	maxX := centerVX + radiusCells
	minY := centerVY - radiusCellsY
	maxY := centerVY + radiusCellsY

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

	// Skip if entirely outside
	if minX > maxX || minY > maxY {
		return
	}

	// Select buffer and update dirty rect based on type
	var accBuffer []int64
	switch c.Type {
	case event.ExplosionTypeMissile:
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
	case event.ExplosionTypeEye:
		accBuffer = r.accBufferEye
		if minX < r.eyeMinX {
			r.eyeMinX = minX
		}
		if maxX > r.eyeMaxX {
			r.eyeMaxX = maxX
		}
		if minY < r.eyeMinY {
			r.eyeMinY = minY
		}
		if maxY > r.eyeMaxY {
			r.eyeMaxY = maxY
		}
	default:
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

	centerVXFixed := vmath.FromInt(centerVX)
	centerVYFixed := vmath.FromInt(centerVY)

	for vy := minY; vy <= maxY; vy++ {
		rowOffset := vy * r.bufWidth
		dy := vmath.FromInt(vy) - centerVYFixed
		dyCirc := vmath.ScaleToCircular(dy)
		dyCircSq := vmath.Mul(dyCirc, dyCirc)

		for vx := minX; vx <= maxX; vx++ {
			dx := vmath.FromInt(vx) - centerVXFixed
			distSq := vmath.Mul(dx, dx) + dyCircSq

			if distSq > radiusSq {
				continue
			}

			// Falloff calculation differs by type
			var distFalloff int64
			if c.Type == event.ExplosionTypeMissile {
				// Quadratic falloff for sharper edge (sharper edge)
				linearFalloff := vmath.Scale - vmath.Div(distSq, radiusSq)
				distFalloff = vmath.Mul(linearFalloff, linearFalloff)
			} else {
				// Linear falloff for dust (softer, more diffuse)
				distFalloff = vmath.Scale - vmath.Div(distSq, radiusSq)
			}

			cellIntensity := vmath.Mul(vmath.Mul(c.Intensity, timeDecay), distFalloff)
			accBuffer[rowOffset+vx] += cellIntensity
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

	// Missile uses Screen blend for brighter flash, dust uses Set for glow buildup
	blendMode := render.BlendAdd
	if explosionType == event.ExplosionTypeMissile {
		blendMode = render.BlendScreen
	}

	for vy := minY; vy <= maxY; vy++ {
		rowOffset := vy * r.bufWidth
		screenY := ctx.GameYOffset + vy

		for vx := minX; vx <= maxX; vx++ {
			intensity := accBuffer[rowOffset+vx]

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

			screenX := ctx.GameXOffset + vx
			alphaFloat := vmath.ToFloat(alphaFixed)

			buf.Set(screenX, screenY, 0, visual.RgbBlack, color, blendMode, alphaFloat, terminal.AttrNone)
		}
	}
}

// renderEyeBuffer renders eye explosion type with character noise overlay
// Two-layer approach: screen-blended bg glow + intensity-scaled fg block characters
func (r *ExplosionRenderer) renderEyeBuffer(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	accBuffer []int64,
	minX, maxX, minY, maxY int,
) {
	palette := explosionPalettes[event.ExplosionTypeEye]

	for vy := minY; vy <= maxY; vy++ {
		rowOffset := vy * r.bufWidth
		screenY := ctx.GameYOffset + vy

		for vx := minX; vx <= maxX; vx++ {
			intensity := accBuffer[rowOffset+vx]
			if intensity < parameter.ExplosionEdgeThreshold {
				continue
			}

			val := intensity
			if val > vmath.Scale {
				val = vmath.Scale
			}

			// Gradient mapping (Edge → Mid → Core)
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

			screenX := ctx.GameXOffset + vx
			alphaFloat := vmath.ToFloat(alphaFixed)

			// Background glow layer (screen blend like missile)
			buf.Set(screenX, screenY, 0, visual.RgbBlack, color, render.BlendScreen, alphaFloat, terminal.AttrNone)

			// Character noise layer
			seed := uint32(vx*7919 + vy*104729)
			hash := seed ^ uint32(val>>20)

			// Density: higher intensity = more cells receive characters [1..7] out of 8
			densitySlot := uint32(1 + val*7/(vmath.Scale+1))
			if hash%8 < densitySlot {
				// Block char band [0..3] from intensity, jittered ±1
				band := int(val * 4 / (vmath.Scale + 1))
				jitter := int(hash/8%3) - 1
				band += jitter
				if band < 0 {
					band = 0
				}
				if band > 3 {
					band = 3
				}

				// Fg brightness tracks intensity for natural fade at edges
				fgColor := render.Scale(palette.Core, alphaFloat)
				buf.SetFgOnly(screenX, screenY, eyeBlockChars[band], fgColor, terminal.AttrNone)
			}
		}
	}
}