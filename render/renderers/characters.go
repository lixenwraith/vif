package renderers

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// CharactersRenderer draws all character entities
type CharactersRenderer struct {
	gameCtx   *engine.GameContext
	charStore *engine.Store[component.CharacterComponent]
}

// NewCharactersRenderer creates a new characters renderer
func NewCharactersRenderer(gameCtx *engine.GameContext) *CharactersRenderer {
	return &CharactersRenderer{
		gameCtx:   gameCtx,
		charStore: engine.GetStore[component.CharacterComponent](gameCtx.World),
	}
}

// Render draws all character entities
func (r *CharactersRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(render.MaskEntity)

	// Query entities with both position and character
	entities := r.charStore.All()

	for _, entity := range entities {
		char, ok := r.charStore.Get(entity)
		if !ok {
			continue
		}

		pos, ok := r.gameCtx.World.Positions.Get(entity)
		if !ok {
			continue
		}

		screenX := ctx.GameX + pos.X
		screenY := ctx.GameY + pos.Y

		if screenX < ctx.GameX || screenX >= ctx.Width || screenY < ctx.GameY || screenY >= ctx.GameY+ctx.GameHeight {
			continue
		}

		fg := resolveCharacterColor(char)
		attrs := resolveTextStyle(char.Style)

		buf.SetFgOnly(screenX, screenY, char.Rune, fg, attrs)
	}
}

// resolveCharacterColor maps semantic color info to concrete RGB
func resolveCharacterColor(char component.CharacterComponent) render.RGB {
	// Handle explicit semantic colors first
	switch char.Color {
	case component.ColorNugget:
		return render.RgbNuggetOrange
	case component.ColorDecay:
		return render.RgbDecay
	case component.ColorBlossom:
		return render.RgbBlossom
	case component.ColorDrain:
		return render.RgbDrain
	case component.ColorCleaner:
		return render.RgbCleanerBase
	case component.ColorMaterialize:
		return render.RgbMaterialize
	case component.ColorFlash:
		return render.RgbRemovalFlash
	case component.ColorNone, component.ColorNormal:
		// Fall through to sequence-based resolution
	}

	// Default: derive from SeqType + SeqLevel
	return render.GetFgForSequence(char.SeqType, char.SeqLevel)
}

// resolveTextStyle maps semantic style to terminal attributes
func resolveTextStyle(style component.TextStyle) terminal.Attr {
	switch style {
	case component.StyleBold:
		return terminal.AttrBold
	case component.StyleDim:
		return terminal.AttrDim
	case component.StyleUnderline:
		return terminal.AttrUnderline
	case component.StyleBlink:
		return terminal.AttrBlink
	default:
		return terminal.AttrNone
	}
}