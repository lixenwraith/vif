package renderer

import (
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// MissileRenderer draws cluster missiles and their trails using traversal for gaps
type MissileRenderer struct {
	gameCtx       *engine.GameContext
	renderMissile missileRenderFunc
}

type missileRenderFunc func(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	missile *component.MissileComponent,
	kinetic *component.KineticComponent,
)

func NewMissileRenderer(ctx *engine.GameContext) *MissileRenderer {
	r := &MissileRenderer{gameCtx: ctx}
	if ctx.World.Resources.Render.ColorMode == terminal.ColorMode256 {
		r.renderMissile = r.renderMissile256
	} else {
		r.renderMissile = r.renderMissileTrueColor
	}
	return r
}

func (r *MissileRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	entities := r.gameCtx.World.Components.Missile.GetAllEntities()
	if len(entities) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)
	for _, e := range entities {
		missile, ok := r.gameCtx.World.Components.Missile.GetComponent(e)
		if !ok {
			continue
		}
		kinetic, ok := r.gameCtx.World.Components.Kinetic.GetComponent(e)
		if !ok {
			continue
		}
		r.renderMissile(ctx, buf, &missile, &kinetic)
	}
}

// --- TrueColor ---

// TODO: merge these 3 functions, too many args with pointers and may not get inlined
func (r *MissileRenderer) renderMissileTrueColor(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	missile *component.MissileComponent,
	kinetic *component.KineticComponent,
) {
	r.renderTrailTrueColor(ctx, buf, missile, kinetic) // Pass kinetic for current pos
	r.renderBody(ctx, buf, missile, kinetic, true)
}

func (r *MissileRenderer) renderTrailTrueColor(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	missile *component.MissileComponent,
	currentKinetic *component.KineticComponent,
) {
	maxAge := parameter.MissileTrailMaxAge

	// Determine palette based on missile type
	startCol, endCol := visual.RgbMissileChildTrailStart, visual.RgbMissileChildTrailEnd
	if missile.Type == component.MissileTypeClusterParent {
		startCol, endCol = visual.RgbMissileParentTrailStart, visual.RgbMissileParentTrailEnd
	}

	prevX, prevY := currentKinetic.PreciseX, currentKinetic.PreciseY

	for i := 0; i < missile.TrailLen; i++ {
		idx := (missile.TrailHead - 1 - i + component.TrailCapacity) % component.TrailCapacity
		pt := &missile.Trail[idx]

		if pt.Age >= maxAge {
			break
		}

		// Pass specific colors to drawLine
		r.drawStepLine(ctx, buf, prevX, prevY, pt.X, pt.Y, pt.Age, maxAge, startCol, endCol)

		prevX, prevY = pt.X, pt.Y
	}
}

func (r *MissileRenderer) drawStepLine(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	x1, y1, x2, y2 int64,
	age, maxAge time.Duration,
	startCol, endCol terminal.RGB,
) {
	tFactor := float64(age) / float64(maxAge)
	alpha := 1.0 - tFactor
	color := render.LerpRGBFixed(startCol, endCol, vmath.FromFloat(tFactor))

	// Step-DDA iterator (thinner diagonal profile than Supercover Traverse)
	traverser := vmath.NewGridTraverser(x1, y1, x2, y2)
	for traverser.Next() {
		mapX, mapY := traverser.Pos()

		screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
		if !visible {
			continue
		}

		buf.Set(screenX, screenY, visual.MissileTrailChar, color, visual.RgbBackground,
			render.BlendAddFg, alpha, terminal.AttrNone)
	}
}

// --- Body Rendering (Shared) ---

