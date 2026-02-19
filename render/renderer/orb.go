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

// OrbRenderer draws weapon orbs with corona glow (TrueColor) or simple sigil (256)
type OrbRenderer struct {
	gameCtx   *engine.GameContext
	colorMode terminal.ColorMode

	// Precomputed ellipse containment (2:1 aspect)
	effectRadiusX    int64
	effectRadiusY    int64
	effectInvRxSq    int64
	effectInvRySq    int64
	effectRadiusXInt int
	effectRadiusYInt int
}

func NewOrbRenderer(gameCtx *engine.GameContext) *OrbRenderer {
	rx := vmath.FromFloat(parameter.OrbCoronaRadiusXFloat)
	ry := vmath.FromFloat(parameter.OrbCoronaRadiusYFloat)
	invRxSq, invRySq := vmath.EllipseInvRadiiSq(rx, ry)

	return &OrbRenderer{
		gameCtx:          gameCtx,
		colorMode:        gameCtx.World.Resources.Config.ColorMode,
		effectRadiusX:    rx,
		effectRadiusY:    ry,
		effectInvRxSq:    invRxSq,
		effectInvRySq:    invRySq,
		effectRadiusXInt: vmath.ToInt(rx),
		effectRadiusYInt: vmath.ToInt(ry),
	}
}

// Render draws all weapon orbs
func (r *OrbRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	entities := r.gameCtx.World.Components.Orb.GetAllEntities()
	if len(entities) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	// TrueColor path: precompute corona rotation once per frame
	if r.colorMode == terminal.ColorModeTrueColor {
		gameTimeMs := r.gameCtx.World.Resources.Time.GameTime.UnixMilli()
		angleFixed := ((gameTimeMs % parameter.OrbCoronaPeriodMs) * vmath.Scale) / parameter.OrbCoronaPeriodMs
		coronaRotDirX := vmath.Cos(angleFixed)
		coronaRotDirY := vmath.Sin(angleFixed)

		for _, entity := range entities {
			orbComp, ok := r.gameCtx.World.Components.Orb.GetComponent(entity)
			if !ok {
				continue
			}

			pos, ok := r.gameCtx.World.Positions.GetPosition(entity)
			if !ok {
				continue
			}

			if !ctx.IsInViewport(pos.X, pos.Y) {
				continue
			}

			r.renderOrbTrueColor(ctx, buf, pos.X, pos.Y, &orbComp, coronaRotDirX, coronaRotDirY)
		}
		return
	}

	// 256-color path: simple sigil only
	for _, entity := range entities {
		orbComp, ok := r.gameCtx.World.Components.Orb.GetComponent(entity)
		if !ok {
			continue
		}

		pos, ok := r.gameCtx.World.Positions.GetPosition(entity)
		if !ok {
			continue
		}

		screenX, screenY, visible := ctx.MapToScreen(pos.X, pos.Y)
		if !visible {
			continue
		}

		r.renderOrb256(buf, screenX, screenY, &orbComp)
	}
}

// renderOrb256 draws simple colored character for 256-color mode
func (r *OrbRenderer) renderOrb256(buf *render.RenderBuffer, screenX, screenY int, orb *component.OrbComponent) {
	var color terminal.RGB
	if orb.FlashRemaining > 0 {
		color = visual.RgbOrbFlash
	} else {
		color = r.baseColor(orb.WeaponType)
	}
	buf.SetFgOnly(screenX, screenY, visual.CircleBullsEye, color, terminal.AttrNone)
}

// renderOrbTrueColor draws corona glow with optional flash burst
func (r *OrbRenderer) renderOrbTrueColor(ctx render.RenderContext, buf *render.RenderBuffer, mapX, mapY int, orb *component.OrbComponent, rotDirX, rotDirY int64) {
	baseColor := r.baseColor(orb.WeaponType)

	if orb.FlashRemaining > 0 {
		progress := orb.FlashRemaining.Seconds() / parameter.OrbFlashDuration.Seconds()
		r.renderBurst(ctx, buf, mapX, mapY, baseColor, progress)
		return
	}

	r.renderCorona(ctx, buf, mapX, mapY, r.coronaColor(orb.WeaponType), rotDirX, rotDirY)

	screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
	if visible {
		buf.SetFgOnly(screenX, screenY, visual.CircleBullsEye, baseColor, terminal.AttrNone)
	}
}

