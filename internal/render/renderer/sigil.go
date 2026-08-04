package renderer

import (
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
)

// SigilRenderer draws non-typeable moving entities (decay, blossom particles)
type SigilRenderer struct {
	gameCtx *engine.GameContext
}

// NewSigilRenderer creates a new sigil renderer
func NewSigilRenderer(gameCtx *engine.GameContext) *SigilRenderer {
	return &SigilRenderer{
		gameCtx: gameCtx,
	}
}

// Render draws all sigil entities
func (r *SigilRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	sigils := r.gameCtx.World.Components.Sigil
	if sigils.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	sigils.Each(func(entity core.Entity, sigilComp *component.SigilComponent) bool {
		sigilPos, ok := r.gameCtx.World.Positions.GetPosition(entity)
		if !ok {
			return true
		}
		screenX, screenY, visible := ctx.MapToScreen(sigilPos.X, sigilPos.Y)
		if !visible {
			return true
		}
		buf.SetFgOnly(screenX, screenY, sigilComp.Rune, sigilComp.Color, terminal.AttrNone)
		return true
	})
}
