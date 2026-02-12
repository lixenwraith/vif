// FILE: render/renderer/teleportline.go
package renderer

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
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
	gameCtx *engine.GameContext
	is256   bool
}

func NewTeleportLineRenderer(ctx *engine.GameContext) *TeleportLineRenderer {
	return &TeleportLineRenderer{
		gameCtx: ctx,
		is256:   ctx.World.Resources.Render.ColorMode == terminal.ColorMode256,
	}
}

func (r *TeleportLineRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	headerEntities := r.gameCtx.World.Components.Swarm.GetAllEntities()
	if len(headerEntities) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	for _, headerEntity := range headerEntities {
		swarmComp, ok := r.gameCtx.World.Components.Swarm.GetComponent(headerEntity)
		if !ok || swarmComp.State != component.SwarmStateTeleport {
			continue
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
	}
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

	for step := 0; step <= totalSteps; step++ {
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
				intensity := r.calcIntensity(progress, t, segStart, segEnd)

				if r.is256 {
					if intensity > parameter.SwarmTeleport256Threshold {
						buf.SetBg256(screenX, screenY, visual.SwarmChargeLine256Palette)
					}
				} else {
					scaledColor := render.Scale(visual.RgbSwarmTeleport, intensity)
					buf.Set(screenX, screenY, 0, visual.RgbBlack, scaledColor,
						render.BlendMaxBg, 1.0, terminal.AttrNone)
				}
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