// renderCorona draws rotating directional glow around orb center
func (r *OrbRenderer) renderCorona(ctx render.RenderContext, buf *render.RenderBuffer, centerX, centerY int, color terminal.RGB, rotDirX, rotDirY int64) {
	for dy := -r.effectRadiusYInt; dy <= r.effectRadiusYInt; dy++ {
		for dx := -r.effectRadiusXInt; dx <= r.effectRadiusXInt; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}

			mapX := centerX + dx
			mapY := centerY + dy

			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			dxF := vmath.FromInt(dx)
			dyF := vmath.FromInt(dy)
			distSq := vmath.EllipseDistSq(dxF, dyF, r.effectInvRxSq, r.effectInvRySq)
			if distSq > vmath.Scale || distSq == 0 {
				continue
			}

			theta := vmath.Atan2(dyF, dxF)
			cellDirX := vmath.Cos(theta)
			cellDirY := vmath.Sin(theta)

			// Dual-direction glow using precomputed rotation
			dot1 := vmath.DotProduct(cellDirX, cellDirY, rotDirX, rotDirY)
			dot2 := vmath.DotProduct(cellDirX, cellDirY, -rotDirX, -rotDirY)
			dot := max(dot1, dot2)
			if dot <= 0 {
				continue
			}

			edgeFactor := vmath.Div(distSq, vmath.Scale)
			intensity := vmath.Mul(dot, edgeFactor)
			alpha := vmath.ToFloat(intensity) * parameter.OrbCoronaIntensity

			if alpha < 0.05 {
				continue
			}

			buf.Set(screenX, screenY, 0, visual.RgbBlack, color, render.BlendAdd, alpha, terminal.AttrNone)
		}
	}
}

// renderBurst draws radial flash burst when orb fires
func (r *OrbRenderer) renderBurst(ctx render.RenderContext, buf *render.RenderBuffer, centerX, centerY int, baseColor terminal.RGB, progress float64) {
	expandPhase := 1.0 - progress
	burstAlpha := progress
	burstColor := render.Lerp(baseColor, visual.RgbOrbFlash, 0.5)

	for dy := -r.effectRadiusYInt; dy <= r.effectRadiusYInt; dy++ {
		for dx := -r.effectRadiusXInt; dx <= r.effectRadiusXInt; dx++ {
			mapX := centerX + dx
			mapY := centerY + dy

			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			dxF := vmath.FromInt(dx)
			dyF := vmath.FromInt(dy)
			distSq := vmath.EllipseDistSq(dxF, dyF, r.effectInvRxSq, r.effectInvRySq)

			if distSq > vmath.Scale {
				continue
			}

			normDist := vmath.ToFloat(distSq) / vmath.ToFloat(vmath.Scale)

			ringTarget := expandPhase * 0.8
			ringDist := 1.0 - (normDist-ringTarget)*(normDist-ringTarget)*4
			if ringDist < 0 {
				ringDist = 0
			}

			centerGlow := 1.0 - normDist
			alpha := (ringDist*0.7 + centerGlow*0.3) * burstAlpha
			if alpha < 0.05 {
				continue
			}

			if dx == 0 && dy == 0 {
				buf.SetFgOnly(screenX, screenY, visual.CircleBullsEye, burstColor, terminal.AttrNone)
			}

			buf.Set(screenX, screenY, 0, visual.RgbBlack, burstColor, render.BlendAdd, alpha, terminal.AttrNone)
		}
	}
}

// baseColor returns sigil color for weapon type
func (r *OrbRenderer) baseColor(wt component.WeaponType) terminal.RGB {
	switch wt {
	case component.WeaponRod:
		return visual.RgbOrbRod
	case component.WeaponLauncher:
		return visual.RgbOrbLauncher
	case component.WeaponDisruptor:
		return visual.RgbOrbDisruptor
	default:
		return visual.RgbOrbFlash
	}
}

// coronaColor returns glow color for weapon type
func (r *OrbRenderer) coronaColor(wt component.WeaponType) terminal.RGB {
	switch wt {
	case component.WeaponRod:
		return visual.RgbOrbCoronaRod
	case component.WeaponLauncher:
		return visual.RgbOrbCoronaLauncher
	case component.WeaponDisruptor:
		return visual.RgbOrbCoronaDisruptor
	default:
		return visual.RgbOrbFlash
	}
}