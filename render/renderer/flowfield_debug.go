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
	gameCtx   *engine.GameContext
	flowCache *navigation.FlowFieldCache
}

func NewFlowFieldDebugRenderer(gameCtx *engine.GameContext) *FlowFieldDebugRenderer {
	return &FlowFieldDebugRenderer{
		gameCtx:   gameCtx,
		flowCache: system.DebugFlow,
	}
}

func (r *FlowFieldDebugRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	if !system.DebugShowFlow {
		return
	}

	if r.flowCache == nil || !r.flowCache.IsValid() {
		return
	}

	field := r.flowCache.Field
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
				// Color gradient: near target = bright cyan, far = dim blue
				t := 1.0 - float64(dist)/float64(maxDist)
				fg = terminal.RGB{
					R: uint8(40 + t*60),
					G: uint8(80 + t*175),
					B: uint8(120 + t*135),
				}
			}

			buf.SetFgOnly(screenX, screenY, arrow, fg, terminal.AttrNone)
		}
	}
}

func (r *FlowFieldDebugRenderer) findMaxDistance(field *navigation.FlowField) int {
	maxDist := 0
	for _, d := range field.Distances {
		if d > maxDist {
			maxDist = d
		}
	}
	return maxDist
}