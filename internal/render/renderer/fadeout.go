package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// FadeoutRenderer draws the cells fading out
type FadeoutRenderer struct {
	gameCtx *engine.GameContext
}

func NewFadeoutRenderer(gameCtx *engine.GameContext) *FadeoutRenderer {
	return &FadeoutRenderer{
		gameCtx: gameCtx,
	}
}

// Render draws each batch's cells in their own colors, fading as one
func (r *FadeoutRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	world := r.gameCtx.World
	fadeouts := world.Components.Fadeout
	if fadeouts.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	fadeouts.Each(func(e core.Entity, fadeout *component.FadeoutComponent) bool {
		progress, ok := deadlineProgress(ctx, world, e)
		if !ok {
			return true
		}
		opacity := 1 - progress
		for i := range fadeout.Cells {
			cell := &fadeout.Cells[i]
			screenX, screenY, visible := ctx.MapToScreen(cell.X, cell.Y)
			if !visible {
				continue
			}
			fg, bg := color.Scale(cell.Fg, opacity), color.Scale(cell.Bg, opacity)
			switch {
			case !cell.Solid:
				// A glyph alone: additive on the foreground, which keeps the cell's background
				buf.Set(screenX, screenY, cell.Rune, fg, visual.RgbBlack, render.BlendAddFg, 1.0, terminal.AttrNone)
			case cell.Rune != 0:
				buf.Set(screenX, screenY, cell.Rune, fg, bg, render.BlendAlpha, opacity, terminal.AttrNone)
			default:
				buf.Set(screenX, screenY, 0, visual.RgbBlack, bg, render.BlendAlpha, opacity, terminal.AttrNone)
			}
		}
		return true
	})
}
