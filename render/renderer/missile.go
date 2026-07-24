package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// MissileRenderer draws missiles and their trails using traversal for gaps
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
	if ctx.World.Resources.Config.ColorMode == terminal.ColorMode256 {
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

func (r *MissileRenderer) renderMissileTrueColor(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	missile *component.MissileComponent,
	kinetic *component.KineticComponent,
) {
	// === Trail ===
	maxAge := parameter.MissileTrailMaxAge

	startCol, endCol := visual.RgbMissileChildTrailStart, visual.RgbMissileChildTrailEnd

	prevX, prevY := kinetic.PreciseX, kinetic.PreciseY

	for i := range missile.TrailLen {
		idx := (missile.TrailHead - 1 - i + component.TrailCapacity) % component.TrailCapacity
		pt := &missile.Trail[idx]

		if pt.Age >= maxAge {
			break
		}

		tFactor := float64(pt.Age) / float64(maxAge)
		alpha := 1.0 - tFactor
		color := render.LerpRGBFixed(startCol, endCol, vmath.FromFloat(tFactor))

		// Step-DDA iterator (thinner diagonal profile than Supercover Traverse)
		traverser := vmath.NewGridTraverser(prevX, prevY, pt.X, pt.Y)
		for traverser.Next() {
			mapX, mapY := traverser.Pos()

			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			buf.Set(screenX, screenY, visual.MissileTrailChar, color, visual.RgbBackground,
				render.BlendAddFg, alpha, terminal.AttrNone)
		}

		prevX, prevY = pt.X, pt.Y
	}

	// === Body ===
	r.renderBody(ctx, buf, missile, kinetic, true)
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

	var color color.RGB
	char := r.directionChar(kinetic.VelX, kinetic.VelY)

	if trueColor {
		buf.Set(screenX, screenY, char, color, visual.RgbBackground,
			render.BlendReplace, 1.0, terminal.AttrBold)
	} else {
		// 256-color fallback remains as is or mapped to similar indices
		buf.SetFgOnly(screenX, screenY, char, visual.RgbMissileChildBody, terminal.AttrFg256|terminal.AttrBold)
	}
}

// directionChar returns arrow character based on velocity direction
func (r *MissileRenderer) directionChar(velX, velY int64) rune {
	// 8-direction quantization
	if velX == 0 && velY == 0 {
		return visual.MissileBaseChar
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
	// === Trail ===
	maxAge := parameter.MissileTrailMaxAge

	for i := range missile.TrailLen {
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
				color.RGB{R: visual.Missile256Trail}, terminal.AttrFg256)
		}
	}

	// === Body ===
	mapX := vmath.ToInt(kinetic.PreciseX)
	mapY := vmath.ToInt(kinetic.PreciseY)

	screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
	if !visible {
		return
	}

	char := r.directionChar(kinetic.VelX, kinetic.VelY)

	buf.SetFgOnly(screenX, screenY, char, color.RGB{R: visual.Missile256Base}, terminal.AttrFg256|terminal.AttrBold)
}
