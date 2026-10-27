package renderer

import (
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// ChargeLineRenderer draws pulsing bg-only warning lines from swarm to locked target
// Active only during SwarmStateLock within the pulse visibility window
// Two pulses travel swarm→target at charge speed, acting as countdown before the charge
// A data species in a state flagged ChargeLine draws the same line to its stored cell
type ChargeLineRenderer struct {
	gameCtx    *engine.GameContext
	renderCell chargeLineCellRenderer
}

// chargeLineCellRenderer draws one pulse cell at the given trail alpha
type chargeLineCellRenderer func(buf *render.RenderBuffer, screenX, screenY int, alpha float64)

func NewChargeLineRenderer(ctx *engine.GameContext) *ChargeLineRenderer {
	r := &ChargeLineRenderer{gameCtx: ctx}
	if ctx.World.Resources.Config.ColorMode == terminal.ColorMode256 {
		r.renderCell = r.cell256
	} else {
		r.renderCell = r.cellTrueColor
	}
	return r
}

func (r *ChargeLineRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	world := r.gameCtx.World
	swarms, behaviors := world.Components.Swarm, world.Components.Behavior
	if swarms.CountEntities() == 0 && behaviors.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	swarms.Each(func(headerEntity core.Entity, swarmComp *component.SwarmComponent) bool {
		if swarmComp.Machine.State != component.SwarmStateLock {
			return true
		}

		headerPos, ok := world.Positions.GetPosition(headerEntity)
		if !ok {
			return true
		}

		r.renderPulses(ctx, buf,
			footprintAt(headerPos.X, headerPos.Y, parameter.SwarmWidth, parameter.SwarmHeight,
				parameter.SwarmHeaderOffsetX, parameter.SwarmHeaderOffsetY),
			footprintAt(swarmComp.LockedTargetX, swarmComp.LockedTargetY, parameter.SwarmWidth, parameter.SwarmHeight,
				parameter.SwarmHeaderOffsetX, parameter.SwarmHeaderOffsetY),
			swarmComp.Machine.Elapsed, parameter.SwarmChargeDuration, parameter.SwarmChargeLineShowDelay)
		return true
	})

	// A data species' pulses end with its flagged entry, each the share of it the
	// swarm's charge is of its lock, so they keep the swarm's rhythm at any lock
	prototypes := world.Resources.Behaviors
	behaviors.Each(func(header core.Entity, b *component.BehaviorComponent) bool {
		if !b.ChargeLine || int(b.Program) >= len(prototypes) {
			return true
		}
		pos, ok := world.Positions.GetPosition(header)
		if !ok {
			return true
		}
		p := &prototypes[b.Program]
		pulse := time.Duration(float64(b.Machine.Duration) * parameter.BehaviorChargeLinePulseShare)
		r.renderPulses(ctx, buf,
			footprintAt(pos.X, pos.Y, p.Width, p.Height, p.HeaderX, p.HeaderY),
			footprintAt(b.TargetX, b.TargetY, p.Width, p.Height, p.HeaderX, p.HeaderY),
			b.Machine.Elapsed, pulse, b.Machine.Duration-parameter.SwarmChargeLinePulseCount*pulse)
		return true
	})
}

// renderPulses draws the pulse due elapsed into a lock: SwarmChargeLinePulseCount
// pulses of pulse each from showDelay on, the later one brighter, from the body's
// centre to the landing's, where the landing marker blinks
func (r *ChargeLineRenderer) renderPulses(
	ctx render.RenderContext, buf *render.RenderBuffer,
	body, landing vmath.Area,
	elapsed, pulse, showDelay time.Duration,
) {
	if pulse <= 0 {
		return
	}
	pulseElapsed := elapsed - max(showDelay, 0)
	if pulseElapsed < 0 || pulseElapsed >= parameter.SwarmChargeLinePulseCount*pulse {
		return
	}

	pulseIndex := int(pulseElapsed / pulse)
	// Elapsed moves a tick at a time, so the head reaches the landing on a pulse's
	// last tick rather than at the instant after it, which no frame shows
	span := max(pulse-parameter.GameUpdateInterval, 1)
	headT := min(float64(pulseElapsed%pulse)/float64(span), 1)

	// Escalating alpha per pulse
	alpha := parameter.SwarmChargeLineAlpha1
	if pulseIndex > 0 {
		alpha = parameter.SwarmChargeLineAlpha2
	}

	from, to := body.Center(), landing.Center()
	r.tracePulse(ctx, buf, from.X, from.Y, to.X, to.Y, body, headT, alpha)
}

// tracePulse draws a single pulse via Bresenham, skipping cells inside the body
// headT [0,1] is normalized pulse head position along the line
func (r *ChargeLineRenderer) tracePulse(
	ctx render.RenderContext, buf *render.RenderBuffer,
	x0, y0, x1, y1 int, body vmath.Area, headT, baseAlpha float64,
) {
	line := vmath.NewLine(x0, y0, x1, y1)
	totalSteps := line.Steps()
	if totalSteps == 0 {
		return
	}

	invSteps := 1.0 / float64(totalSteps)
	trailLen := parameter.SwarmChargeLineTrail
	if trailLen <= 0.0 {
		return
	}

	for step := range totalSteps + 1 {
		// Skip cells inside the body
		if !body.Contains(line.X(), line.Y()) {
			t := float64(step) * invSteps
			if t <= headT {
				trailDist := headT - t
				if trailDist <= trailLen {
					screenX, screenY, visible := ctx.MapToScreen(line.X(), line.Y())
					if visible {
						r.renderCell(buf, screenX, screenY, baseAlpha*(1.0-trailDist/trailLen))
					}
				}
			}
		}
		line = line.Next()
	}
}

func (r *ChargeLineRenderer) cellTrueColor(buf *render.RenderBuffer, screenX, screenY int, alpha float64) {
	buf.Set(screenX, screenY, 0, visual.RgbBlack, visual.RgbSwarmChargeLine, render.BlendMaxBg, alpha, terminal.AttrNone)
}

// cell256 has no alpha, so faint trail cells are dropped rather than dimmed
func (r *ChargeLineRenderer) cell256(buf *render.RenderBuffer, screenX, screenY int, alpha float64) {
	if alpha > parameter.SwarmChargeLine256Threshold {
		buf.SetBg256(screenX, screenY, visual.SwarmChargeLine256Palette)
	}
}
