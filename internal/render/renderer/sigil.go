package renderer

import (
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// SigilRenderer draws non-typeable moving entities, including particles.
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
