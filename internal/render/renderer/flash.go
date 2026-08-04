package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
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
	flashes := r.gameCtx.World.Components.Flash
	if flashes.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	flashes.Each(func(flashEntity core.Entity, flashComp *component.FlashComponent) bool {
		if flashComp.Remaining <= 0 {
			return true
		}

		posComp, ok := r.gameCtx.World.Positions.GetPosition(flashEntity)
		if !ok {
			return true
		}

		// Transform map coords to screen coords with visibility check
		screenX, screenY, visible := ctx.MapToScreen(posComp.X, posComp.Y)
		if !visible {
			return true
		}

		// Opacity fades from 1.0 to 0.0 over duration (bright to transparent)
		opacity := (float64(flashComp.Remaining) / float64(flashComp.Duration))
		if opacity < 0.0 {
			opacity = 0.0
		}

		flashColor := color.Scale(visual.RgbRemovalFlash, opacity)

		// Additive blend on foreground only, preserves background
		buf.Set(screenX, screenY, flashComp.Rune, flashColor, visual.RgbBlack, render.BlendAddFg, 1.0, terminal.AttrNone)
		return true
	})
}
