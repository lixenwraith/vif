package renderer

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
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
	entities := r.gameCtx.World.Components.Sigil.GetAllEntities()
	if len(entities) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	for _, entity := range entities {
		sigilComp, ok := r.gameCtx.World.Components.Sigil.GetComponent(entity)
		if !ok {
			continue
		}

		sigilPos, ok := r.gameCtx.World.Positions.GetPosition(entity)
		if !ok {
			continue
		}

		screenX := ctx.GameXOffset + sigilPos.X
		screenY := ctx.GameYOffset + sigilPos.Y

		if screenX < ctx.GameXOffset || screenX >= ctx.ScreenWidth ||
			screenY < ctx.GameYOffset || screenY >= ctx.GameYOffset+ctx.GameHeight {
			continue
		}

		fg := resolveSigilColor(sigilComp.Color)
		buf.SetFgOnly(screenX, screenY, sigilComp.Rune, fg, terminal.AttrNone)
	}
}

// resolveSigilColor maps SigilColor toterminal.RGB
func resolveSigilColor(color component.SigilColor) terminal.RGB {
	switch color {
	case component.SigilNugget:
		return visual.RgbNuggetOrange
	case component.SigilDrain:
		return visual.RgbDrain
	case component.SigilBlossom:
		return visual.RgbBlossom
	case component.SigilDecay:
		return visual.RgbDecay
	case component.SigilHitFlash:
		return visual.RgbCombatHitFlash
	case component.SigilEnraged:
		return visual.RgbCombatEnraged
	case component.SigilDustDark:
		return visual.RgbDustDark
	case component.SigilDustNormal:
		return visual.RgbDustNormal
	case component.SigilDustBright:
		return visual.RgbDustBright
	case component.SigilOrbRod:
		return visual.RgbOrbRod
	case component.SigilOrbLauncher:
		return visual.RgbOrbLauncher
	case component.SigilOrbChain:
		return visual.RgbOrbChain
	case component.SigilOrbFlash:
		return visual.RgbOrbFlash
	case component.SigilWall:
		return visual.RgbWhite
	default:
		return visual.RgbBackground
	}
}