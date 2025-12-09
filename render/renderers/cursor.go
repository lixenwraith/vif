package renderers

import (
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// CursorRenderer draws the cursor with complex entity overlap handling
type CursorRenderer struct {
	gameCtx *engine.GameContext
}

// NewCursorRenderer creates a new cursor renderer
func NewCursorRenderer(gameCtx *engine.GameContext) *CursorRenderer {
	return &CursorRenderer{
		gameCtx: gameCtx,
	}
}

// IsVisible returns true when the cursor should be rendered
func (c *CursorRenderer) IsVisible() bool {
	return !c.gameCtx.IsSearchMode() && !c.gameCtx.IsCommandMode()
}

// Render draws the cursor
func (c *CursorRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	buf.SetWriteMask(render.MaskUI)
	screenX := ctx.GameX + ctx.CursorX
	screenY := ctx.GameY + ctx.CursorY

	// Bounds check
	if screenX < ctx.GameX || screenX >= ctx.Width || screenY < ctx.GameY || screenY >= ctx.GameY+ctx.GameHeight {
		return
	}

	// 1. Determine Default State (Empty Cell)
	var charAtCursor = ' '
	var cursorBgColor render.RGB

	// Default background based on mode
	if c.gameCtx.IsInsertMode() {
		cursorBgColor = render.RgbCursorInsert
	} else {
		cursorBgColor = render.RgbCursorNormal
	}

	var charFgColor = render.RgbBlack

	// 2. Get entities at cursor position using z-index selection

	// Get all entities at cursor position
	entities := world.Positions.GetAllAt(ctx.CursorX, ctx.CursorY)

	// Check for Drain (highest priority overlay, masks everything)
	isDrain := false
	for _, e := range entities {
		if world.Drains.Has(e) {
			isDrain = true
			break
		}
	}

	// Get the highest priority character entity for display (excluding cursor and non-character entities)
	displayEntity := engine.SelectTopEntityFiltered(entities, world, func(e engine.Entity) bool {
		// Exclude cursor itself and non-character entities
		if e == c.gameCtx.CursorEntity {
			return false
		}
		// Only consider entities with characters
		return world.Characters.Has(e)
	})

	hasChar := displayEntity != 0
	isNugget := false
	var charFg render.RGB

	if hasChar {
		if charComp, ok := world.Characters.Get(displayEntity); ok {
			charAtCursor = charComp.Rune
			// Resolve color from semantic fields
			charFg = resolveCharacterColor(charComp)
			if world.Nuggets.Has(displayEntity) {
				isNugget = true
			}
		}
	}

	// Priority 3: Decay (Lowest Priority)
	hasDecay := false
	if !isDrain && !hasChar {
		for _, e := range entities {
			if decay, ok := world.Decays.Get(e); ok {
				charAtCursor = decay.Char
				hasDecay = true
				break
			}
		}
	}

	// 3. Resolve Final Visuals based on priority
	if isDrain {
		// Drain overrides everything
		charAtCursor = constants.DrainChar
		cursorBgColor = render.RgbDrain
		charFgColor = render.RgbBlack
	} else if hasChar {
		// Character found - inherit background from character's foreground color
		cursorBgColor = charFg
		if isNugget {
			charFgColor = render.RgbNuggetDark
		} else {
			charFgColor = render.RgbBlack
		}
	} else if hasDecay {
		// Decay found on empty space
		cursorBgColor = render.RgbDecay
		charFgColor = render.RgbBlack
	}

	// 4. Error Flash Overlay (Absolute Highest Priority for Background)
	// Reads component directly to ensure flash works during pause
	cursorComp, ok := world.Cursors.Get(c.gameCtx.CursorEntity)
	if ok && cursorComp.ErrorFlashRemaining > 0 {
		if c.gameCtx.PausableClock.Now().UnixNano() < cursorComp.ErrorFlashRemaining.Nanoseconds() {
			cursorBgColor = render.RgbCursorError
			charFgColor = render.RgbBlack
		}
	}

	// 5. Render
	buf.SetWithBg(screenX, screenY, charAtCursor, charFgColor, cursorBgColor)
}