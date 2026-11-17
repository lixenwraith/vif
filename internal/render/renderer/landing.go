package renderer

import (
	"math"
	"time"

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

// LandingRenderer blinks the centre cell of the footprint an attack lands on: the
// swarm's from its lock through its charge or teleport, a data species' while its
// state is flagged Landing. A Kraken's charge marks where its body ends, far larger.
// The lines show the path; this shows where to keep clear.
type LandingRenderer struct {
	gameCtx    *engine.GameContext
	renderCell landingCellRenderer
	palette    bool
	style      int // the look a Kraken's charge marks in, of krakenLandingLooks
}

// landingCellRenderer draws the marker at step of its colour cycle
type landingCellRenderer func(buf *render.RenderBuffer, screenX, screenY, step int)

func NewLandingRenderer(ctx *engine.GameContext) *LandingRenderer {
	r := &LandingRenderer{gameCtx: ctx, style: visual.KrakenLandingStyle}
	if ctx.World.Resources.Config.ColorMode == terminal.ColorMode256 {
		r.renderCell, r.palette = r.cell256, true
	} else {
		r.renderCell = r.cellTrueColor
	}
	return r
}

func (r *LandingRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	world := r.gameCtx.World
	swarms, behaviors, krakens := world.Components.Swarm, world.Components.Behavior, world.Components.Kraken
	if swarms.CountEntities() == 0 && behaviors.CountEntities() == 0 && krakens.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	krakens.Each(func(_ core.Entity, k *component.KrakenComponent) bool {
		if k.State == component.KrakenSpin || k.State == component.KrakenMove {
			r.markCharge(ctx, buf, k)
		}
		return true
	})

	swarms.Each(func(_ core.Entity, sw *component.SwarmComponent) bool {
		var x, y int
		switch sw.Machine.State {
		case component.SwarmStateLock, component.SwarmStateCharge:
			x, y = sw.LockedTargetX, sw.LockedTargetY
		case component.SwarmStateTeleport:
			x, y = sw.TeleportTargetX, sw.TeleportTargetY
		default:
			return true
		}
		r.mark(ctx, buf, component.Footprints[component.SpeciesSwarm].At(x, y), sw.Machine.Elapsed)
		return true
	})

	prototypes := world.Resources.Behaviors
	behaviors.Each(func(_ core.Entity, b *component.BehaviorComponent) bool {
		if !b.Landing || int(b.Program) >= len(prototypes) {
			return true
		}
		p := &prototypes[b.Program]
		r.mark(ctx, buf, p.Footprint.At(b.TargetX, b.TargetY), b.Machine.Elapsed)
		return true
	})
}

// mark draws the marker on the centre of the area the body covers, its cycle starting
// on each state's entry
func (r *LandingRenderer) mark(ctx render.RenderContext, buf *render.RenderBuffer, footprint vmath.Area, elapsed time.Duration) {
	c := footprint.Center()
	if screenX, screenY, visible := ctx.MapToScreen(c.X, c.Y); visible {
		r.renderCell(buf, screenX, screenY, int(elapsed/visual.LandingBlinkStep))
	}
}

// krakenLandingLooks is each look's color and share of KrakenLandingAlpha for a
// cell d of the mark's radius from the charge's end, soon of the way through the
// spin and at step of the blink
var krakenLandingLooks = [...]func(d, soon float64, step int) (color.RGB, float64){
	visual.KrakenLandingGlow: func(d, soon float64, _ int) (color.RGB, float64) {
		return visual.RgbKrakenAttack, (0.3 + 0.4*soon) * d * d * landingEdge((1-d)/visual.KrakenLandingFeather)
	},
	visual.KrakenLandingBlink: func(d, _ float64, step int) (color.RGB, float64) {
		return visual.RgbLandingBlink[step%len(visual.RgbLandingBlink)], 0.4 * landingEdge((1-d)/visual.KrakenLandingFeather)
	},
	visual.KrakenLandingRing: func(d, _ float64, _ int) (color.RGB, float64) {
		return visual.RgbKrakenAttack, landingRing(d, 1)
	},
	visual.KrakenLandingClose: func(d, soon float64, _ int) (color.RGB, float64) {
		return visual.RgbKrakenAttack, max(landingRing(d, 2-soon), 0.3*soon*landingEdge((1-d)/visual.KrakenLandingFeather))
	},
}

// landingEdge eases t from nothing at 0 to all at 1
func landingEdge(t float64) float64 {
	t = min(max(t, 0), 1)
	return t * t * (3 - 2*t)
}

// landingRing is a band KrakenLandingBand wide whose outer edge is radius
func landingRing(d, radius float64) float64 {
	return landingEdge(1 - math.Abs(d-radius+visual.KrakenLandingBand/2)/(visual.KrakenLandingBand/2))
}

// markCharge marks where a Kraken's charge ends, KrakenLandingReach body radii
// round, from its spin's first tick until the body arrives
func (r *LandingRenderer) markCharge(ctx render.RenderContext, buf *render.RenderBuffer, k *component.KrakenComponent) {
	soon := 1.0
	if k.State == component.KrakenSpin {
		soon = min(max(1-k.StateRemaining.Seconds()/parameter.KrakenSpinDuration.Seconds(), 0), 1)
	}
	look := krakenLandingLooks[r.style]
	step := int(time.Duration(k.Time*float64(time.Second)) / visual.LandingBlinkStep)
	x, y := k.TargetX, k.TargetY
	ry := parameter.KrakenBodyRadius * visual.KrakenLandingReach
	rx := ry * parameter.KrakenRowSquash
	// A closing ring starts at twice the mark's radius
	for cy := int(math.Ceil(y - 2*ry - vmath.CellCenter)); cy <= int(y+2*ry-vmath.CellCenter); cy++ {
		for cx := int(math.Ceil(x - 2*rx - vmath.CellCenter)); cx <= int(x+2*rx-vmath.CellCenter); cx++ {
			d := math.Hypot((float64(cx)+vmath.CellCenter-x)/rx, (float64(cy)+vmath.CellCenter-y)/ry)
			c, alpha := look(d, soon, step)
			// A console cannot blend a filled mark, so it draws the outline, as a shield's
			if r.palette && r.style == visual.KrakenLandingClose {
				alpha = landingRing(d, 2-soon)
			} else if r.palette {
				alpha = landingRing(d, 1)
			}
			sx, sy, visible := ctx.MapToScreen(cx, cy)
			if !visible || alpha <= 0 {
				continue
			}
			if !r.palette {
				buf.Set(sx, sy, 0, visual.RgbBlack, c, render.BlendAlphaBg, alpha*visual.KrakenLandingAlpha, terminal.AttrNone)
			} else if alpha >= visual.Effect256Threshold {
				buf.SetBg256(sx, sy, visual.Kraken256Landing)
			}
		}
	}
	if r.style == visual.KrakenLandingRing {
		r.mark(ctx, buf, vmath.Area{X: int(x), Y: int(y), Width: 1, Height: 1}, time.Duration(step)*visual.LandingBlinkStep)
	}
}

// cellTrueColor lights the background alone, so a glyph under the marker still reads
func (r *LandingRenderer) cellTrueColor(buf *render.RenderBuffer, screenX, screenY, step int) {
	buf.SetBgOnly(screenX, screenY, visual.RgbLandingBlink[step%len(visual.RgbLandingBlink)])
}

// cell256 draws a foreground block: eight backgrounds cannot show the bright entries
func (r *LandingRenderer) cell256(buf *render.RenderBuffer, screenX, screenY, step int) {
	entry := visual.Landing256Blink[step%len(visual.Landing256Blink)]
	buf.SetFgOnly(screenX, screenY, visual.LandingMark256, color.RGB{R: entry}, terminal.AttrFg256)
}
