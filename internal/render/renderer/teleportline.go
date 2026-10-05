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
)

// Phase thresholds matching materialize timing
const (
	teleportFillEnd = 0.3
	teleportHoldEnd = 0.7
	teleportRecede  = 1.0 - teleportHoldEnd
)

// TeleportLineRenderer draws a phase-based beam from swarm origin to teleport destination
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

	swarms.Each(func(_ core.Entity, swarmComp *component.SwarmComponent) bool {
		if swarmComp.State != component.SwarmStateTeleport {
			return true
		}

		elapsed := parameter.SwarmTeleportDuration - swarmComp.TeleportRemaining
		progress := float64(elapsed) / float64(parameter.SwarmTeleportDuration)
		if progress < 0 {
			progress = 0
		}
		if progress > 1 {
			progress = 1
		}

		r.renderBeam(ctx, buf,
			swarmComp.TeleportStartX, swarmComp.TeleportStartY,
			swarmComp.TeleportTargetX, swarmComp.TeleportTargetY,
			parameter.SwarmWidth, parameter.SwarmHeight,
			parameter.SwarmHeaderOffsetX, parameter.SwarmHeaderOffsetY,
			progress)
		return true
	})
}

// renderBeam draws single line with phase-based visibility, excluding entity bounding boxes
// headerX/Y are anchor positions; width/height and offsets define the bounding box to exclude
func (r *TeleportLineRenderer) renderBeam(
	ctx render.RenderContext, buf *render.RenderBuffer,
	x0, y0, x1, y1 int,
	boxWidth, boxHeight, headerOffsetX, headerOffsetY int,
	progress float64,
) {
	dx := x1 - x0
	dy := y1 - y0
	absDx, absDy := dx, dy
	if absDx < 0 {
		absDx = -absDx
	}
	if absDy < 0 {
		absDy = -absDy
	}

	totalSteps := max(absDx, absDy)
	if totalSteps == 0 {
		return
	}

	// Calculate bounding boxes for start and end entities
	startBoxMinX := x0 - headerOffsetX
	startBoxMinY := y0 - headerOffsetY
	endBoxMinX := x1 - headerOffsetX
	endBoxMinY := y1 - headerOffsetY

	// Calculate visible segment bounds based on phase
	var segStart, segEnd float64
	switch {
	case progress < teleportFillEnd:
		fillT := progress / teleportFillEnd
		segStart = 0
		segEnd = fillT
	case progress < teleportHoldEnd:
		segStart = 0
		segEnd = 1
	default:
		recedeT := (progress - teleportHoldEnd) / teleportRecede
		segStart = recedeT
		segEnd = 1
	}

	stepX, stepY := 1, 1
	if dx < 0 {
		stepX = -1
	}
	if dy < 0 {
		stepY = -1
	}

	invSteps := 1.0 / float64(totalSteps)
	err := absDx - absDy
	mapX, mapY := x0, y0

	for step := range totalSteps + 1 {
		t := float64(step) * invSteps

		// Only draw cells within visible segment
		if t >= segStart && t <= segEnd {
			// Skip cells inside start entity bounding box
			if mapX >= startBoxMinX && mapX < startBoxMinX+boxWidth &&
				mapY >= startBoxMinY && mapY < startBoxMinY+boxHeight {
				goto advance
			}
			// Skip cells inside end entity bounding box
			if mapX >= endBoxMinX && mapX < endBoxMinX+boxWidth &&
				mapY >= endBoxMinY && mapY < endBoxMinY+boxHeight {
				goto advance
			}

			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if visible {
				r.renderCell(buf, screenX, screenY, r.calcIntensity(progress, t, segStart, segEnd))
			}
		}

	advance:
		if step < totalSteps {
			e2 := 2 * err
			if e2 > -absDy {
				err -= absDy
				mapX += stepX
			}
			if e2 < absDx {
				err += absDx
				mapY += stepY
			}
		}
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
	case progress < teleportFillEnd:
		// Fill: gradient dim→bright toward leading edge, pulse at head
		base := 0.4 + 0.6*posInSeg
		if posInSeg > 0.9 {
			base = 1.0
		}
		return base

	case progress < teleportHoldEnd:
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
