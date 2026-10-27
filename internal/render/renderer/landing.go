package renderer

import (
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
// state is flagged Landing. The lines show the path; this shows where to keep clear.
type LandingRenderer struct {
	gameCtx    *engine.GameContext
	renderCell landingCellRenderer
}

// landingCellRenderer draws the marker at step of its colour cycle
type landingCellRenderer func(buf *render.RenderBuffer, screenX, screenY, step int)

func NewLandingRenderer(ctx *engine.GameContext) *LandingRenderer {
	r := &LandingRenderer{gameCtx: ctx}
	if ctx.World.Resources.Config.ColorMode == terminal.ColorMode256 {
		r.renderCell = r.cell256
	} else {
		r.renderCell = r.cellTrueColor
	}
	return r
}

func (r *LandingRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	world := r.gameCtx.World
	swarms, behaviors := world.Components.Swarm, world.Components.Behavior
	if swarms.CountEntities() == 0 && behaviors.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

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
		r.mark(ctx, buf, footprintAt(x, y, parameter.SwarmWidth, parameter.SwarmHeight,
			parameter.SwarmHeaderOffsetX, parameter.SwarmHeaderOffsetY), sw.Machine.Elapsed)
		return true
	})

	prototypes := world.Resources.Behaviors
	behaviors.Each(func(_ core.Entity, b *component.BehaviorComponent) bool {
		if !b.Landing || int(b.Program) >= len(prototypes) {
			return true
		}
		p := &prototypes[b.Program]
		r.mark(ctx, buf, footprintAt(b.TargetX, b.TargetY, p.Width, p.Height, p.HeaderX, p.HeaderY), b.Machine.Elapsed)
		return true
	})
}

// footprintAt is the area a body covers with its header on (x, y); its centre is where
// the landing marker blinks and where the charge and teleport lines run between
func footprintAt(x, y, width, height, headerX, headerY int) vmath.Area {
	return vmath.Area{X: x - headerX, Y: y - headerY, Width: width, Height: height}
}

// mark draws the marker on the footprint's centre, its cycle starting on each state's entry
func (r *LandingRenderer) mark(ctx render.RenderContext, buf *render.RenderBuffer, footprint vmath.Area, elapsed time.Duration) {
	c := footprint.Center()
	if screenX, screenY, visible := ctx.MapToScreen(c.X, c.Y); visible {
		r.renderCell(buf, screenX, screenY, int(elapsed/visual.LandingBlinkStep))
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
