package renderer

import (
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
	swarms := r.gameCtx.World.Components.Swarm
	if swarms.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	pulseDur := parameter.SwarmChargeDuration
	if pulseDur <= 0 {
		return
	}
	showDelay := parameter.SwarmChargeLineShowDelay
	if showDelay < 0 {
		showDelay = 0
	}
	pulseWindow := parameter.SwarmChargeLinePulseCount * pulseDur

	swarms.Each(func(headerEntity core.Entity, swarmComp *component.SwarmComponent) bool {
		if swarmComp.Machine.State != component.SwarmStateLock {
			return true
		}

		pulseElapsed := swarmComp.Machine.Elapsed - showDelay
		if pulseElapsed < 0 || pulseElapsed >= pulseWindow {
			return true
		}

		pulseIndex := int(pulseElapsed / pulseDur)
		pulseLocalT := pulseElapsed % pulseDur
		headT := float64(pulseLocalT) / float64(pulseDur)

		// Escalating alpha per pulse
		alpha := parameter.SwarmChargeLineAlpha1
		if pulseIndex > 0 {
			alpha = parameter.SwarmChargeLineAlpha2
		}

		headerPos, ok := r.gameCtx.World.Positions.GetPosition(headerEntity)
		if !ok {
			return true
		}

		r.tracePulse(ctx, buf,
			headerPos.X, headerPos.Y,
			swarmComp.LockedTargetX, swarmComp.LockedTargetY,
			headT, alpha)
		return true
	})
}

// tracePulse draws a single pulse via Bresenham, skipping cells inside swarm footprint
// headT [0,1] is normalized pulse head position along the line
func (r *ChargeLineRenderer) tracePulse(
	ctx render.RenderContext, buf *render.RenderBuffer,
	x0, y0, x1, y1 int, headT, baseAlpha float64,
) {
	line := vmath.NewLine(x0, y0, x1, y1)
	totalSteps := line.Steps()
	if totalSteps == 0 {
		return
	}

	// Swarm bounding box for edge clipping
	bbox := vmath.Area{
		X: x0 - parameter.SwarmHeaderOffsetX, Y: y0 - parameter.SwarmHeaderOffsetY,
		Width: parameter.SwarmWidth, Height: parameter.SwarmHeight,
	}

	invSteps := 1.0 / float64(totalSteps)
	trailLen := parameter.SwarmChargeLineTrail
	if trailLen <= 0.0 {
		return
	}

	for step := range totalSteps + 1 {
		// Skip cells inside swarm body
		if !bbox.Contains(line.X(), line.Y()) {
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
