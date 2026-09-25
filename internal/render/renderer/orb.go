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

// OrbRenderer draws weapon orbs with corona glow (TrueColor) or simple sigil (256)
type OrbRenderer struct {
	gameCtx   *engine.GameContext
	renderOrb orbRenderFunc

	// Precomputed ellipse containment (2:1 aspect)
	effectInvRxSq    float64
	effectInvRySq    float64
	effectRadiusXInt int
	effectRadiusYInt int
}

func NewOrbRenderer(gameCtx *engine.GameContext) *OrbRenderer {
	rx := parameter.OrbCoronaRadiusX
	ry := parameter.OrbCoronaRadiusY
	invRxSq, invRySq := vmath.EllipseInvRadiiSqF(rx, ry)

	r := &OrbRenderer{
		gameCtx:          gameCtx,
		effectInvRxSq:    invRxSq,
		effectInvRySq:    invRySq,
		effectRadiusXInt: int(math.Floor(rx)),
		effectRadiusYInt: int(math.Floor(ry)),
	}
	if gameCtx.World.Resources.Config.ColorMode == terminal.ColorMode256 {
		r.renderOrb = r.renderOrb256
	} else {
		r.renderOrb = r.renderOrbTrueColor
	}
	return r
}

// orbRenderFunc draws one orb whose map cell is visible
type orbRenderFunc func(ctx render.RenderContext, buf *render.RenderBuffer, mapX, mapY int, orb *component.OrbComponent, glyph rune)

// Render draws all weapon orbs
func (r *OrbRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	orbs := r.gameCtx.World.Components.Orb
	if orbs.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	orbs.Each(func(entity core.Entity, orbComp *component.OrbComponent) bool {
		pos, ok := r.gameCtx.World.Positions.GetPosition(entity)
		if !ok || !ctx.IsInViewport(pos.X, pos.Y) {
			return true
		}
		r.renderOrb(ctx, buf, pos.X, pos.Y, orbComp, r.chargeGlyph(orbComp))
		return true
	})
}

// chargeGlyph resolves the ASCII digit for stacked sub-max charges (1-9), or the base
// bullseye at max charge (Disruptor's max is 1, so it always renders as bullseye)
func (r *OrbRenderer) chargeGlyph(orb *component.OrbComponent) rune {
	weaponComp, ok := r.gameCtx.World.Components.Weapon.GetPtr(orb.OwnerEntity)
	if !ok {
		return visual.CircleBullsEye
	}
	charges := weaponComp.Charges[orb.WeaponType]
	if charges <= 0 || charges >= parameter.WeaponMaxCharges[orb.WeaponType] {
		return visual.CircleBullsEye
	}
	return rune('0' + charges)
}

// renderOrb256 draws simple colored character for 256-color mode
func (r *OrbRenderer) renderOrb256(ctx render.RenderContext, buf *render.RenderBuffer, mapX, mapY int, orb *component.OrbComponent, glyph rune) {
	screenX, screenY, _ := ctx.MapToScreen(mapX, mapY)
	var c color.RGB
	if orb.FlashRemaining > 0 {
		c = visual.RgbOrbFlash
	} else {
		c = r.baseColor(orb.WeaponType)
	}
	buf.SetFgOnly(screenX, screenY, glyph, c, terminal.AttrNone)
}

// renderOrbTrueColor draws corona glow with optional flash burst
func (r *OrbRenderer) renderOrbTrueColor(ctx render.RenderContext, buf *render.RenderBuffer, mapX, mapY int, orb *component.OrbComponent, glyph rune) {
	baseColor := r.baseColor(orb.WeaponType)

	if orb.FlashRemaining > 0 {
		progress := orb.FlashRemaining.Seconds() / parameter.OrbFlashDuration.Seconds()
		r.renderBurst(ctx, buf, mapX, mapY, baseColor, glyph, progress)
		return
	}

	angle := 0.0
	if parameter.OrbCoronaPeriodMs > 0 {
		gameTimeMs := r.gameCtx.World.Resources.Time.GameTime.UnixMilli()
		angle = float64(gameTimeMs%parameter.OrbCoronaPeriodMs) / float64(parameter.OrbCoronaPeriodMs) * vmath.TwoPi
	}
	r.renderCorona(ctx, buf, mapX, mapY, r.coronaColor(orb.WeaponType), vmath.CosF(angle), vmath.SinF(angle))

	screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
	if visible {
		buf.SetFgOnly(screenX, screenY, glyph, baseColor, terminal.AttrNone)
	}
}

// renderCorona draws rotating directional glow around orb center
func (r *OrbRenderer) renderCorona(ctx render.RenderContext, buf *render.RenderBuffer, centerX, centerY int, c color.RGB, rotDirX, rotDirY float64) {
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

			dxF := float64(dx)
			dyF := float64(dy)
			distSq := vmath.EllipseDistSqF(dxF, dyF, r.effectInvRxSq, r.effectInvRySq)
			if distSq > 1.0 || distSq == 0.0 {
				continue
			}

			// Dual-direction glow using vector normalization
			cellDirX, cellDirY := vmath.Normalize2DF(dxF, dyF)
			dot1 := vmath.DotProductF(cellDirX, cellDirY, rotDirX, rotDirY)
			dot2 := vmath.DotProductF(cellDirX, cellDirY, -rotDirX, -rotDirY)
			dot := max(dot1, dot2)
			if dot <= 0.0 {
				continue
			}

			edgeFactor := distSq
			intensity := dot * edgeFactor
			alpha := intensity * parameter.OrbCoronaIntensity

			if alpha < 0.05 {
				continue
			}

			buf.Set(screenX, screenY, 0, visual.RgbBlack, c, render.BlendAdd, alpha, terminal.AttrNone)
		}
	}
}

// renderBurst draws radial flash burst when orb fires
func (r *OrbRenderer) renderBurst(ctx render.RenderContext, buf *render.RenderBuffer, centerX, centerY int, baseColor color.RGB, glyph rune, progress float64) {
	expandPhase := 1.0 - progress
	burstAlpha := progress
	burstColor := color.Lerp(baseColor, visual.RgbOrbFlash, 0.5)

	for dy := -r.effectRadiusYInt; dy <= r.effectRadiusYInt; dy++ {
		for dx := -r.effectRadiusXInt; dx <= r.effectRadiusXInt; dx++ {
			mapX := centerX + dx
			mapY := centerY + dy

			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			dxF := float64(dx)
			dyF := float64(dy)
			distSq := vmath.EllipseDistSqF(dxF, dyF, r.effectInvRxSq, r.effectInvRySq)

			if distSq > 1.0 {
				continue
			}

			normDist := distSq

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
				buf.SetFgOnly(screenX, screenY, glyph, burstColor, terminal.AttrNone)
			}

			buf.Set(screenX, screenY, 0, visual.RgbBlack, burstColor, render.BlendAdd, alpha, terminal.AttrNone)
		}
	}
}

// baseColor returns sigil color for weapon type
func (r *OrbRenderer) baseColor(wt component.WeaponType) color.RGB {
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
func (r *OrbRenderer) coronaColor(wt component.WeaponType) color.RGB {
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
