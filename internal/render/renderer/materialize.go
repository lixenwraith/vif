package renderer

import (
	"math"

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

// A beacon's beams take the nugget's own colour; a spawn's keep the materialize cyan
var materializePalette = [component.SpawnTypeCount]color.RGB{
	component.SpawnTypeDrain:  visual.RgbMaterialize,
	component.SpawnTypeSwarm:  visual.RgbMaterialize,
	component.SpawnTypeNugget: visual.RgbMaterializeNugget,
}

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

	// Beams run from the edge of the map, not the edge of the terminal: on a
	// viewport wider than the map the two differ by the centring margin, and a
	// beam starting there starts outside the world.
	pf := ctx.PlayfieldViewportRect()
	if pf.Empty() {
		return
	}

	timers := r.gameCtx.World.Components.Timer
	materializes.Each(func(e core.Entity, mat *component.MaterializeComponent) bool {
		timer, ok := timers.GetPtr(e)
		if !ok || mat.Type < 0 || mat.Type >= component.SpawnTypeCount {
			return true
		}
		progress := timer.Progress(component.TimerDeadline)

		// Transform target area to viewport coords
		targetVX, targetVY, _ := ctx.MapToViewport(mat.TargetX, mat.TargetY)

		style, c := component.MaterializeStyles[mat.Type], materializePalette[mat.Type]
		for _, dir := range [...]beamDir{dirUp, dirDown, dirLeft, dirRight} {
			r.renderBeam(ctx, buf, pf, mat, progress, style, c, targetVX, targetVY, dir)
		}
		return true
	})
}

func (r *MaterializeRenderer) renderBeam(ctx render.RenderContext, buf *render.RenderBuffer, pf render.Rect, mat *component.MaterializeComponent, progress float64, style component.MaterializeStyle, c color.RGB, targetVX, targetVY int, dir beamDir) {
	var edgePos, distance int
	var spanStart, spanEnd int // Range along the target edge

	switch dir {
	case dirUp:
		edgePos = pf.Y0
		distance = targetVY - pf.Y0
		spanStart = targetVX
		spanEnd = targetVX + mat.AreaWidth - 1
	case dirDown:
		edgePos = pf.Y1 - 1
		targetBottom := targetVY + mat.AreaHeight - 1
		distance = pf.Y1 - 1 - targetBottom
		spanStart = targetVX
		spanEnd = targetVX + mat.AreaWidth - 1
	case dirLeft:
		edgePos = pf.X0
		distance = targetVX - pf.X0
		spanStart = targetVY
		spanEnd = targetVY + mat.AreaHeight - 1
	case dirRight:
		edgePos = pf.X1 - 1
		targetRight := targetVX + mat.AreaWidth - 1
		distance = pf.X1 - 1 - targetRight
		spanStart = targetVY
		spanEnd = targetVY + mat.AreaHeight - 1
	}

	if distance <= 0 {
		return // Target at edge, no beam to draw
	}

	// The fill runs from the edge to its front; the hold and the recede keep the
	// whole line, the recede fading it in place. A dark front running from the
	// edge to the target read as a second, faster pass.
	segEnd := distance
	if progress < style.FillEnd {
		segEnd = int(math.Floor(progress / style.FillEnd * float64(distance)))
	}

	// Render cells across the target edge span
	for cellOffset := 0; cellOffset <= segEnd; cellOffset++ {
		intensity := r.calcIntensity(style, progress, cellOffset, segEnd)
		for spanPos := spanStart; spanPos <= spanEnd; spanPos++ {
			r.renderBeamCellSpan(ctx, buf, pf, c, dir, edgePos, cellOffset, spanPos, intensity)
		}
	}
}

func (r *MaterializeRenderer) renderBeamCellSpan(ctx render.RenderContext, buf *render.RenderBuffer, pf render.Rect, c color.RGB, dir beamDir, edgePos, cellOffset, spanPos int, intensity float64) {
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

	// Bounds check against the visible map, in viewport space
	if !pf.Contains(vx, vy) {
		return
	}

	if intensity > 1.0 {
		intensity = 1.0
	}
	if intensity < 0.0 {
		intensity = 0.0
	}

	scaledColor := color.Scale(c, intensity)
	screenX, screenY := ctx.ViewportToScreen(vx, vy)

	buf.Set(screenX, screenY, 0, visual.RgbBlack, scaledColor, render.BlendMaxBg, 1.0, terminal.AttrNone)
}

// calcIntensity returns normalized intensity for a cell based on phase and position.
func (r *MaterializeRenderer) calcIntensity(style component.MaterializeStyle, progress float64, cellOffset, segEnd int) float64 {
	switch {
	case progress < style.FillEnd:
		// Fill: gradient from dim (edge) to bright (leading edge) + pulse at front
		if segEnd <= 0 {
			return 1.0
		}
		baseIntensity := float64(cellOffset) / float64(segEnd)
		if cellOffset >= segEnd-2 {
			pulseAngle := progress * float64(parameter.MaterializePulseHz) * vmath.TwoPi
			return baseIntensity * (0.8 + vmath.Sin(pulseAngle)/5.0)
		}
		return baseIntensity

	case progress < style.HoldEnd:
		return 1.0

	default:
		// Recede: the whole line dims with what is left of the phase
		recede := 1.0 - style.HoldEnd
		if recede <= 0 {
			return 0
		}
		return (1.0 - progress) / recede
	}
}
