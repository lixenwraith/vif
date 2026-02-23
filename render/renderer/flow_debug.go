package renderer

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/navigation"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/system"
	"github.com/lixenwraith/vi-fighter/terminal"
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
	// Mode 1: Point entity flow field (existing)
	if system.DebugShowFlow && !system.DebugShowCompositeNav {
		r.renderFlowField(ctx, buf, system.DebugFlow, false)
		return
	}

	// Mode 2: Composite navigation debug
	if system.DebugShowCompositeNav {
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

	for mapY := 0; mapY < field.Height; mapY++ {
		for mapX := 0; mapX < field.Width; mapX++ {
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
			var fg terminal.RGB

			switch {
			case dir == navigation.DirTarget:
				arrow = '●'
				fg = terminal.RGB{R: 255, G: 255, B: 255}
			case dir == navigation.DirNone || dir < 0:
				arrow = '·'
				fg = terminal.RGB{R: 60, G: 60, B: 60}
			default:
				arrow = flowDirArrows[dir]
				t := 1.0 - float64(dist)/float64(maxDist)
				if isComposite {
					// Composite: orange/yellow gradient
					fg = terminal.RGB{
						R: uint8(180 + t*75),
						G: uint8(80 + t*120),
						B: uint8(20 + t*30),
					}
				} else {
					// Point: cyan/blue gradient
					fg = terminal.RGB{
						R: uint8(40 + t*60),
						G: uint8(80 + t*175),
						B: uint8(120 + t*135),
					}
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
	pass := system.DebugCompositePassability
	if pass == nil {
		return
	}

	w, h := pass.GetDimensions()

	for mapY := 0; mapY < h; mapY++ {
		for mapX := 0; mapX < w; mapX++ {
			vx, vy, visible := ctx.MapToViewport(mapX, mapY)
			if !visible {
				continue
			}

			screenX := ctx.GameXOffset + vx
			screenY := ctx.GameYOffset + vy

			if pass.IsValid(mapX, mapY) {
				// Valid: dim green dot
				buf.SetFgOnly(screenX, screenY, '·', terminal.RGB{R: 40, G: 100, B: 40}, terminal.AttrNone)
			} else {
				// Blocked: dim red X
				buf.SetFgOnly(screenX, screenY, '×', terminal.RGB{R: 120, G: 40, B: 40}, terminal.AttrNone)
			}
		}
	}
}

func (r *FlowFieldDebugRenderer) renderCompositeFlowField(ctx render.RenderContext, buf *render.RenderBuffer) {
	cache := system.DebugCompositeFlow
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

	for mapY := 0; mapY < field.Height; mapY++ {
		for mapX := 0; mapX < field.Width; mapX++ {
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
			var fg terminal.RGB

			switch {
			case dir == navigation.DirTarget:
				arrow = '◆'
				fg = terminal.RGB{R: 255, G: 200, B: 50}
			case dir == navigation.DirNone || dir < 0:
				// Blocked in passability but visited? Shouldn't happen
				continue
			default:
				arrow = flowDirArrows[dir]
				t := 1.0 - float64(dist)/float64(maxDist)
				// Orange/yellow for composite flow
				fg = terminal.RGB{
					R: uint8(200 + t*55),
					G: uint8(120 + t*100),
					B: uint8(20 + t*40),
				}
			}

			buf.SetFgOnly(screenX, screenY, arrow, fg, terminal.AttrNone)
		}
	}
}

func (r *FlowFieldDebugRenderer) findMaxDistance(field *navigation.FlowField) int {
	maxDist := 0
	for i, d := range field.Distances {
		// Only count cells from current generation
		if field.VisitedGen[i] == field.CurrentGen && d > maxDist && d < 1<<29 {
			maxDist = d
		}
	}
	return maxDist
}