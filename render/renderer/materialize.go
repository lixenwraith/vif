package renderer

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// Phase thresholds in Q32.32
var (
	matFillEnd      = vmath.FromFloat(parameter.MaterializeFillEnd)
	matHoldEnd      = vmath.FromFloat(parameter.MaterializeHoldEnd)
	matRecede       = vmath.Scale - matHoldEnd // Duration of recede phase
	matWidthFalloff = vmath.FromFloat(parameter.MaterializeWidthFalloff)
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
	entities := r.gameCtx.World.Components.Materialize.GetAllEntities()
	if len(entities) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	for _, entity := range entities {
		mat, ok := r.gameCtx.World.Components.Materialize.GetComponent(entity)
		if !ok {
			continue
		}

		r.renderBeam(ctx, buf, &mat, dirUp)
		r.renderBeam(ctx, buf, &mat, dirDown)
		r.renderBeam(ctx, buf, &mat, dirLeft)
		r.renderBeam(ctx, buf, &mat, dirRight)
	}
}

func (r *MaterializeRenderer) renderBeam(ctx render.RenderContext, buf *render.RenderBuffer, mat *component.MaterializeComponent, dir beamDir) {
	var edgePos, distance int
	var spanStart, spanEnd int // Range along the target edge

	switch dir {
	case dirUp:
		edgePos = 0
		distance = mat.TargetY
		spanStart = mat.TargetX
		spanEnd = mat.TargetX + mat.AreaWidth - 1
	case dirDown:
		edgePos = ctx.GameHeight - 1
		targetBottom := mat.TargetY + mat.AreaHeight - 1
		distance = ctx.GameHeight - 1 - targetBottom
		spanStart = mat.TargetX
		spanEnd = mat.TargetX + mat.AreaWidth - 1
	case dirLeft:
		edgePos = 0
		distance = mat.TargetX
		spanStart = mat.TargetY
		spanEnd = mat.TargetY + mat.AreaHeight - 1
	case dirRight:
		edgePos = ctx.GameWidth - 1
		targetRight := mat.TargetX + mat.AreaWidth - 1
		distance = ctx.GameWidth - 1 - targetRight
		spanStart = mat.TargetY
		spanEnd = mat.TargetY + mat.AreaHeight - 1
	}

	if distance <= 0 {
		return // Target at edge, no beam to draw
	}

	distFixed := vmath.FromInt(distance)
	progress := mat.Progress

	// Calculate segment bounds based on phase (Q32.32)
	var segStartFixed, segEndFixed int64

	switch {
	case progress < matFillEnd:
		// Fill: edge to leading edge
		// fillProgress = progress / fillEnd (normalized within fill phase)
		fillProgress := vmath.Div(progress, matFillEnd)
		segStartFixed = 0
		segEndFixed = vmath.Mul(fillProgress, distFixed)

	case progress < matHoldEnd:
		// Hold: full line
		segStartFixed = 0
		segEndFixed = distFixed

	default:
		// Recede: darkness from edge toward target
		// recedeProgress = (progress - holdEnd) / (1.0 - holdEnd)
		recedeProgress := vmath.Div(progress-matHoldEnd, matRecede)
		segStartFixed = vmath.Mul(recedeProgress, distFixed)
		segEndFixed = distFixed
	}

	segStart := vmath.ToInt(segStartFixed)
	segEnd := vmath.ToInt(segEndFixed)

	// Render cells across the target edge span
	for cellOffset := segStart; cellOffset <= segEnd; cellOffset++ {
		intensity := r.calcIntensity(mat.Progress, cellOffset, segStart, segEnd)
		for spanPos := spanStart; spanPos <= spanEnd; spanPos++ {
			r.renderBeamCellSpan(ctx, buf, mat, dir, edgePos, cellOffset, spanPos, intensity)
		}
	}
}

func (r *MaterializeRenderer) renderBeamCellSpan(ctx render.RenderContext, buf *render.RenderBuffer, mat *component.MaterializeComponent, dir beamDir, edgePos, cellOffset, spanPos int, intensity int64) {
	var cellX, cellY int
	switch dir {
	case dirUp:
		cellX = spanPos
		cellY = edgePos + cellOffset
	case dirDown:
		cellX = spanPos
		cellY = edgePos - cellOffset
	case dirLeft:
		cellX = edgePos + cellOffset
		cellY = spanPos
	case dirRight:
		cellX = edgePos - cellOffset
		cellY = spanPos
	}

	if cellX < 0 || cellX >= ctx.GameWidth || cellY < 0 || cellY >= ctx.GameHeight {
		return
	}

	intensityFloat := vmath.ToFloat(intensity)
	if intensityFloat > 1.0 {
		intensityFloat = 1.0
	}
	if intensityFloat < 0.0 {
		intensityFloat = 0.0
	}

	scaledColor := render.Scale(visual.RgbMaterialize, intensityFloat)
	screenX := ctx.GameXOffset + cellX
	screenY := ctx.GameYOffset + cellY

	buf.Set(screenX, screenY, 0, visual.RgbBlack, scaledColor, render.BlendMaxBg, 1.0, terminal.AttrNone)
}

// calcIntensity returns Q32.32 intensity for a cell based on phase and position
func (r *MaterializeRenderer) calcIntensity(progress int64, cellOffset, segStart, segEnd int) int64 {
	if segEnd <= segStart {
		return vmath.Scale
	}

	segLen := segEnd - segStart
	cellPos := cellOffset - segStart // Positions within segment

	switch {
	case progress < matFillEnd:
		// Fill: gradient from dim (edge) to bright (leading edge) + pulse at front
		// Base gradient: cellPos / segLen
		baseIntensity := vmath.Div(vmath.FromInt(cellPos), vmath.FromInt(segLen))

		// Pulse at leading edge (last few cells)
		if cellOffset >= segEnd-2 && segEnd > 0 {
			// Sine pulse: 0.8 + 0.2 * sin(progress * pulseHz * 2π)
			// Approximate with vmath.Sin where angle 0..Scale = 0..2π
			pulseAngle := vmath.Mul(progress, vmath.FromInt(parameter.MaterializePulseHz))
			pulseMod := vmath.Sin(pulseAngle) // -Scale to +Scale
			pulseIntensity := vmath.Scale - vmath.FromFloat(0.2) + vmath.Div(pulseMod, vmath.FromInt(5))
			return vmath.Mul(baseIntensity, pulseIntensity)
		}
		return baseIntensity

	case progress < matHoldEnd:
		// Hold: max brightness
		return vmath.Scale

	default:
		// Recede: bright at target end, fading toward receding edge
		// Invert: cells closer to target (higher cellPos) stay brighter
		return vmath.Div(vmath.FromInt(cellPos), vmath.FromInt(segLen))
	}
}