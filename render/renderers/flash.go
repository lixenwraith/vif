package renderers

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// FlashRenderer draws brief destruction flash effects
type FlashRenderer struct {
	gameCtx    *engine.GameContext
	flashStore *engine.Store[component.FlashComponent]
}

// NewEffectsRenderer creates fg-only effects renderer for flash
func NewFlashRenderer(gameCtx *engine.GameContext) *FlashRenderer {
	return &FlashRenderer{
		gameCtx:    gameCtx,
		flashStore: engine.GetStore[component.FlashComponent](gameCtx.World),
	}
}

// Render draws brief flash effects when characters are removed
func (r *FlashRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	entities := r.flashStore.All()
	if len(entities) == 0 {
		return
	}

	buf.SetWriteMask(constant.MaskTransient)

	for _, entity := range entities {
		flash, ok := r.flashStore.Get(entity)
		if !ok {
			continue
		}

		if flash.Y < 0 || flash.Y >= ctx.GameHeight || flash.X < 0 || flash.X >= ctx.GameWidth {
			continue
		}

		if flash.Remaining <= 0 {
			continue
		}

		// Opacity fades from 0.0 to 1.0 over duration (transparent to bright)
		opacity := 1.0 - (float64(flash.Remaining) / float64(flash.Duration))
		if opacity < 0.0 {
			opacity = 0.0
		}

		flashColor := render.Scale(render.RgbRemovalFlash, opacity)

		screenX := ctx.GameX + flash.X
		screenY := ctx.GameY + flash.Y

		// Additive blend on foreground only, preserves background
		buf.Set(screenX, screenY, flash.Char, flashColor, render.RGBBlack, render.BlendAddFg, 1.0, terminal.AttrNone)
	}
}