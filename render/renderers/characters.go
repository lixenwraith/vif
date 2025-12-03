package renderers

import (
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// CharactersRenderer draws all character entities
type CharactersRenderer struct {
	gameCtx *engine.GameContext
}

// NewCharactersRenderer creates a new characters renderer
func NewCharactersRenderer(gameCtx *engine.GameContext) *CharactersRenderer {
	return &CharactersRenderer{
		gameCtx: gameCtx,
	}
}

// Render draws all character entities
func (c *CharactersRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	// Query entities with both position and character
	entities := world.Query().
		With(world.Positions).
		With(world.Characters).
		Execute()

	// Iteration over character entities
	for _, entity := range entities {
		pos, okP := world.Positions.Get(entity)
		char, okC := world.Characters.Get(entity)
		if !okP || !okC {
			continue // Entity destroyed mid-iteration
		}

		screenX := ctx.GameX + pos.X
		screenY := ctx.GameY + pos.Y

		if screenX < ctx.GameX || screenX >= ctx.Width || screenY < ctx.GameY || screenY >= ctx.GameY+ctx.GameHeight {
			continue
		}

		// Resolve semantic color to RGB
		fg := resolveCharacterColor(char)
		attrs := resolveTextStyle(char.Style)

		// Apply pause dimming
		if c.gameCtx.IsPaused.Load() {
			fg = render.Scale(fg, 0.7)
		}

		// Characters have NO background - use SetFgOnly to preserve underlying bg
		// Grid (layer 100) already drew backgrounds; we float on top
		buf.SetFgOnly(screenX, screenY, char.Rune, fg, attrs)
	}
}

// resolveCharacterColor maps semantic color info to concrete RGB
func resolveCharacterColor(char components.CharacterComponent) render.RGB {
	// Handle explicit semantic colors first
	switch char.Color {
	case components.ColorNugget:
		return render.RgbNuggetOrange
	case components.ColorDecay:
		return render.RgbDecay
	case components.ColorDrain:
		return render.RgbDrain
	case components.ColorCleaner:
		return render.RgbCleanerBase
	case components.ColorMaterialize:
		return render.RgbMaterialize
	case components.ColorFlash:
		return render.RgbRemovalFlash
	case components.ColorNone, components.ColorNormal:
		// Fall through to sequence-based resolution
	}

	// Default: derive from SeqType + SeqLevel
	return render.GetFgForSequence(char.SeqType, char.SeqLevel)
}

// resolveTextStyle maps semantic style to terminal attributes
func resolveTextStyle(style components.TextStyle) terminal.Attr {
	switch style {
	case components.StyleBold:
		return terminal.AttrBold
	case components.StyleDim:
		return terminal.AttrDim
	case components.StyleUnderline:
		return terminal.AttrUnderline
	case components.StyleBlink:
		return terminal.AttrBlink
	default:
		return terminal.AttrNone
	}
}