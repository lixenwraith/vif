package renderer

import (
	"math"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
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
	missiles := r.gameCtx.World.Components.Missile
	if missiles.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)
	missiles.Each(func(e core.Entity, missile *component.MissileComponent) bool {
		kinetic, ok := r.gameCtx.World.Components.Kinetic.GetPtr(e)
		if !ok {
			return true
		}
		r.renderMissile(ctx, buf, missile, kinetic)
		return true
	})
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
	if maxAge <= 0 {
		r.renderBodyTrueColor(ctx, buf, missile, kinetic)
		return
	}

	startCol, endCol := visual.RgbMissileChildTrailStart, visual.RgbMissileChildTrailEnd
	if missile.Hostile {
		startCol, endCol = visual.RgbMissileHostileTrailStart, visual.RgbMissileHostileTrailEnd
	}

	prevX, prevY := kinetic.PreciseX, kinetic.PreciseY

	for i := range missile.TrailLen {
		idx := (missile.TrailHead - 1 - i + component.TrailCapacity) % component.TrailCapacity
		pt := &missile.Trail[idx]

		if pt.Age >= maxAge {
			break
		}

		tFactor := float64(pt.Age) / float64(maxAge)
		alpha := 1.0 - tFactor
		color := render.LerpRGB(startCol, endCol, tFactor)

		// Step-DDA iterator (thinner diagonal profile than Supercover Traverse)
		traverser := vmath.NewGridTraverserF(prevX, prevY, pt.X, pt.Y)
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
	r.renderBodyTrueColor(ctx, buf, missile, kinetic)
}

// bodyCell resolves the screen cell a missile body occupies
func (r *MissileRenderer) bodyCell(ctx render.RenderContext, kinetic *component.KineticComponent) (int, int, bool) {
	point := vmath.PointAtF(kinetic.PreciseX, kinetic.PreciseY)
	return ctx.MapToScreen(point.X, point.Y)
}

// renderBodyTrueColor writes the head's glyph only, so it reads over any field under it
func (r *MissileRenderer) renderBodyTrueColor(ctx render.RenderContext, buf *render.RenderBuffer, missile *component.MissileComponent, kinetic *component.KineticComponent) {
	if screenX, screenY, ok := r.bodyCell(ctx, kinetic); ok {
		c := visual.RgbMissileChildBody
		if missile.Hostile {
			c = visual.RgbMissileHostileBody
		}
		char := visual.MissileHeadChars[headingOctant(kinetic.VelX, kinetic.VelY)]
		buf.SetFgOnly(screenX, screenY, char, c, terminal.AttrBold)
	}
}

func (r *MissileRenderer) renderBody256(ctx render.RenderContext, buf *render.RenderBuffer, missile *component.MissileComponent, kinetic *component.KineticComponent) {
	if screenX, screenY, ok := r.bodyCell(ctx, kinetic); ok {
		c := visual.Missile256Base
		if missile.Hostile {
			c = visual.Missile256HostileBase
		}
		char := visual.MissileHeadChars256[headingOctant(kinetic.VelX, kinetic.VelY)]
		buf.SetFgOnly(screenX, screenY, char, color.RGB{R: c}, terminal.AttrFg256|terminal.AttrBold)
	}
}

// headingOctant indexes a heading glyph table: E W S N, then SE NE SW NW, then 8 at rest.
// An axis wins while the other velocity component is under half of it.
func headingOctant(velX, velY float64) int {
	absX, absY := math.Abs(velX), math.Abs(velY)
	switch {
	case absX == 0 && absY == 0:
		return 8
	case absY < absX/2 && velX > 0:
		return 0
	case absY < absX/2:
		return 1
	case absX < absY/2 && velY > 0:
		return 2
	case absX < absY/2:
		return 3
	case velX > 0 && velY > 0:
		return 4
	case velX > 0:
		return 5
	case velY > 0:
		return 6
	}
	return 7
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
	if maxAge <= 0 {
		r.renderBody256(ctx, buf, missile, kinetic)
		return
	}
	trail := visual.Missile256Trail
	if missile.Hostile {
		trail = visual.Missile256HostileTrail
	}

	for i := range missile.TrailLen {
		idx := (missile.TrailHead - missile.TrailLen + i + component.TrailCapacity) % component.TrailCapacity
		pt := &missile.Trail[idx]

		if pt.Age >= maxAge {
			continue
		}

		point := vmath.PointAtF(pt.X, pt.Y)
		mapX := point.X
		mapY := point.Y

		screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
		if !visible {
			continue
		}

		// Binary visibility for 256-color (no alpha blending)
		if pt.Age < maxAge/2 {
			buf.SetFgOnly(screenX, screenY, visual.MissileTrailChar256,
				color.RGB{R: trail}, terminal.AttrFg256)
		}
	}

	// === Body ===
	r.renderBody256(ctx, buf, missile, kinetic)
}