func (r *MissileRenderer) renderBody(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	missile *component.MissileComponent,
	kinetic *component.KineticComponent,
	trueColor bool,
) {
	mapX := vmath.ToInt(kinetic.PreciseX)
	mapY := vmath.ToInt(kinetic.PreciseY)

	screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
	if !visible {
		return
	}

	var char rune
	var color terminal.RGB

	switch missile.Type {
	case component.MissileTypeClusterParent:
		char = visual.MissileParentChar
		color = visual.RgbMissileParentBody
	case component.MissileTypeClusterChild:
		char = r.directionChar(kinetic.VelX, kinetic.VelY)
		color = visual.RgbMissileChildBody
	}

	if trueColor {
		buf.Set(screenX, screenY, char, color, visual.RgbBackground,
			render.BlendReplace, 1.0, terminal.AttrBold)
	} else {
		// 256-color fallback remains as is or mapped to similar indices
		buf.SetFgOnly(screenX, screenY, char, terminal.RGB{R: visual.Missile256Body}, terminal.AttrFg256|terminal.AttrBold)
	}
}

// directionChar returns arrow character based on velocity direction
func (r *MissileRenderer) directionChar(velX, velY int64) rune {
	// 8-direction quantization
	if velX == 0 && velY == 0 {
		return visual.MissileSeekerChar
	}

	// Normalize and quantize to octant
	absX := velX
	if absX < 0 {
		absX = -absX
	}
	absY := velY
	if absY < 0 {
		absY = -absY
	}

	// Threshold for diagonal vs cardinal (tan(22.5°) ≈ 0.414)
	threshold := absX / 2 // Approximation

	if absY < threshold {
		// Horizontal
		if velX > 0 {
			return '▸' // Right
		}
		return '◂' // Left
	}
	if absX < threshold {
		// Vertical
		if velY > 0 {
			return '▾' // Down
		}
		return '▴' // Up
	}

	// Diagonal
	if velX > 0 {
		if velY > 0 {
			return '◢' // Down-right
		}
		return '◥' // Up-right
	}
	if velY > 0 {
		return '◣' // Down-left
	}
	return '◤' // Up-left
}

// --- 256-Color Rendering ---

func (r *MissileRenderer) renderMissile256(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	missile *component.MissileComponent,
	kinetic *component.KineticComponent,
) {
	// Render trail
	r.renderTrail256(ctx, buf, missile)

	// Render body
	r.renderBody256(ctx, buf, missile, kinetic)
}

func (r *MissileRenderer) renderTrail256(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	missile *component.MissileComponent,
) {
	maxAge := parameter.MissileTrailMaxAge

	for i := 0; i < missile.TrailLen; i++ {
		idx := (missile.TrailHead - missile.TrailLen + i + component.TrailCapacity) % component.TrailCapacity
		pt := &missile.Trail[idx]

		if pt.Age >= maxAge {
			continue
		}

		mapX := vmath.ToInt(pt.X)
		mapY := vmath.ToInt(pt.Y)

		screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
		if !visible {
			continue
		}

		// Binary visibility for 256-color (no alpha blending)
		if pt.Age < maxAge/2 {
			buf.SetFgOnly(screenX, screenY, visual.MissileTrailChar,
				terminal.RGB{R: visual.Missile256Trail}, terminal.AttrFg256)
		}
	}
}

func (r *MissileRenderer) renderBody256(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	missile *component.MissileComponent,
	kinetic *component.KineticComponent,
) {
	mapX := vmath.ToInt(kinetic.PreciseX)
	mapY := vmath.ToInt(kinetic.PreciseY)

	screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
	if !visible {
		return
	}

	var char rune
	var paletteIdx uint8

	switch missile.Type {
	case component.MissileTypeClusterParent:
		char = visual.MissileParentChar
		paletteIdx = visual.Missile256Body
	case component.MissileTypeClusterChild:
		char = r.directionChar(kinetic.VelX, kinetic.VelY)
		paletteIdx = visual.Missile256Seeker
	}

	buf.SetFgOnly(screenX, screenY, char, terminal.RGB{R: paletteIdx}, terminal.AttrFg256|terminal.AttrBold)
}