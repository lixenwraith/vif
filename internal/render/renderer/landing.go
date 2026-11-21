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
// state is flagged Landing. A Kraken's charge marks where its body ends, far larger,
// and a Devourer's omen the ray its gut forms along. The lines show the path; this
// shows where to keep clear.
type LandingRenderer struct {
	gameCtx    *engine.GameContext
	renderCell landingCellRenderer
	palette    bool
	shape      component.DevourerShape
}

// landingCellRenderer draws the marker at step of its colour cycle
type landingCellRenderer func(buf *render.RenderBuffer, screenX, screenY, step int)

func NewLandingRenderer(ctx *engine.GameContext) *LandingRenderer {
	r := &LandingRenderer{gameCtx: ctx}
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
	devourers := world.Components.Devourer
	if swarms.CountEntities() == 0 && behaviors.CountEntities() == 0 && krakens.CountEntities() == 0 &&
		devourers.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	krakens.Each(func(_ core.Entity, k *component.KrakenComponent) bool {
		if k.State == component.KrakenSpin || k.State == component.KrakenMove {
			r.markCharge(ctx, buf, k)
		}
		return true
	})

	devourers.Each(func(_ core.Entity, d *component.DevourerComponent) bool {
		if d.Omen.Remaining > 0 {
			r.markOmen(ctx, buf, d)
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

// markCharge marks where a Kraken's charge ends, from its spin's first tick until
// the body arrives: an outline KrakenLandingReach body radii round, over a glow
// that rises through the spin and fades toward the centre
func (r *LandingRenderer) markCharge(ctx render.RenderContext, buf *render.RenderBuffer, k *component.KrakenComponent) {
	glow := visual.KrakenLandingGlowMax
	if k.State == component.KrakenSpin {
		spun := min(max(1-k.StateRemaining.Seconds()/parameter.KrakenSpinDuration.Seconds(), 0), 1)
		glow = visual.KrakenLandingGlowMin + (visual.KrakenLandingGlowMax-visual.KrakenLandingGlowMin)*spun
	}
	x, y := k.TargetX, k.TargetY
	ry := parameter.KrakenBodyRadius * visual.KrakenLandingReach
	rx := ry * parameter.KrakenRowSquash
	half := visual.KrakenLandingBand / 2
	for cy := int(math.Ceil(y - ry - vmath.CellCenter)); cy <= int(y+ry-vmath.CellCenter); cy++ {
		for cx := int(math.Ceil(x - rx - vmath.CellCenter)); cx <= int(x+rx-vmath.CellCenter); cx++ {
			d := math.Hypot((float64(cx)+vmath.CellCenter-x)/rx, (float64(cy)+vmath.CellCenter-y)/ry)
			sx, sy, visible := ctx.MapToScreen(cx, cy)
			if d > 1 || !visible {
				continue
			}
			// The outline eases in and out across its band, whose outer edge is the mark's
			ring := max(1-math.Abs(d-1+half)/half, 0)
			ring *= ring * (3 - 2*ring)
			if r.palette {
				// A console cannot blend the glow, so it draws the outline, as a shield's
				if ring >= visual.Effect256Threshold {
					buf.SetBg256(sx, sy, visual.Kraken256Landing)
				}
				continue
			}
			alpha := visual.KrakenLandingAlpha * ring
			if inner := d / (1 - half); inner < 1 {
				alpha = max(alpha, glow*inner*inner)
			}
			if alpha > 0 {
				buf.Set(sx, sy, 0, visual.RgbBlack, visual.RgbKrakenAttack, render.BlendAlphaBg, alpha, terminal.AttrNone)
			}
		}
	}
}

// markOmen lights the ray a Devourer's gut forms along, whole from its first tick
// and over the open space alone: a core that rises toward the attack color as the
// gut nears, beating faster, its ends aglow, in a faint halo. Each cell takes its
// distance from the ray once, so no halo doubles where two of the ray's cells meet.
func (r *LandingRenderer) markOmen(ctx render.RenderContext, buf *render.RenderBuffer, d *component.DevourerComponent) {
	cfg := r.gameCtx.World.Resources.Config
	// Its system built its shape this frame already, unless something moved it since
	shape := r.gameCtx.World.Resources.DevourerShape
	if !shape.Built(d.Seed, d.Frame(), d.Grown(), cfg.MapWidth, cfg.MapHeight) {
		r.shape.Build(d.Seed, d.Frame(), d.Grown(), cfg.MapWidth, cfg.MapHeight)
		shape = &r.shape
	}
	x0, y0 := shape.Wall(d.Omen.From)
	x1, y1 := shape.Wall(d.Omen.To)
	near := min(max(1-d.Omen.Remaining.Seconds()/parameter.DevourerOmen.Seconds(), 0), 1)
	beat := 0.5 + 0.5*math.Cos(2*math.Pi*(2+3*near)*near)
	core := (visual.DevourerOmenCoreMin + (visual.DevourerOmenCoreMax-visual.DevourerOmenCoreMin)*near) * (0.8 + 0.2*beat)
	halo := visual.KrakenLandingGlowMin + (visual.KrakenLandingGlowMax-visual.KrakenLandingGlowMin)*near
	reach := visual.DevourerOmenHalo
	dx, dy := x1-x0, y1-y0
	span := dx*dx + dy*dy
	for row := max(0, int(math.Ceil((min(y0, y1)-reach)/2-vmath.CellCenter))); row <= min(cfg.MapHeight-1, int((max(y0, y1)+reach)/2-vmath.CellCenter)); row++ {
		cy := 2 * (float64(row) + vmath.CellCenter)
		// The ray's columns within reach of this row
		ta, tb := 0.0, 1.0
		if dy != 0 {
			ta, tb = min(max((cy-reach-y0)/dy, 0), 1), min(max((cy+reach-y0)/dy, 0), 1)
		}
		xa, xb := min(x0+dx*ta, x0+dx*tb), max(x0+dx*ta, x0+dx*tb)
		for col := max(0, int(math.Ceil(xa-reach-vmath.CellCenter))); col <= min(cfg.MapWidth-1, int(xb+reach-vmath.CellCenter)); col++ {
			cx := float64(col) + vmath.CellCenter
			t := 0.0
			if span > 0 {
				t = min(max(((cx-x0)*dx+(cy-y0)*dy)/span, 0), 1)
			}
			dist := math.Hypot(cx-x0-dx*t, cy-y0-dy*t)
			end := min(math.Hypot(cx-x0, cy-y0), math.Hypot(cx-x1, cy-y1))
			if dist > reach || shape.Covers(col, row) {
				continue
			}
			sx, sy, visible := ctx.MapToScreen(col, row)
			if !visible {
				continue
			}
			alpha := core
			if dist > visual.DevourerOmenCore {
				fade := 1 - (dist-visual.DevourerOmenCore)/(reach-visual.DevourerOmenCore)
				alpha = halo * fade * fade
			}
			if end < visual.DevourerOmenEnd {
				alpha = max(alpha, core*(1-end/visual.DevourerOmenEnd))
			}
			if r.palette {
				// A console cannot blend the halo, so it draws the core alone
				if alpha >= visual.Effect256Threshold && dist <= visual.DevourerOmenCore {
					buf.SetBg256(sx, sy, visual.Devourer256Omen)
				}
				continue
			}
			buf.Set(sx, sy, 0, visual.RgbBlack, visual.RgbDevourerOmen, render.BlendAlphaBg, alpha, terminal.AttrNone)
		}
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
