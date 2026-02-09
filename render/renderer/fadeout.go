package renderer

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// FadeoutRenderer draws fadeout effects for wall destruction
type FadeoutRenderer struct {
	gameCtx *engine.GameContext
}

func NewFadeoutRenderer(gameCtx *engine.GameContext) *FadeoutRenderer {
	return &FadeoutRenderer{
		gameCtx: gameCtx,
	}
}

func (r *FadeoutRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	entities := r.gameCtx.World.Components.Fadeout.GetAllEntities()
	if len(entities) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	for _, entity := range entities {
		fadeout, ok := r.gameCtx.World.Components.Fadeout.GetComponent(entity)
		if !ok {
			continue
		}

		if fadeout.Remaining <= 0 {
			continue
		}

		// Transform map coords to screen coords with visibility check
		screenX, screenY, visible := ctx.MapToScreen(fadeout.X, fadeout.Y)
		if !visible {
			continue
		}

		// Opacity fades from 1.0 to 0.0 over duration
		opacity := float64(fadeout.Remaining) / float64(fadeout.Duration)
		if opacity < 0.0 {
			opacity = 0.0
		}

		// Scale colors by opacity
		scaledBg := render.Scale(fadeout.BgColor, opacity)

		if fadeout.Char != 0 {
			// Fg + Bg fadeout
			scaledFg := render.Scale(fadeout.FgColor, opacity)
			buf.Set(screenX, screenY, fadeout.Char, scaledFg, scaledBg, render.BlendAlpha, opacity, terminal.AttrNone)
		} else {
			// Bg-only fadeout
			buf.Set(screenX, screenY, 0, visual.RgbBlack, scaledBg, render.BlendAlpha, opacity, terminal.AttrNone)
		}
	}
}