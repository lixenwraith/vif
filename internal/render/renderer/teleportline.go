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

// TeleportLineRenderer draws a phase-based beam from the swarm to its teleport destination
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
	swarms := r.gameCtx.World.Components.Swarm
	if swarms.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	swarms.Each(func(headerEntity core.Entity, swarmComp *component.SwarmComponent) bool {
		if swarmComp.Machine.State != component.SwarmStateTeleport {
			return true
		}

		// The beam leaves the header where it stands: scrolling rock can carry a swarm mid-teleport
		headerPos, ok := r.gameCtx.World.Positions.GetPosition(headerEntity)
		if !ok {
			return true
		}

		r.renderBeam(ctx, buf,
			headerPos.X, headerPos.Y,
			swarmComp.TeleportTargetX, swarmComp.TeleportTargetY,
			parameter.SwarmWidth, parameter.SwarmHeight,
			parameter.SwarmHeaderOffsetX, parameter.SwarmHeaderOffsetY,
			swarmComp.Machine.Progress())
		return true
	})
}

// renderBeam draws single line with phase-based visibility, excluding entity bounding boxes
// (x0, y0) and (x1, y1) are header cells; width/height and offsets define the bounding box to exclude
func (r *TeleportLineRenderer) renderBeam(
	ctx render.RenderContext, buf *render.RenderBuffer,
	x0, y0, x1, y1 int,
	boxWidth, boxHeight, headerOffsetX, headerOffsetY int,
	progress float64,
) {
	line := vmath.NewLine(x0, y0, x1, y1)
	totalSteps := line.Steps()
	if totalSteps == 0 {
		return
	}

	// Calculate bounding boxes for start and end entities
	startBox := vmath.Area{X: x0 - headerOffsetX, Y: y0 - headerOffsetY, Width: boxWidth, Height: boxHeight}
	endBox := vmath.Area{X: x1 - headerOffsetX, Y: y1 - headerOffsetY, Width: boxWidth, Height: boxHeight}

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

		// Only draw cells within visible segment, outside both bounding boxes
		if t >= segStart && t <= segEnd && !startBox.Contains(line.X(), line.Y()) && !endBox.Contains(line.X(), line.Y()) {
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
