package renderer

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// ChargeLineRenderer draws pulsing bg-only warning lines from swarm to locked target
// Active only during SwarmStateLock within the pulse visibility window
// Two pulses travel swarm→target at charge speed, acting as countdown before the charge
type ChargeLineRenderer struct {
	gameCtx *engine.GameContext
	is256   bool
}

func NewChargeLineRenderer(ctx *engine.GameContext) *ChargeLineRenderer {
	return &ChargeLineRenderer{
		gameCtx: ctx,
		is256:   ctx.World.Resources.Config.ColorMode == terminal.ColorMode256,
	}
}

func (r *ChargeLineRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	headerEntities := r.gameCtx.World.Components.Swarm.GetAllEntities()
	if len(headerEntities) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	pulseDur := parameter.SwarmChargeDuration
	showDelay := parameter.SwarmChargeLineShowDelay
	if showDelay < 0 {
		showDelay = 0
	}
	pulseWindow := parameter.SwarmChargeLinePulseCount * pulseDur

	for _, headerEntity := range headerEntities {
		swarmComp, ok := r.gameCtx.World.Components.Swarm.GetComponent(headerEntity)
		if !ok || swarmComp.State != component.SwarmStateLock {
			continue
		}

		lockElapsed := parameter.SwarmLockDuration - swarmComp.LockRemaining
		pulseElapsed := lockElapsed - showDelay
		if pulseElapsed < 0 || pulseElapsed >= pulseWindow {
			continue
		}

		pulseIndex := int(pulseElapsed / pulseDur)
		pulseLocalT := pulseElapsed % pulseDur
		headT := float64(pulseLocalT) / float64(pulseDur)

		// Escalating alpha per pulse
		alpha := parameter.SwarmChargeLineAlpha1Float
		if pulseIndex > 0 {
			alpha = parameter.SwarmChargeLineAlpha2Float
		}

		headerPos, ok := r.gameCtx.World.Positions.GetPosition(headerEntity)
		if !ok {
			continue
		}

		r.tracePulse(ctx, buf,
			headerPos.X, headerPos.Y,
			swarmComp.LockedTargetX, swarmComp.LockedTargetY,
			headT, alpha)
	}
}

// tracePulse draws a single pulse via Bresenham, skipping cells inside swarm footprint
// headT [0,1] is normalized pulse head position along the line
func (r *ChargeLineRenderer) tracePulse(
	ctx render.RenderContext, buf *render.RenderBuffer,
	x0, y0, x1, y1 int, headT, baseAlpha float64,
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

	stepX, stepY := 1, 1
	if dx < 0 {
		stepX = -1
	}
	if dy < 0 {
		stepY = -1
	}

	// Swarm bounding box for edge clipping
	bboxMinX := x0 - parameter.SwarmHeaderOffsetX
	bboxMaxX := bboxMinX + parameter.SwarmWidth - 1
	bboxMinY := y0 - parameter.SwarmHeaderOffsetY
	bboxMaxY := bboxMinY + parameter.SwarmHeight - 1

	invSteps := 1.0 / float64(totalSteps)
	trailLen := parameter.SwarmChargeLineTrailFloat

	err := absDx - absDy
	mapX, mapY := x0, y0

	for step := 0; step <= totalSteps; step++ {
		// Skip cells inside swarm body
		inBox := mapX >= bboxMinX && mapX <= bboxMaxX && mapY >= bboxMinY && mapY <= bboxMaxY
		if !inBox {
			t := float64(step) * invSteps
			if t <= headT {
				trailDist := headT - t
				if trailDist <= trailLen {
					screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
					if visible {
						cellAlpha := baseAlpha * (1.0 - trailDist/trailLen)
						if r.is256 {
							if cellAlpha > parameter.SwarmChargeLine256Threshold {
								buf.SetBg256(screenX, screenY, visual.SwarmChargeLine256Palette)
							}
						} else {
							buf.Set(screenX, screenY, 0, visual.RgbBlack,
								visual.RgbSwarmChargeLine, render.BlendMaxBg,
								cellAlpha, terminal.AttrNone)
						}
					}
				}
			}
		}

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