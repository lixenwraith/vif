package renderer

import (
	"math"
	"time"

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
		elapsed := timer.Elapsed(component.TimerDeadline)

		// Transform target area to viewport coords
		targetVX, targetVY, _ := ctx.MapToViewport(mat.TargetX, mat.TargetY)

		style := timedStyle(component.MaterializeStyles[mat.Type], timer.Slots[component.TimerDeadline].Duration)
		c := materializePalette[mat.Type]
		for _, dir := range [...]beamDir{dirUp, dirDown, dirLeft, dirRight} {
			r.renderBeam(ctx, buf, pf, mat, elapsed, style, c, targetVX, targetVY, dir)
		}
		return true
	})
}

func (r *MaterializeRenderer) renderBeam(ctx render.RenderContext, buf *render.RenderBuffer, pf render.Rect, mat *component.MaterializeComponent, elapsed time.Duration, style component.MaterializeStyle, c color.RGB, targetVX, targetVY int, dir beamDir) {
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

	// The lit segment runs from the tail to the head and carries one gradient,
	// dim at the tail and bright at the head, so no phase boundary snaps it
	tail, head := beamSpan(style, elapsed, distance)
	for cellOffset := tail; cellOffset <= head; cellOffset++ {
		intensity := beamIntensity(style, elapsed, cellOffset, tail, head)
		for spanPos := spanStart; spanPos <= spanEnd; spanPos++ {
			r.renderBeamCellSpan(ctx, buf, pf, c, dir, edgePos, cellOffset, spanPos, intensity)
		}
	}
}

// timedStyle spreads a style's fill, hold and recede over d, the duration its
// materialize's timer was armed with, in the style's proportions
func timedStyle(style component.MaterializeStyle, d time.Duration) component.MaterializeStyle {
	total := style.Duration()
	if d <= 0 || d == total {
		return style
	}
	share := float64(d) / float64(total)
	style.Fill = time.Duration(float64(style.Fill) * share)
	style.Hold = time.Duration(float64(style.Hold) * share)
	style.Recede = d - style.Fill - style.Hold
	return style
}

// beamSpan is the lit segment at elapsed, as offsets from the edge: the head runs
// to the target through the fill, both ends rest through the hold, and the tail
// runs after the head through the recede, draining the line into the target
func beamSpan(style component.MaterializeStyle, elapsed time.Duration, distance int) (tail, head int) {
	switch {
	case elapsed < style.Fill:
		return 0, int(math.Floor(float64(distance) * float64(elapsed) / float64(style.Fill)))
	case elapsed < style.Fill+style.Hold || style.Recede <= 0:
		return 0, distance
	}
	receded := float64(elapsed-style.Fill-style.Hold) / float64(style.Recede)
	return min(distance, int(math.Floor(float64(distance)*receded))), distance
}

// beamIntensity grades the lit segment from the tail's floor to the bright head,
// which pulses while the fill advances it
func beamIntensity(style component.MaterializeStyle, elapsed time.Duration, cellOffset, tail, head int) float64 {
	intensity := 1.0
	if head > tail {
		floor := parameter.MaterializeTailIntensity
		intensity = floor + (1-floor)*float64(cellOffset-tail)/float64(head-tail)
	}
	if elapsed < style.Fill && cellOffset >= head-2 {
		pulseAngle := elapsed.Seconds() * parameter.MaterializePulseHz * vmath.TwoPi
		intensity *= 0.8 + vmath.Sin(pulseAngle)/5.0
	}
	return intensity
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
