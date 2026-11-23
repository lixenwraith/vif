package renderer

import (
	"math"
	"unicode"

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

// OrbRenderer draws weapon orbs, and a magic square's on its outer orbit, with corona
// glow (TrueColor) or simple sigil (256)
type OrbRenderer struct {
	gameCtx   *engine.GameContext
	renderOrb orbRenderFunc
	fullChar  rune // a fully charged orb's glyph

	// Precomputed ellipse containment at the cell aspect
	effectInvRxSq    float64
	effectInvRySq    float64
	effectRadiusXInt int
	effectRadiusYInt int
}

func NewOrbRenderer(gameCtx *engine.GameContext) *OrbRenderer {
	rx := parameter.OrbCoronaRadiusX
	ry := gameCtx.World.Resources.Config.CellAspect.Y(parameter.OrbCoronaRadiusY)
	invRxSq, invRySq := vmath.EllipseInvRadiiSq(rx, ry)

	r := &OrbRenderer{
		gameCtx:          gameCtx,
		effectInvRxSq:    invRxSq,
		effectInvRySq:    invRySq,
		effectRadiusXInt: int(math.Floor(rx)),
		effectRadiusYInt: int(math.Floor(ry)),
	}
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
	if world.Components.Orb.CountEntities() == 0 && world.Components.Square.CountEntities() == 0 {
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
	world.Components.Square.Each(func(_ core.Entity, sq *component.SquareComponent) bool {
		r.squareOrbs(ctx, buf, sq)
		return true
	})
}

// squareOrbs draws each aiming kind's orb on the square's outer orbit: at the middle
// of the side its line faces, bursting as the line fires, or dim and turning round
// the orbit while no line holds the kind, the kinds half a turn apart
func (r *OrbRenderer) squareOrbs(ctx render.RenderContext, buf *render.RenderBuffer, sq *component.SquareComponent) {
	period := int64(parameter.SquareOrbIdlePeriod)
	turn := float64(ctx.GameTime.UnixNano()%period) / float64(period) * vmath.TwoPi
	aiming := 0
	for kind, spec := range component.SquareKinds {
		if left := sq.Effects[kind].Active; spec.Boost && left > 0 {
			r.squareSurge(ctx, buf, sq, kind, float64(left)/float64(spec.Duration))
		}
		if spec.Boost {
			continue
		}
		look, phase := &visual.SquareOrbs[kind], float64(aiming)*math.Pi
		aiming++
		line := sq.Line(component.SquareKind(kind))
		if line < 0 {
			r.squareOrb(ctx, buf, sq.OrbAt(turn+phase, ctx.CellAspect), look, spec.Name, visual.SquareOrbIdle, 0)
			continue
		}
		burst := 0.0
		if since := spec.Cooldown - sq.Effects[kind].Cooldown; sq.Effects[kind].Cooldown > 0 && since < parameter.OrbFlashDuration {
			burst = 1 - since.Seconds()/parameter.OrbFlashDuration.Seconds()
		}
		cells, n := sq.Orbs(line, ctx.CellAspect)
		for _, cell := range cells[:n] {
			r.squareOrb(ctx, buf, cell, look, spec.Name, 1, burst)
		}
	}
}

// squareSurge lights the shield's rim for a boosting kind, held a share of its
// boost's time: a band in the kind's glow inside the rim, heaviest in two lobes
// that turn round it, screened over the shield and fading over the boost's last
// third; a console draws the rim in dots
func (r *OrbRenderer) squareSurge(ctx render.RenderContext, buf *render.RenderBuffer, sq *component.SquareComponent, kind int, held float64) {
	cx, cy := sq.Center()
	rim := sq.ShieldRadius(ctx.CellAspect)
	startX, endX, startY, endY, ok := effectBounds(ctx, cx, cy, rim+1, ctx.CellAspect.FromCircular(rim+1))
	if !ok {
		return
	}
	period := int64(visual.SquareSurgeTurn)
	lobeX, lobeY := math.Sincos(2 * float64(ctx.GameTime.UnixNano()%period) / float64(period) * vmath.TwoPi)
	colors, console := &visual.SquareKinds[kind], r.gameCtx.World.Resources.Config.ColorMode == terminal.ColorMode256
	for y := startY; y <= endY; y++ {
		dy := ctx.CellAspect.Circular(float64(y - cy))
		for x := startX; x <= endX; x++ {
			// Columns inside the rim; past it the band ends within one
			dx := float64(x - cx)
			distSq := dx*dx + dy*dy
			depth := rim - math.Sqrt(distSq)
			shape := 1 - depth/visual.SquareSurgeWidth
			if depth < 0 {
				shape = 1 + depth
			}
			sx, sy, visible := ctx.MapToScreen(x, y)
			if shape <= 0 || distSq == 0 || !visible {
				continue
			}
			if console {
				if depth >= 0 && depth < 1 {
					buf.SetFgOnly(sx, sy, '·', color.RGB{R: colors.Console}, terminal.AttrFg256)
				}
				continue
			}
			// The cosine of twice the cell's angle from the lobes', which puts two on the rim
			lobe := ((dx*dx-dy*dy)*lobeY + 2*dx*dy*lobeX) / distSq
			buf.SetBgScreen(sx, sy, colors.Glow, visual.RgbBackground, visual.SquareSurgeAlpha*min(3*held, 1)*shape*(0.6+0.4*lobe))
		}
	}
}

// squareOrb draws one orb of a square's at a cell: its glyph in its core color under
// a corona of its other two, at a share of full light, or bursting as its line fires;
// a console draws its kind's initial in the kind's color
func (r *OrbRenderer) squareOrb(ctx render.RenderContext, buf *render.RenderBuffer, at vmath.Point, look *visual.SquareOrb, name string, light, burst float64) {
	if !ctx.IsInViewport(at.X, at.Y) {
		return
	}
	screenX, screenY, _ := ctx.MapToScreen(at.X, at.Y)
	if r.gameCtx.World.Resources.Config.ColorMode == terminal.ColorMode256 {
		attr := terminal.AttrFg256
		if light >= 1 {
			attr |= terminal.AttrBold
		}
		buf.SetFgOnly(screenX, screenY, unicode.ToUpper(rune(name[0])), color.RGB{R: look.Console}, attr)
		return
	}
	if burst > 0 {
		r.renderBurst(ctx, buf, at.X, at.Y, look.Lobe, visual.CircleBlock, burst)
		return
	}
	angle := float64(ctx.GameTime.UnixMilli()%parameter.OrbCoronaPeriodMs) / float64(parameter.OrbCoronaPeriodMs) * vmath.TwoPi
	r.renderCorona(ctx, buf, at.X, at.Y, look.Lobe, look.Counter, vmath.Cos(angle), vmath.Sin(angle), light)
	buf.SetFgOnly(screenX, screenY, visual.CircleBlock, color.Lerp(visual.RgbBackground, look.Core, light), terminal.AttrNone)
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
func (r *OrbRenderer) renderCorona(ctx render.RenderContext, buf *render.RenderBuffer, centerX, centerY int, lobe, counter color.RGB, rotDirX, rotDirY, light float64) {
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
			distSq := vmath.EllipseDistSq(dxF, dyF, r.effectInvRxSq, r.effectInvRySq)
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
			distSq := vmath.EllipseDistSq(dxF, dyF, r.effectInvRxSq, r.effectInvRySq)

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
