package renderers

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// SigilRenderer draws non-typeable moving entities (decay, blossom particles)
type SigilRenderer struct {
	gameCtx    *engine.GameContext
	sigilStore *engine.Store[component.SigilComponent]
}

// NewSigilRenderer creates a new sigil renderer
func NewSigilRenderer(gameCtx *engine.GameContext) *SigilRenderer {
	return &SigilRenderer{
		gameCtx:    gameCtx,
		sigilStore: engine.GetStore[component.SigilComponent](gameCtx.World),
	}
}

// Render draws all sigil entities
func (r *SigilRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	entities := r.sigilStore.All()
	if len(entities) == 0 {
		return
	}

	buf.SetWriteMask(constant.MaskTransient)

	for _, entity := range entities {
		sigil, ok := r.sigilStore.Get(entity)
		if !ok {
			continue
		}

		pos, ok := r.gameCtx.World.Positions.Get(entity)
		if !ok {
			continue
		}

		screenX := ctx.GameX + pos.X
		screenY := ctx.GameY + pos.Y

		if screenX < ctx.GameX || screenX >= ctx.Width ||
			screenY < ctx.GameY || screenY >= ctx.GameY+ctx.GameHeight {
			continue
		}

		fg := resolveSigilColor(sigil.Color)
		buf.SetFgOnly(screenX, screenY, sigil.Rune, fg, terminal.AttrNone)
	}
}

// resolveSigilColor maps SigilColor to RGB
func resolveSigilColor(color component.SigilColor) render.RGB {
	switch color {
	case component.SigilDrain:
		return render.RgbDrain
	case component.SigilBlossom:
		return render.RgbBlossom
	case component.SigilDecay:
		return render.RgbDecay
	case component.SigilCleaner:
		return render.RgbCleanerBasePositive
	default:
		return render.RgbBackground
	}
}