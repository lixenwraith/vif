package renderers

import (
	"github.com/gdamore/tcell/v2"
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
	defaultStyle := tcell.StyleDefault.Background(render.RgbBackground)

	screenX := ctx.GameX + ctx.CursorX
	screenY := ctx.GameY + ctx.CursorY

	// Bounds check
	if screenX < ctx.GameX || screenX >= ctx.Width || screenY < ctx.GameY || screenY >= ctx.GameY+ctx.GameHeight {
		return
	}

	// 1. Determine Default State (Empty Cell)
	var charAtCursor = ' '
	var cursorBgColor tcell.Color

	// Default background based on mode
	if c.gameCtx.IsInsertMode() {
		cursorBgColor = render.RgbCursorInsert
	} else {
		cursorBgColor = render.RgbCursorNormal
	}

	var charFgColor tcell.Color = tcell.ColorBlack

	// 2. Get entities at cursor position using z-index selection

	// Get all entities at cursor position
	entities := world.Positions.GetAllEntitiesAt(ctx.CursorX, ctx.CursorY)

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
	var charStyle tcell.Style

	if hasChar {
		if charComp, ok := world.Characters.Get(displayEntity); ok {
			charAtCursor = charComp.Rune
			charStyle = charComp.Style
			if world.Nuggets.Has(displayEntity) {
				isNugget = true
			}
		}
	}

	// Priority 3: Decay (Lowest Priority)
	// Only checked if no standard character or drain is present
	// We scan manually because Decay entities are not fully integrated into PositionStore
	// Because of sub-pixel precision requirement of position
	// TODO: find a clever way around it for uniformity
	hasDecay := false
	if !isDrain && !hasChar {
		decayEntities := world.Decays.All()
		for _, e := range decayEntities {
			decay, ok := world.Decays.Get(e)
			if ok && decay.Column == ctx.CursorX && int(decay.YPosition) == ctx.CursorY {
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
		charFgColor = tcell.ColorBlack
	} else if hasChar {
		// Character found (Blue/Green/Red/Gold/Nugget)
		// Inherit background from character's foreground color
		fg, _, _ := charStyle.Decompose()
		cursorBgColor = fg

		if isNugget {
			charFgColor = render.RgbNuggetDark
		} else {
			charFgColor = tcell.ColorBlack
		}
	} else if hasDecay {
		// Decay found on empty space
		cursorBgColor = render.RgbDecay
		charFgColor = tcell.ColorBlack
	}

	// 4. Error Flash Overlay (Absolute Highest Priority for Background)
	// Reads component directly to ensure flash works during pause
	cursorComp, ok := world.Cursors.Get(c.gameCtx.CursorEntity)
	if ok && cursorComp.ErrorFlashEnd > 0 {
		if c.gameCtx.PausableClock.Now().UnixNano() < cursorComp.ErrorFlashEnd {
			cursorBgColor = render.RgbCursorError
			charFgColor = tcell.ColorBlack
		}
	}

	// 5. Render
	style := defaultStyle.Foreground(charFgColor).Background(cursorBgColor)
	buf.Set(screenX, screenY, charAtCursor, style)
}