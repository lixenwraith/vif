package renderers

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// Phase thresholds in Q16.16
var (
	matFillEnd      = vmath.FromFloat(constant.MaterializeFillEnd)
	matHoldEnd      = vmath.FromFloat(constant.MaterializeHoldEnd)
	matRecede       = vmath.Scale - matHoldEnd // Duration of recede phase
	matWidthFalloff = vmath.FromFloat(constant.MaterializeWidthFalloff)
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
	entities := r.gameCtx.World.Component.Materialize.All()
	if len(entities) == 0 {
		return
	}

	buf.SetWriteMask(constant.MaskTransient)

	for _, entity := range entities {
		mat, ok := r.gameCtx.World.Component.Materialize.Get(entity)
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
	// Calculate edge position and distance to target
	var edgePos, distance int
	switch dir {
	case dirUp:
		edgePos = 0
		distance = mat.TargetY
	case dirDown:
		edgePos = ctx.GameHeight - 1
		distance = ctx.GameHeight - 1 - mat.TargetY
	case dirLeft:
		edgePos = 0
		distance = mat.TargetX
	case dirRight:
		edgePos = ctx.GameWidth - 1
		distance = ctx.GameWidth - 1 - mat.TargetX
	}

	if distance <= 0 {
		return // Target at edge, no beam to draw
	}

	distFixed := vmath.FromInt(distance)
	progress := mat.Progress

	// Calculate segment bounds based on phase (Q16.16)
	var segStartFixed, segEndFixed int32

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

	// Render cells in segment
	for cellOffset := segStart; cellOffset <= segEnd; cellOffset++ {
		intensity := r.calcIntensity(mat.Progress, cellOffset, segStart, segEnd, distance)
		r.renderBeamCell(ctx, buf, mat, dir, edgePos, cellOffset, intensity)
	}
}

// calcIntensity returns Q16.16 intensity for a cell based on phase and position
func (r *MaterializeRenderer) calcIntensity(progress int32, cellOffset, segStart, segEnd, distance int) int32 {
	if segEnd <= segStart {
		return vmath.Scale
	}

	segLen := segEnd - segStart
	cellPos := cellOffset - segStart // Position within segment

	switch {
	case progress < matFillEnd:
		// Fill: gradient from dim (edge) to bright (leading edge) + pulse at front
		// Base gradient: cellPos / segLen
		baseIntensity := vmath.Div(vmath.FromInt(cellPos), vmath.FromInt(segLen))

		// Pulse at leading edge (last few cells)
		if cellOffset >= segEnd-2 && segEnd > 0 {
			// Sine pulse: 0.8 + 0.2 * sin(progress * pulseHz * 2π)
			// Approximate with vmath.Sin where angle 0..Scale = 0..2π
			pulseAngle := vmath.Mul(progress, vmath.FromInt(constant.MaterializePulseHz))
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

func (r *MaterializeRenderer) renderBeamCell(ctx render.RenderContext, buf *render.RenderBuffer, mat *component.MaterializeComponent, dir beamDir, edgePos, cellOffset int, intensity int32) {
	// Calculate base cell position
	var baseX, baseY int
	switch dir {
	case dirUp:
		baseX = mat.TargetX
		baseY = edgePos + cellOffset
	case dirDown:
		baseX = mat.TargetX
		baseY = edgePos - cellOffset
	case dirLeft:
		baseX = edgePos + cellOffset
		baseY = mat.TargetY
	case dirRight:
		baseX = edgePos - cellOffset
		baseY = mat.TargetY
	}

	// Render width with orthogonal falloff
	halfWidth := mat.Width / 2
	for offset := -halfWidth; offset <= halfWidth; offset++ {
		var cellX, cellY int
		switch dir {
		case dirUp, dirDown:
			cellX = baseX + offset
			cellY = baseY
		case dirLeft, dirRight:
			cellX = baseX
			cellY = baseY + offset
		}

		// Bounds check
		if cellX < 0 || cellX >= ctx.GameWidth || cellY < 0 || cellY >= ctx.GameHeight {
			continue
		}

		// Apply width falloff for side lines
		cellIntensity := intensity
		if offset != 0 {
			cellIntensity = vmath.Mul(cellIntensity, matWidthFalloff)
		}

		// Convert to float for RGB scaling (final step only)
		intensityFloat := vmath.ToFloat(cellIntensity)
		if intensityFloat > 1.0 {
			intensityFloat = 1.0
		}
		if intensityFloat < 0.0 {
			intensityFloat = 0.0
		}

		scaledColor := render.Scale(render.RgbMaterialize, intensityFloat)

		screenX := ctx.GameX + cellX
		screenY := ctx.GameY + cellY

		buf.Set(screenX, screenY, 0, render.RGBBlack, scaledColor, render.BlendMaxBg, 1.0, terminal.AttrNone)
	}
}