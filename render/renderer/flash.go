package renderer

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// FlashRenderer draws brief destruction flash effects
type FlashRenderer struct {
	gameCtx *engine.GameContext
}

// NewEffectsRenderer creates fg-only effects renderer for flash
func NewFlashRenderer(gameCtx *engine.GameContext) *FlashRenderer {
	return &FlashRenderer{
		gameCtx: gameCtx,
	}
}

// Render draws brief flash effects when characters are removed
func (r *FlashRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	entities := r.gameCtx.World.Components.Flash.GetAllEntities()
	if len(entities) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	for _, entity := range entities {
		flash, ok := r.gameCtx.World.Components.Flash.GetComponent(entity)
		if !ok {
			continue
		}

		if flash.Y < 0 || flash.Y >= ctx.GameHeight || flash.X < 0 || flash.X >= ctx.GameWidth {
			continue
		}

		if flash.Remaining <= 0 {
			continue
		}

		// Opacity fades from 1.0 to 0.0 over duration (bright to transparent)
		opacity := (float64(flash.Remaining) / float64(flash.Duration))
		if opacity < 0.0 {
			opacity = 0.0
		}

		flashColor := render.Scale(visual.RgbRemovalFlash, opacity)

		screenX := ctx.GameXOffset + flash.X
		screenY := ctx.GameYOffset + flash.Y

		// Additive blend on foreground only, preserves background
		buf.Set(screenX, screenY, flash.Char, flashColor, visual.RgbBlack, render.BlendAddFg, 1.0, terminal.AttrNone)
	}
}