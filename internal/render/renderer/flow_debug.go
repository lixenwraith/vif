package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/navigation"
)

var flowDirArrows = [8]rune{
	'↑', '↗', '→', '↘', '↓', '↙', '←', '↖',
}

type FlowFieldDebugRenderer struct {
	gameCtx *engine.GameContext
}

func NewFlowFieldDebugRenderer(gameCtx *engine.GameContext) *FlowFieldDebugRenderer {
	return &FlowFieldDebugRenderer{
		gameCtx: gameCtx,
	}
}

func (r *FlowFieldDebugRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	dbg := &r.gameCtx.NavigationDebug
	// Mode 1: Point entity flow field (existing)
	if dbg.ShowFlow && !dbg.ShowComposite {
		r.renderFlowField(ctx, buf, dbg.Flow, false)
		return
	}

	// Mode 2: Composite navigation debug
	if dbg.ShowComposite {
		r.renderCompositeDebug(ctx, buf)
		return
	}
}

func (r *FlowFieldDebugRenderer) renderFlowField(ctx render.RenderContext, buf *render.RenderBuffer, cache *navigation.FlowFieldCache, isComposite bool) {
	if cache == nil || !cache.IsValid() {
		return
	}

	field := cache.Field
	if field == nil || !field.Valid {
		return
	}

	buf.SetWriteMask(visual.MaskUI)

	maxDist := r.findMaxDistance(field)
	if maxDist <= 0 {
		maxDist = 1
	}

	for mapY := range field.Height {
		for mapX := range field.Width {
			vx, vy, visible := ctx.MapToViewport(mapX, mapY)
			if !visible {
				continue
			}

			screenX := ctx.GameXOffset + vx
			screenY := ctx.GameYOffset + vy

			idx := mapY*field.Width + mapX
			dir := field.Directions[idx]
			dist := field.Distances[idx]

			// Check generation validity
			if field.VisitedGen[idx] != field.CurrentGen {
				dir = navigation.DirNone
			}

			var arrow rune
			var fg color.RGB

			switch {
			case dir == navigation.DirTarget:
				arrow = '●'
				fg = visual.RgbFlowTarget
			case dir == navigation.DirNone || dir < 0:
				arrow = '·'
				fg = visual.RgbFlowUnreached
			default:
				arrow = flowDirArrows[dir]
				t := 1.0 - float64(dist)/float64(maxDist)
				if isComposite {
					fg = visual.FlowCompositeGradient.At(t)
				} else {
					fg = visual.FlowPointGradient.At(t)
				}
			}

			buf.SetFgOnly(screenX, screenY, arrow, fg, terminal.AttrNone)
		}
	}
}

func (r *FlowFieldDebugRenderer) renderCompositeDebug(ctx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(visual.MaskUI)

	// Layer 1: Passability grid (background)
	r.renderPassabilityGrid(ctx, buf)

	// Layer 2: Composite flow field (foreground, only where passable)
	r.renderCompositeFlowField(ctx, buf)
}

func (r *FlowFieldDebugRenderer) renderPassabilityGrid(ctx render.RenderContext, buf *render.RenderBuffer) {
	pass := r.gameCtx.NavigationDebug.CompositePassability
	if pass == nil {
		return
	}

	w, h := pass.GetDimensions()

	for mapY := range h {
		for mapX := range w {
			vx, vy, visible := ctx.MapToViewport(mapX, mapY)
			if !visible {
				continue
			}

			screenX := ctx.GameXOffset + vx
			screenY := ctx.GameYOffset + vy

			if pass.IsValid(mapX, mapY) {
				buf.SetFgOnly(screenX, screenY, '·', visual.RgbFlowPassable, terminal.AttrNone)
			} else {
				buf.SetFgOnly(screenX, screenY, '×', visual.RgbFlowBlocked, terminal.AttrNone)
			}
		}
	}
}

func (r *FlowFieldDebugRenderer) renderCompositeFlowField(ctx render.RenderContext, buf *render.RenderBuffer) {
	cache := r.gameCtx.NavigationDebug.CompositeFlow
	if cache == nil || !cache.IsValid() {
		return
	}

	field := cache.Field
	if field == nil || !field.Valid {
		return
	}

	maxDist := r.findMaxDistance(field)
	if maxDist <= 0 {
		maxDist = 1
	}

	for mapY := range field.Height {
		for mapX := range field.Width {
			vx, vy, visible := ctx.MapToViewport(mapX, mapY)
			if !visible {
				continue
			}

			screenX := ctx.GameXOffset + vx
			screenY := ctx.GameYOffset + vy

			idx := mapY*field.Width + mapX

			// Skip cells not visited this generation
			if field.VisitedGen[idx] != field.CurrentGen {
				continue
			}

			dir := field.Directions[idx]
			dist := field.Distances[idx]

			var arrow rune
			var fg color.RGB

			switch {
			case dir == navigation.DirTarget:
				arrow = '◆'
				fg = visual.RgbFlowCompositeTarget
			case dir == navigation.DirNone || dir < 0:
				continue // blocked in passability yet visited: never
			default:
				arrow = flowDirArrows[dir]
				t := 1.0 - float64(dist)/float64(maxDist)
				fg = visual.FlowCompositeFieldGradient.At(t)
			}

			buf.SetFgOnly(screenX, screenY, arrow, fg, terminal.AttrNone)
		}
	}
}

func (r *FlowFieldDebugRenderer) findMaxDistance(field *navigation.FlowField) int {
	maxDist := 0
	for i, d := range field.Distances {
		// Only count cells from current generation
		if field.VisitedGen[i] == field.CurrentGen && int(d) > maxDist && d < 1<<29 {
			maxDist = int(d)
		}
	}
	return maxDist
}
