package renderer

import (
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

// TeleportLineRenderer draws a phase-based beam from the swarm to its teleport destination,
// and from a data species standing in a state flagged Beam to the cell it stored
// Phases: Fill (beam extends) → Hold (full line) → Recede (darkness sweeps)
type TeleportLineRenderer struct {
	gameCtx    *engine.GameContext
	renderCell teleportLineCellRenderer
}

// teleportLineCellRenderer draws one beam cell at the given intensity
type teleportLineCellRenderer func(buf *render.RenderBuffer, screenX, screenY int, intensity float64)

func NewTeleportLineRenderer(ctx *engine.GameContext) *TeleportLineRenderer {
	r := &TeleportLineRenderer{gameCtx: ctx}
	if ctx.World.Resources.Config.ColorMode == terminal.ColorMode256 {
		r.renderCell = r.cell256
	} else {
		r.renderCell = r.cellTrueColor
	}
	return r
}

func (r *TeleportLineRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	world := r.gameCtx.World
	swarms, behaviors := world.Components.Swarm, world.Components.Behavior
	if swarms.CountEntities() == 0 && behaviors.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	swarms.Each(func(headerEntity core.Entity, swarmComp *component.SwarmComponent) bool {
		if swarmComp.Machine.State != component.SwarmStateTeleport {
			return true
		}

		// The beam leaves the header where it stands: scrolling rock can carry a swarm mid-teleport
		headerPos, ok := world.Positions.GetPosition(headerEntity)
		if !ok {
			return true
		}

		r.renderBeam(ctx, buf,
			footprintAt(headerPos.X, headerPos.Y, parameter.SwarmWidth, parameter.SwarmHeight,
				parameter.SwarmHeaderOffsetX, parameter.SwarmHeaderOffsetY),
			footprintAt(swarmComp.TeleportTargetX, swarmComp.TeleportTargetY, parameter.SwarmWidth, parameter.SwarmHeight,
				parameter.SwarmHeaderOffsetX, parameter.SwarmHeaderOffsetY),
			swarmComp.Machine.Progress())
		return true
	})

	// A data species' beam runs over its state's entry, the footprint its prototype's
	prototypes := world.Resources.Behaviors
	behaviors.Each(func(header core.Entity, b *component.BehaviorComponent) bool {
		if !b.Beaming || int(b.Program) >= len(prototypes) {
			return true
		}
		pos, ok := world.Positions.GetPosition(header)
		if !ok {
			return true
		}
		p := &prototypes[b.Program]
		r.renderBeam(ctx, buf,
			footprintAt(pos.X, pos.Y, p.Width, p.Height, p.HeaderX, p.HeaderY),
			footprintAt(b.TargetX, b.TargetY, p.Width, p.Height, p.HeaderX, p.HeaderY),
			b.Machine.Progress())
		return true
	})
}

// renderBeam draws the line with phase-based visibility from the body's centre into the
// landing's, where the landing marker blinks, skipping the cells of the body it leaves
func (r *TeleportLineRenderer) renderBeam(
	ctx render.RenderContext, buf *render.RenderBuffer,
	body, landing vmath.Area,
	progress float64,
) {
	from, to := body.Center(), landing.Center()
	line := vmath.NewLine(from.X, from.Y, to.X, to.Y)
	totalSteps := line.Steps()
	if totalSteps == 0 {
		return
	}

	// Calculate visible segment bounds based on phase
	var segStart, segEnd float64
	switch {
	case progress < visual.TeleportFillEnd:
		fillT := progress / visual.TeleportFillEnd
		segStart = 0
		segEnd = fillT
	case progress < visual.TeleportHoldEnd:
		segStart = 0
		segEnd = 1
	default:
		recedeT := (progress - visual.TeleportHoldEnd) / visual.TeleportRecede
		segStart = recedeT
		segEnd = 1
	}

	invSteps := 1.0 / float64(totalSteps)
	for step := range totalSteps + 1 {
		t := float64(step) * invSteps

		if t >= segStart && t <= segEnd && !body.Contains(line.X(), line.Y()) {
			screenX, screenY, visible := ctx.MapToScreen(line.X(), line.Y())
			if visible {
				r.renderCell(buf, screenX, screenY, r.calcIntensity(progress, t, segStart, segEnd))
			}
		}
		line = line.Next()
	}
}

// calcIntensity returns brightness [0,1] based on phase and position within segment
func (r *TeleportLineRenderer) calcIntensity(progress, t, segStart, segEnd float64) float64 {
	if segEnd <= segStart {
		return 1.0
	}

	segLen := segEnd - segStart
	posInSeg := (t - segStart) / segLen

	switch {
	case progress < visual.TeleportFillEnd:
		// Fill: gradient dim→bright toward leading edge, pulse at head
		base := 0.4 + 0.6*posInSeg
		if posInSeg > 0.9 {
			base = 1.0
		}
		return base

	case progress < visual.TeleportHoldEnd:
		return 1.0

	default:
		// Recede: cells closer to target stay brighter
		return 0.3 + 0.7*posInSeg
	}
}

func (r *TeleportLineRenderer) cellTrueColor(buf *render.RenderBuffer, screenX, screenY int, intensity float64) {
	buf.Set(screenX, screenY, 0, visual.RgbBlack, color.Scale(visual.RgbSwarmTeleport, intensity),
		render.BlendMaxBg, 1.0, terminal.AttrNone)
}

// cell256 has no intensity ramp, so dim beam cells are dropped rather than scaled
func (r *TeleportLineRenderer) cell256(buf *render.RenderBuffer, screenX, screenY int, intensity float64) {
	if intensity > parameter.SwarmTeleport256Threshold {
		buf.SetBg256(screenX, screenY, visual.SwarmChargeLine256Palette)
	}
}
