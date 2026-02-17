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
	coronaRadiusX, coronaRadiusY int64
	coronaInvRxSq, coronaInvRySq int64
	burstRadiusX, burstRadiusY   int64
	burstInvRxSq, burstInvRySq   int64
	coronaRadiusXInt             int
	coronaRadiusYInt             int
	burstRadiusXInt              int
	burstRadiusYInt              int
}

// NewOrbRenderer creates the orb visualization renderer
func NewOrbRenderer(gameCtx *engine.GameContext) *OrbRenderer {
	coronaRx := vmath.FromFloat(parameter.OrbCoronaRadiusXFloat)
	coronaRy := vmath.FromFloat(parameter.OrbCoronaRadiusYFloat)
	burstRx := vmath.FromFloat(parameter.OrbBurstRadiusXFloat)
	burstRy := vmath.FromFloat(parameter.OrbBurstRadiusYFloat)

	coronaInvRxSq, coronaInvRySq := vmath.EllipseInvRadiiSq(coronaRx, coronaRy)
	burstInvRxSq, burstInvRySq := vmath.EllipseInvRadiiSq(burstRx, burstRy)

	return &OrbRenderer{
		gameCtx:          gameCtx,
		colorMode:        gameCtx.World.Resources.Config.ColorMode,
		coronaRadiusX:    coronaRx,
		coronaRadiusY:    coronaRy,
		coronaInvRxSq:    coronaInvRxSq,
		coronaInvRySq:    coronaInvRySq,
		burstRadiusX:     burstRx,
		burstRadiusY:     burstRy,
		burstInvRxSq:     burstInvRxSq,
		burstInvRySq:     burstInvRySq,
		coronaRadiusXInt: vmath.ToInt(coronaRx),
		coronaRadiusYInt: vmath.ToInt(coronaRy),
		burstRadiusXInt:  vmath.ToInt(burstRx),
		burstRadiusYInt:  vmath.ToInt(burstRy),
	}
}

// Render draws all weapon orbs
func (r *OrbRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	entities := r.gameCtx.World.Components.Orb.GetAllEntities()
	if len(entities) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

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

		if r.colorMode == terminal.ColorMode256 {
			r.renderOrb256(buf, screenX, screenY, &orbComp)
		} else {
			r.renderOrbTrueColor(ctx, buf, pos.X, pos.Y, &orbComp)
		}
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

// renderCorona draws rotating directional glow around orb center
func (r *OrbRenderer) renderCorona(ctx render.RenderContext, buf *render.RenderBuffer, centerX, centerY int, color terminal.RGB) {
	// Rotation angle from game time
	gameTimeMs := r.gameCtx.World.Resources.Time.GameTime.UnixMilli()
	periodMs := parameter.OrbCoronaPeriodMs
	angleFixed := ((gameTimeMs % periodMs) * vmath.Scale) / periodMs
	rotDirX := vmath.Cos(angleFixed)
	rotDirY := vmath.Sin(angleFixed)

	// Bounding box
	for dy := -r.coronaRadiusYInt; dy <= r.coronaRadiusYInt; dy++ {
		for dx := -r.coronaRadiusXInt; dx <= r.coronaRadiusXInt; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}

			mapX := centerX + dx
			mapY := centerY + dy

			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			// Ellipse containment check
			dxF := vmath.FromInt(dx)
			dyF := vmath.FromInt(dy)
			distSq := vmath.EllipseDistSq(dxF, dyF, r.coronaInvRxSq, r.coronaInvRySq)
			if distSq > vmath.Scale || distSq == 0 {
				continue
			}

			// Normalize direction
			cellDirX, cellDirY := vmath.Normalize2D(dxF, dyF)

			// Dual-direction glow
			dot1 := vmath.DotProduct(cellDirX, cellDirY, rotDirX, rotDirY)
			dot2 := vmath.DotProduct(cellDirX, cellDirY, -rotDirX, -rotDirY)
			dot := max(dot1, dot2)
			if dot <= 0 {
				continue
			}

			// Edge factor: stronger at ellipse boundary (distSq approaching Scale)
			edgeFactor := vmath.Div(distSq, vmath.Scale)

			// Intensity = dot alignment × edge proximity
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
	// progress: 1.0 = just fired, 0.0 = flash ending
	expandPhase := 1.0 - progress
	burstAlpha := progress

	// Blend white with weapon color
	burstColor := render.Lerp(baseColor, visual.RgbOrbFlash, 0.5)

	for dy := -r.burstRadiusYInt; dy <= r.burstRadiusYInt; dy++ {
		for dx := -r.burstRadiusXInt; dx <= r.burstRadiusXInt; dx++ {
			mapX := centerX + dx
			mapY := centerY + dy

			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			dxF := vmath.FromInt(dx)
			dyF := vmath.FromInt(dy)
			distSq := vmath.EllipseDistSq(dxF, dyF, r.burstInvRxSq, r.burstInvRySq)

			if distSq > vmath.Scale {
				continue
			}

			// Normalized distance 0-1 within ellipse
			normDist := vmath.ToFloat(distSq) / vmath.ToFloat(vmath.Scale)

			// Ring effect: peak at expanding edge
			ringTarget := expandPhase * 0.8
			ringDist := 1.0 - (normDist-ringTarget)*(normDist-ringTarget)*4
			if ringDist < 0 {
				ringDist = 0
			}

			// Center glow
			centerGlow := 1.0 - normDist

			alpha := (ringDist*0.7 + centerGlow*0.3) * burstAlpha
			if alpha < 0.05 {
				continue
			}

			// Center cell: draw sigil
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

// renderOrbTrueColor draws corona glow with optional flash burst
func (r *OrbRenderer) renderOrbTrueColor(ctx render.RenderContext, buf *render.RenderBuffer, mapX, mapY int, orb *component.OrbComponent) {
	baseColor := r.baseColor(orb.WeaponType)

	// Flash state: radial burst
	if orb.FlashRemaining > 0 {
		progress := orb.FlashRemaining.Seconds() / parameter.OrbFlashDuration.Seconds()
		r.renderBurst(ctx, buf, mapX, mapY, baseColor, progress)
		return
	}

	// Idle state: rotating corona
	r.renderCorona(ctx, buf, mapX, mapY, r.coronaColor(orb.WeaponType))

	// Center sigil
	screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
	if visible {
		buf.SetFgOnly(screenX, screenY, visual.CircleBullsEye, baseColor, terminal.AttrNone)
	}
}