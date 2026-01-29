package renderer

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// MarkerRenderer draws visual area indicators
type MarkerRenderer struct {
	gameCtx *engine.GameContext
}

func NewMarkerRenderer(ctx *engine.GameContext) *MarkerRenderer {
	return &MarkerRenderer{gameCtx: ctx}
}

func (r *MarkerRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	entities := r.gameCtx.World.Components.Marker.GetAllEntities()
	if len(entities) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	for _, entity := range entities {
		marker, ok := r.gameCtx.World.Components.Marker.GetComponent(entity)
		if !ok {
			continue
		}

		switch marker.Shape {
		case component.MarkerShapeNone:
			// Invisible - no rendering
			continue
		case component.MarkerShapeRectangle:
			r.renderRectangle(ctx, buf, &marker)
		case component.MarkerShapeInvert:
			r.renderInvert(ctx, buf, &marker)
		}
	}
}

func (r *MarkerRenderer) renderRectangle(ctx render.RenderContext, buf *render.RenderBuffer, marker *component.MarkerComponent) {
	alpha := vmath.ToFloat(marker.Intensity)
	if alpha <= 0 {
		return
	}
	if alpha > 1.0 {
		alpha = 1.0
	}

	for dy := 0; dy < marker.Height; dy++ {
		for dx := 0; dx < marker.Width; dx++ {
			cellX := marker.X + dx
			cellY := marker.Y + dy

			if cellX < 0 || cellX >= ctx.GameWidth || cellY < 0 || cellY >= ctx.GameHeight {
				continue
			}

			screenX := ctx.GameXOffset + cellX
			screenY := ctx.GameYOffset + cellY

			buf.Set(screenX, screenY, 0, visual.RgbBlack, marker.Color, render.BlendMaxBg, alpha, 0)
		}
	}
}

func (r *MarkerRenderer) renderInvert(ctx render.RenderContext, buf *render.RenderBuffer, marker *component.MarkerComponent) {
	// Stub: character inversion for vim motion highlights
	// Implementation requires reading existing cell fg/bg and swapping
	// Deferred until buffer supports read-back or pre-render pass
}