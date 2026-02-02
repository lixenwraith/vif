package renderer

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// MissileRenderer draws cluster missiles and their trails
type MissileRenderer struct {
	gameCtx *engine.GameContext

	// Mode-specific render function
	renderMissile missileRenderFunc
}

type missileRenderFunc func(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	missile *component.MissileComponent,
	kinetic *component.KineticComponent,
)

func NewMissileRenderer(ctx *engine.GameContext) *MissileRenderer {
	r := &MissileRenderer{
		gameCtx: ctx,
	}

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

// --- TrueColor Rendering ---

func (r *MissileRenderer) renderMissileTrueColor(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	missile *component.MissileComponent,
	kinetic *component.KineticComponent,
) {
	// Render trail (oldest to newest for proper overdraw)
	r.renderTrailTrueColor(ctx, buf, missile)

	// Render body
	r.renderBodyTrueColor(ctx, buf, missile, kinetic)
}

func (r *MissileRenderer) renderTrailTrueColor(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	missile *component.MissileComponent,
) {
	maxAge := parameter.MissileTrailMaxAge

	// Iterate ring buffer from oldest to newest
	for i := 0; i < missile.TrailLen; i++ {
		idx := (missile.TrailHead - missile.TrailLen + i + component.TrailCapacity) % component.TrailCapacity
		pt := &missile.Trail[idx]

		if pt.Age >= maxAge {
			continue
		}

		screenX := ctx.GameXOffset + vmath.ToInt(pt.X)
		screenY := ctx.GameYOffset + vmath.ToInt(pt.Y)

		if screenX < ctx.GameXOffset || screenX >= ctx.GameXOffset+ctx.GameWidth ||
			screenY < ctx.GameYOffset || screenY >= ctx.GameYOffset+ctx.GameHeight {
			continue
		}

		// Alpha and color based on age (1.0 at birth, 0.0 at maxAge)
		t := vmath.FromInt(pt.Age) * vmath.Scale / vmath.FromInt(maxAge)
		alpha := 1.0 - float64(pt.Age)/float64(maxAge)

		// Lerp Gold → Red
		color := render.LerpRGBFixed(visual.RgbMissileTrailStart, visual.RgbMissileTrailEnd, t)

		buf.Set(screenX, screenY, visual.MissileTrailChar, color, visual.RgbBackground,
			render.BlendAddFg, alpha, terminal.AttrNone)
	}
}

func (r *MissileRenderer) renderBodyTrueColor(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	missile *component.MissileComponent,
	kinetic *component.KineticComponent,
) {
	screenX := ctx.GameXOffset + vmath.ToInt(kinetic.PreciseX)
	screenY := ctx.GameYOffset + vmath.ToInt(kinetic.PreciseY)

	if screenX < ctx.GameXOffset || screenX >= ctx.GameXOffset+ctx.GameWidth ||
		screenY < ctx.GameYOffset || screenY >= ctx.GameYOffset+ctx.GameHeight {
		return
	}

	var char rune
	var color terminal.RGB

	switch missile.Type {
	case component.MissileTypeClusterParent:
		char = visual.MissileParentChar
		color = visual.RgbMissileBody
	case component.MissileTypeClusterChild:
		char = r.directionChar(kinetic.VelX, kinetic.VelY)
		color = visual.RgbMissileSeeker
	}

	buf.Set(screenX, screenY, char, color, visual.RgbBackground,
		render.BlendReplace, 1.0, terminal.AttrBold)
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

		screenX := ctx.GameXOffset + vmath.ToInt(pt.X)
		screenY := ctx.GameYOffset + vmath.ToInt(pt.Y)

		if screenX < ctx.GameXOffset || screenX >= ctx.GameXOffset+ctx.GameWidth ||
			screenY < ctx.GameYOffset || screenY >= ctx.GameYOffset+ctx.GameHeight {
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
	screenX := ctx.GameXOffset + vmath.ToInt(kinetic.PreciseX)
	screenY := ctx.GameYOffset + vmath.ToInt(kinetic.PreciseY)

	if screenX < ctx.GameXOffset || screenX >= ctx.GameXOffset+ctx.GameWidth ||
		screenY < ctx.GameYOffset || screenY >= ctx.GameYOffset+ctx.GameHeight {
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