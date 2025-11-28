package renderers

import (
	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// CharactersRenderer draws all character entities.
type CharactersRenderer struct {
	gameCtx *engine.GameContext
}

// NewCharactersRenderer creates a new characters renderer.
func NewCharactersRenderer(gameCtx *engine.GameContext) *CharactersRenderer {
	return &CharactersRenderer{
		gameCtx: gameCtx,
	}
}

// Render draws all character entities.
func (c *CharactersRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	defaultStyle := tcell.StyleDefault.Background(render.RgbBackground)

	// Get ping color based on mode
	pingColor := c.getPingColor()

	// Query entities with both position and character
	entities := world.Query().
		With(world.Positions).
		With(world.Characters).
		Execute()

	// Direct iteration
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

		// Get existing content to preserve background (e.g., Shield color)
		_, bg, _ := buf.DecomposeAt(screenX, screenY)

		// Handle default background case
		if bg == tcell.ColorDefault {
			bg = render.RgbBackground
		}

		// Extract foreground from character's defined style
		fg, _, attrs := char.Style.Decompose()

		// Check if character is on a ping line (cursor row or column)
		onPingLine := (pos.Y == ctx.CursorY) || (pos.X == ctx.CursorX)

		// Also check if on ping grid lines when ping is active
		if !onPingLine && c.gameCtx.GetPingActive() {
			// Check if on vertical grid line (columns at ±5, ±10, ±15, etc.)
			deltaX := pos.X - ctx.CursorX
			if deltaX%5 == 0 && deltaX != 0 {
				onPingLine = true
			}
			// Check if on horizontal grid line (rows at ±5, ±10, ±15, etc.)
			deltaY := pos.Y - ctx.CursorY
			if deltaY%5 == 0 && deltaY != 0 {
				onPingLine = true
			}
		}

		// Logic to determine final background:
		// 1. Existing background takes priority (Shield, etc.)
		// 2. If existing is default black AND on ping line -> Ping Color
		// 3. Otherwise -> Preserve existing background
		finalBg := bg
		if onPingLine && bg == render.RgbBackground {
			finalBg = pingColor
		}

		finalStyle := defaultStyle.Foreground(fg).Background(finalBg).Attributes(attrs)

		// Apply dimming effect when paused
		if c.gameCtx.IsPaused.Load() {
			red, green, blue := fg.RGB()
			dimmedR := int32(float64(red) * 0.7)
			dimmedG := int32(float64(green) * 0.7)
			dimmedB := int32(float64(blue) * 0.7)
			dimmedFg := tcell.NewRGBColor(dimmedR, dimmedG, dimmedB)
			finalStyle = tcell.StyleDefault.Foreground(dimmedFg).Background(finalBg).Attributes(attrs)
		}

		buf.Set(screenX, screenY, char.Rune, finalStyle)
	}
}

// getPingColor determines the ping highlight color based on game mode.
func (c *CharactersRenderer) getPingColor() tcell.Color {
	// INSERT mode: use whitespace color (dark gray)
	// NORMAL/SEARCH mode: use character color (almost black)
	if c.gameCtx.IsInsertMode() {
		return render.RgbPingHighlight // Dark gray (50,50,50)
	}
	return render.RgbPingNormal // Almost black for NORMAL and SEARCH modes
}
