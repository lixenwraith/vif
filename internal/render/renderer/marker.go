package renderer

import (
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// MarkerRenderer draws visual area indicators
type MarkerRenderer struct {
	gameCtx *engine.GameContext
}

func NewMarkerRenderer(ctx *engine.GameContext) *MarkerRenderer {
	return &MarkerRenderer{gameCtx: ctx}
}

func (r *MarkerRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	markers := r.gameCtx.World.Components.Marker
	if markers.CountEntities() == 0 {
		return
	}

	markers.Each(func(_ core.Entity, marker *component.MarkerComponent) bool {
		switch marker.Shape {
		case component.MarkerShapeNone:
			// Invisible - no rendering
			return true
		case component.MarkerShapeRectangle:
			buf.SetWriteMask(visual.MaskTransient)
			r.renderRectangle(ctx, buf, marker)
		case component.MarkerShapeInvert:
			buf.SetWriteMask(visual.MaskUI) // Motion markers render above splash
			r.renderInvert(ctx, buf, marker)
		}
		return true
	})
}

func (r *MarkerRenderer) renderRectangle(ctx render.RenderContext, buf *render.RenderBuffer, marker *component.MarkerComponent) {
	alpha := vmath.ToFloat(marker.Intensity)
	if alpha <= 0 {
		return
	}
	if alpha > 1.0 {
		alpha = 1.0
	}

	for dy := range marker.Height {
		for dx := range marker.Width {
			mapX := marker.X + dx
			mapY := marker.Y + dy

			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			buf.Set(screenX, screenY, 0, visual.RgbBlack, marker.Color, render.BlendMaxBg, alpha, 0)
		}
	}
}

func (r *MarkerRenderer) renderInvert(ctx render.RenderContext, buf *render.RenderBuffer, marker *component.MarkerComponent) {
	if marker.Intensity <= 0 {
		return
	}

	var entitiesBuf [parameter.MaxEntitiesPerCell]core.Entity
	for dy := range marker.Height {
		for dx := range marker.Width {
			mapX := marker.X + dx
			mapY := marker.Y + dy

			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			count := r.gameCtx.World.Positions.GetAllEntitiesAtInto(mapX, mapY, entitiesBuf[:])

			var char rune
			var fg, bg = visual.RgbWhite, visual.RgbBackground

			for i := range count {
				e := entitiesBuf[i]
				if e == 0 {
					continue
				}

				// Check glyph first
				if glyph, ok := r.gameCtx.World.Components.Glyph.GetPtr(e); ok {
					char = glyph.Rune
					fg = visual.GlyphColorLUT[glyph.Type][glyph.Level]
					break
				}

				// Fallback to sigil
				if sigil, ok := r.gameCtx.World.Components.Sigil.GetPtr(e); ok {
					char = sigil.Rune
					fg = sigil.Color
					break
				}
			}

			if char == 0 {
				char = ' '
			}

			// Invert: swap fg/bg
			buf.SetWithBg(screenX, screenY, char, bg, fg)
		}
	}
}
