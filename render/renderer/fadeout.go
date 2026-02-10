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
	fadeoutEntities := r.gameCtx.World.Components.Fadeout.GetAllEntities()
	if len(fadeoutEntities) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	for _, fadeoutEntity := range fadeoutEntities {
		fadeoutComp, ok := r.gameCtx.World.Components.Fadeout.GetComponent(fadeoutEntity)
		if !ok {
			continue
		}

		if fadeoutComp.Remaining <= 0 {
			continue
		}

		posComp, ok := r.gameCtx.World.Positions.GetPosition(fadeoutEntity)
		if !ok {
			continue
		}

		// Transform map coords to screen coords with visibility check
		screenX, screenY, visible := ctx.MapToScreen(posComp.X, posComp.Y)
		if !visible {
			continue
		}

		// Opacity fades from 1.0 to 0.0 over duration
		opacity := float64(fadeoutComp.Remaining) / float64(fadeoutComp.Duration)
		if opacity < 0.0 {
			opacity = 0.0
		}

		// Scale colors by opacity
		scaledBg := render.Scale(fadeoutComp.BgColor, opacity)

		if fadeoutComp.Char != 0 {
			// Fg + Bg fadeout
			scaledFg := render.Scale(fadeoutComp.FgColor, opacity)
			buf.Set(screenX, screenY, fadeoutComp.Char, scaledFg, scaledBg, render.BlendAlpha, opacity, terminal.AttrNone)
		} else {
			// Bg-only fadeout
			buf.Set(screenX, screenY, 0, visual.RgbBlack, scaledBg, render.BlendAlpha, opacity, terminal.AttrNone)
		}
	}
}
