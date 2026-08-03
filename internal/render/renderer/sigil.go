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
	// entities := r.gameCtx.World.Components.Sigil.GetAllEntities()
	// if len(entities) == 0 {
	// 	return
	// }

	sigils := r.gameCtx.World.Components.Sigil
	if sigils.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	// for _, entity := range entities {
	// 	sigilComp, ok := r.gameCtx.World.Components.Sigil.GetComponent(entity)
	// 	if !ok {
	// 		continue
	// 	}
	//
	// 	sigilPos, ok := r.gameCtx.World.Positions.GetPosition(entity)
	// 	if !ok {
	// 		continue
	// 	}
	//
	// 	screenX, screenY, visible := ctx.MapToScreen(sigilPos.X, sigilPos.Y)
	// 	if !visible {
	// 		continue
	// 	}
	//
	// 	fg := sigilComp.Color
	// 	buf.SetFgOnly(screenX, screenY, sigilComp.Rune, fg, terminal.AttrNone)
	// }

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

