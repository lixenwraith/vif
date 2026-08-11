package renderer

import (
	"math"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// Phase thresholds for fill, hold, and recede.
const (
	matFillEnd = parameter.MaterializeFillEnd
	matHoldEnd = parameter.MaterializeHoldEnd
	matRecede  = 1.0 - matHoldEnd
)

type beamDir int

const (
	dirUp beamDir = iota
	dirDown
	dirLeft
	dirRight
)

// MaterializeRenderer draws phase-based converging beams
type MaterializeRenderer struct {
	gameCtx *engine.GameContext
}

func NewMaterializeRenderer(ctx *engine.GameContext) *MaterializeRenderer {
	return &MaterializeRenderer{
		gameCtx: ctx,
	}
}

func (r *MaterializeRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	materializes := r.gameCtx.World.Components.Materialize
	if materializes.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	materializes.Each(func(_ core.Entity, mat *component.MaterializeComponent) bool {
		// Transform target area to viewport coords
		targetVX, targetVY, _ := ctx.MapToViewport(mat.TargetX, mat.TargetY)

		r.renderBeam(ctx, buf, mat, targetVX, targetVY, dirUp)
		r.renderBeam(ctx, buf, mat, targetVX, targetVY, dirDown)
		r.renderBeam(ctx, buf, mat, targetVX, targetVY, dirLeft)
		r.renderBeam(ctx, buf, mat, targetVX, targetVY, dirRight)
		return true
	})
}

func (r *MaterializeRenderer) renderBeam(ctx render.RenderContext, buf *render.RenderBuffer, mat *component.MaterializeComponent, targetVX, targetVY int, dir beamDir) {
	var edgePos, distance int
	var spanStart, spanEnd int // Range along the target edge

	switch dir {
	case dirUp:
		edgePos = 0
		distance = targetVY
		spanStart = targetVX
		spanEnd = targetVX + mat.AreaWidth - 1
	case dirDown:
		edgePos = ctx.ViewportHeight - 1
		targetBottom := targetVY + mat.AreaHeight - 1
		distance = ctx.ViewportHeight - 1 - targetBottom
		spanStart = targetVX
		spanEnd = targetVX + mat.AreaWidth - 1
	case dirLeft:
		edgePos = 0
		distance = targetVX
		spanStart = targetVY
		spanEnd = targetVY + mat.AreaHeight - 1
	case dirRight:
		edgePos = ctx.ViewportWidth - 1
		targetRight := targetVX + mat.AreaWidth - 1
		distance = ctx.ViewportWidth - 1 - targetRight
		spanStart = targetVY
		spanEnd = targetVY + mat.AreaHeight - 1
	}

	if distance <= 0 {
		return // Target at edge, no beam to draw
	}

	dist := float64(distance)
	progress := mat.Progress

	// Calculate segment bounds based on phase.
	var segStartF, segEndF float64

	switch {
	case progress < matFillEnd:
		// Fill: edge to leading edge
		// fillProgress = progress / fillEnd (normalized within fill phase)
		if matFillEnd == 0.0 {
			return
		}
		fillProgress := progress / matFillEnd
		segStartF = 0.0
		segEndF = fillProgress * dist

	case progress < matHoldEnd:
		// Hold: full line
		segStartF = 0.0
		segEndF = dist

	default:
		// Recede: darkness from edge toward target
		// recedeProgress = (progress - holdEnd) / (1.0 - holdEnd)
		if matRecede == 0.0 {
			return
		}
		recedeProgress := (progress - matHoldEnd) / matRecede
		segStartF = recedeProgress * dist
		segEndF = dist
	}

	segStart := int(math.Floor(segStartF))
	segEnd := int(math.Floor(segEndF))

	// Render cells across the target edge span
	for cellOffset := segStart; cellOffset <= segEnd; cellOffset++ {
		intensity := r.calcIntensity(mat.Progress, cellOffset, segStart, segEnd)
		for spanPos := spanStart; spanPos <= spanEnd; spanPos++ {
			r.renderBeamCellSpan(ctx, buf, dir, edgePos, cellOffset, spanPos, intensity)
		}
	}
}

func (r *MaterializeRenderer) renderBeamCellSpan(ctx render.RenderContext, buf *render.RenderBuffer, dir beamDir, edgePos, cellOffset, spanPos int, intensity float64) {
	var vx, vy int
	switch dir {
	case dirUp:
		vx = spanPos
		vy = edgePos + cellOffset
	case dirDown:
		vx = spanPos
		vy = edgePos - cellOffset
	case dirLeft:
		vx = edgePos + cellOffset
		vy = spanPos
	case dirRight:
		vx = edgePos - cellOffset
		vy = spanPos
	}

	// Bounds check in viewport space
	if vx < 0 || vx >= ctx.ViewportWidth || vy < 0 || vy >= ctx.ViewportHeight {
		return
	}

	if intensity > 1.0 {
		intensity = 1.0
	}
	if intensity < 0.0 {
		intensity = 0.0
	}

	scaledColor := color.Scale(visual.RgbMaterialize, intensity)
	screenX, screenY := ctx.ViewportToScreen(vx, vy)

	buf.Set(screenX, screenY, 0, visual.RgbBlack, scaledColor, render.BlendMaxBg, 1.0, terminal.AttrNone)
}

// calcIntensity returns normalized intensity for a cell based on phase and position.
func (r *MaterializeRenderer) calcIntensity(progress float64, cellOffset, segStart, segEnd int) float64 {
	if segEnd <= segStart {
		return 1.0
	}

	segLen := segEnd - segStart
	cellPos := cellOffset - segStart // Positions within segment

	switch {
	case progress < matFillEnd:
		// Fill: gradient from dim (edge) to bright (leading edge) + pulse at front
		// Base gradient: cellPos / segLen
		baseIntensity := float64(cellPos) / float64(segLen)

		// Pulse at leading edge (last few cells)
		if cellOffset >= segEnd-2 && segEnd > 0 {
			// Sine pulse: 0.8 + 0.2 * sin(progress * pulseHz * 2π)
			pulseAngle := progress * float64(parameter.MaterializePulseHz) * vmath.TwoPi
			pulseIntensity := 0.8 + vmath.SinF(pulseAngle)/5.0
			return baseIntensity * pulseIntensity
		}
		return baseIntensity

	case progress < matHoldEnd:
		// Hold: max brightness
		return 1.0

	default:
		// Recede: bright at target end, fading toward receding edge
		// Invert: cells closer to target (higher cellPos) stay brighter
		return float64(cellPos) / float64(segLen)
	}
}
