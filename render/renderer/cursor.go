package renderer

import (
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
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
func (r *CursorRenderer) IsVisible() bool {
	return !r.gameCtx.IsSearchMode() && !r.gameCtx.IsCommandMode()
}

// Render draws the cursor
func (r *CursorRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(constant.MaskUI)
	screenX := ctx.GameXOffset + ctx.CursorX
	screenY := ctx.GameYOffset + ctx.CursorY

	// Bounds check
	if screenX < ctx.GameXOffset || screenX >= ctx.ScreenWidth || screenY < ctx.GameYOffset || screenY >= ctx.GameYOffset+ctx.GameHeight {
		return
	}

	// 1. Determine default GameState (Empty Cell)
	var charAtCursor = ' '
	var cursorBgColor render.RGB

	// Default background based on mode
	if r.gameCtx.IsInsertMode() {
		cursorBgColor = render.RgbCursorInsert
	} else {
		cursorBgColor = render.RgbCursorNormal
	}

	var charFgColor = render.RgbBlack

	// 2. Scan entities at cursor position
	var entitiesBuf [constant.MaxEntitiesPerCell]core.Entity
	count := r.gameCtx.World.Positions.GetAllEntitiesAtInto(ctx.CursorX, ctx.CursorY, entitiesBuf[:])

	var glyphEntity core.Entity
	var sigilEntity core.Entity

	for i := 0; i < count; i++ {
		e := entitiesBuf[i]

		// Priority 1: Glyph (Interactable)
		// Stop immediately if found (first found takes precedence)
		if r.gameCtx.World.Components.Glyph.HasEntity(e) {
			glyphEntity = e
			break
		}

		// Priority 2: Sigil (Visual/Enemy)
		// Store candidate but continue searching for glyphs
		if sigilEntity == 0 && r.gameCtx.World.Components.Sigil.HasEntity(e) {
			sigilEntity = e
		}
	}

	// 3. Resolve Visuals
	if glyphEntity != 0 {
		if glyph, ok := r.gameCtx.World.Components.Glyph.GetComponent(glyphEntity); ok {
			charAtCursor = glyph.Rune
			fg := resolveGlyphColor(glyph)

			// Cursor background takes the entity's foreground color
			cursorBgColor = fg

			// Check for Nugget (special coloring)
			if r.gameCtx.World.Components.Nugget.HasEntity(glyphEntity) {
				cursorBgColor = render.RgbNuggetOrange
				charFgColor = render.RgbNuggetDark
			} else {
				charFgColor = render.RgbBlack
			}
		}
	} else if sigilEntity != 0 {
		if sigil, ok := r.gameCtx.World.Components.Sigil.GetComponent(sigilEntity); ok {
			charAtCursor = sigil.Rune
			// Cursor background takes the sigil's color
			cursorBgColor = resolveSigilColor(sigil.Color)
			charFgColor = render.RgbBlack
		}
	}

	// 4. Error Flash Overlay
	cursorComp, ok := r.gameCtx.World.Components.Cursor.GetComponent(r.gameCtx.World.Resources.Cursor.Entity)
	if ok && cursorComp.ErrorFlashRemaining > 0 {
		cursorBgColor = render.RgbCursorError
		charFgColor = render.RgbBlack
	}

	// 5. Render
	buf.SetWithBg(screenX, screenY, charAtCursor, charFgColor, cursorBgColor)
}