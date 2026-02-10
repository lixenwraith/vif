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
	flashEntities := r.gameCtx.World.Components.Flash.GetAllEntities()
	if len(flashEntities) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	for _, flashEntity := range flashEntities {
		flashComp, ok := r.gameCtx.World.Components.Flash.GetComponent(flashEntity)
		if !ok {
			continue
		}

		if flashComp.Remaining <= 0 {
			continue
		}

		posComp, ok := r.gameCtx.World.Positions.GetPosition(flashEntity)
		if !ok {
			continue
		}

		// Transform map coords to screen coords with visibility check
		screenX, screenY, visible := ctx.MapToScreen(posComp.X, posComp.Y)
		if !visible {
			continue
		}

		// Opacity fades from 1.0 to 0.0 over duration (bright to transparent)
		opacity := (float64(flashComp.Remaining) / float64(flashComp.Duration))
		if opacity < 0.0 {
			opacity = 0.0
		}

		flashColor := render.Scale(visual.RgbRemovalFlash, opacity)

		// Additive blend on foreground only, preserves background
		buf.Set(screenX, screenY, flashComp.Rune, flashColor, visual.RgbBlack, render.BlendAddFg, 1.0, terminal.AttrNone)
	}
}
