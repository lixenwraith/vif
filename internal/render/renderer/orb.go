package renderer

import (
	"math"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// OrbRenderer draws weapon orbs with corona glow (TrueColor) or simple sigil (256)
type OrbRenderer struct {
	gameCtx   *engine.GameContext
	renderOrb orbRenderFunc
	fullChar  rune // a fully charged orb's glyph
	orbGlow
}

// orbGlow lights an orb's cells at the cell aspect: the corona turning round it and
// the burst as it fires
type orbGlow struct {
	invRxSq, invRySq float64
	radiusX, radiusY int
}

func newOrbGlow(aspect vmath.Aspect) orbGlow {
	rx, ry := parameter.OrbCoronaRadiusX, aspect.Y(parameter.OrbCoronaRadiusY)
	invRxSq, invRySq := vmath.EllipseInvRadiiSq(rx, ry)
	return orbGlow{invRxSq: invRxSq, invRySq: invRySq, radiusX: int(math.Floor(rx)), radiusY: int(math.Floor(ry))}
}

func NewOrbRenderer(gameCtx *engine.GameContext) *OrbRenderer {
	r := &OrbRenderer{gameCtx: gameCtx, orbGlow: newOrbGlow(gameCtx.World.Resources.Config.CellAspect)}
	if gameCtx.World.Resources.Config.ColorMode == terminal.ColorMode256 {
		r.renderOrb, r.fullChar = r.renderOrb256, visual.OrbFullChar256
	} else {
		r.renderOrb, r.fullChar = r.renderOrbTrueColor, visual.CircleBullsEye
	}
	return r
}

// orbRenderFunc draws one orb whose map cell is visible
type orbRenderFunc func(ctx render.RenderContext, buf *render.RenderBuffer, mapX, mapY int, orb *component.OrbComponent, glyph rune)

// Render draws every orb
func (r *OrbRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	world := r.gameCtx.World
	if world.Components.Orb.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	world.Components.Orb.Each(func(entity core.Entity, orbComp *component.OrbComponent) bool {
		pos, ok := world.Positions.GetPosition(entity)
		if !ok || !ctx.IsInViewport(pos.X, pos.Y) {
			return true
		}
		r.renderOrb(ctx, buf, pos.X, pos.Y, orbComp, r.chargeGlyph(orbComp))
		return true
	})
}

// chargeGlyph is the digit of a charge count below the cap, or the bullseye at it
func (r *OrbRenderer) chargeGlyph(orb *component.OrbComponent) rune {
	weaponComp, ok := r.gameCtx.World.Components.Weapon.GetPtr(orb.OwnerEntity)
	if !ok {
		return r.fullChar
	}
	charges := weaponComp.Charges[orb.WeaponType]
	if charges <= 0 || charges >= component.WeaponSpecs[orb.WeaponType].MaxCharges {
		return r.fullChar
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
		c = orbPalette[orb.WeaponType].base
	}
	buf.SetFgOnly(screenX, screenY, glyph, c, terminal.AttrNone)
}

// renderOrbTrueColor draws corona glow with optional flash burst
func (r *OrbRenderer) renderOrbTrueColor(ctx render.RenderContext, buf *render.RenderBuffer, mapX, mapY int, orb *component.OrbComponent, glyph rune) {
	baseColor := orbPalette[orb.WeaponType].base

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
	corona := orbPalette[orb.WeaponType].corona
	r.renderCorona(ctx, buf, mapX, mapY, corona, corona, vmath.Cos(angle), vmath.Sin(angle), 1)

	screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
	if visible {
		buf.SetFgOnly(screenX, screenY, glyph, baseColor, terminal.AttrNone)
	}
}

// renderCorona draws the glow turning round an orb's center: a lobe along the turn's
// direction and its counter opposite, each in its color, at a share of full light
func (r *orbGlow) renderCorona(ctx render.RenderContext, buf *render.RenderBuffer, centerX, centerY int, lobe, counter color.RGB, rotDirX, rotDirY, light float64) {
	for dy := -r.radiusY; dy <= r.radiusY; dy++ {
		for dx := -r.radiusX; dx <= r.radiusX; dx++ {
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
			distSq := vmath.EllipseDistSq(dxF, dyF, r.invRxSq, r.invRySq)
			if distSq > 1.0 || distSq == 0.0 {
				continue
			}

			// A lobe each way along the turn, the cell lit by the one it faces
			cellDirX, cellDirY := vmath.Normalize2D(dxF, dyF)
			dot, c := vmath.DotProduct(cellDirX, cellDirY, rotDirX, rotDirY), lobe
			if dot < 0 {
				dot, c = -dot, counter
			}

			edgeFactor := distSq
			intensity := dot * edgeFactor
			alpha := intensity * parameter.OrbCoronaIntensity * light

			if alpha < 0.05 {
				continue
			}

			buf.Set(screenX, screenY, 0, visual.RgbBlack, c, render.BlendAdd, alpha, terminal.AttrNone)
		}
	}
}

// renderBurst draws radial flash burst when orb fires
func (r *orbGlow) renderBurst(ctx render.RenderContext, buf *render.RenderBuffer, centerX, centerY int, baseColor color.RGB, glyph rune, progress float64) {
	expandPhase := 1.0 - progress
	burstAlpha := progress
	burstColor := color.Lerp(baseColor, visual.RgbOrbFlash, 0.5)

	for dy := -r.radiusY; dy <= r.radiusY; dy++ {
		for dx := -r.radiusX; dx <= r.radiusX; dx++ {
			mapX := centerX + dx
			mapY := centerY + dy

			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			dxF := float64(dx)
			dyF := float64(dy)
			distSq := vmath.EllipseDistSq(dxF, dyF, r.invRxSq, r.invRySq)

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

// orbPalette is each weapon kind's sigil and corona color
var orbPalette = [component.WeaponCount]struct{ base, corona color.RGB }{
	component.WeaponRod:       {visual.RgbOrbRod, visual.RgbOrbCoronaRod},
	component.WeaponLauncher:  {visual.RgbOrbLauncher, visual.RgbOrbCoronaLauncher},
	component.WeaponDisruptor: {visual.RgbOrbDisruptor, visual.RgbOrbCoronaDisruptor},
	component.WeaponTurret:    {visual.RgbOrbTurret, visual.RgbOrbCoronaTurret},
	component.WeaponEmitter:   {visual.RgbOrbEmitter, visual.RgbOrbCoronaEmitter},
}
