package renderer

import (
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
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
	buf.SetWriteMask(visual.MaskUI)
	screenX := ctx.GameXOffset + ctx.CursorX
	screenY := ctx.GameYOffset + ctx.CursorY

	// Bounds check
	if screenX < ctx.GameXOffset || screenX >= ctx.ScreenWidth || screenY < ctx.GameYOffset || screenY >= ctx.GameYOffset+ctx.GameHeight {
		return
	}

	// 1. Determine default state (Empty Cell)
	var charAtCursor = ' '
	var cursorBgColor terminal.RGB

	// Default background based on mode
	if r.gameCtx.IsInsertMode() {
		cursorBgColor = visual.RgbCursorInsert
	} else {
		cursorBgColor = visual.RgbCursorNormal
	}

	var charFgColor = visual.RgbBlack

	// 2. Scan entities at cursor position
	var entitiesBuf [parameter.MaxEntitiesPerCell]core.Entity
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
			fg := visual.GlyphColorLUT[glyph.Type][glyph.Level]

			// Cursor background takes the entity's foreground color
			cursorBgColor = fg

			// Check for Nugget (special coloring)
			if r.gameCtx.World.Components.Nugget.HasEntity(glyphEntity) {
				cursorBgColor = visual.RgbNuggetOrange
				charFgColor = visual.RgbNuggetDark
			} else {
				charFgColor = visual.RgbBlack
			}
		}
	} else if sigilEntity != 0 {
		if sigil, ok := r.gameCtx.World.Components.Sigil.GetComponent(sigilEntity); ok {
			charAtCursor = sigil.Rune
			// Cursor background takes the sigil's color
			cursorBgColor = sigil.Color
			charFgColor = visual.RgbBlack
		}
	}

	// 4. Error Flash Overlay
	cursorComp, ok := r.gameCtx.World.Components.Cursor.GetComponent(r.gameCtx.World.Resources.Player.Entity)
	if ok && cursorComp.ErrorFlashRemaining > 0 {
		cursorBgColor = visual.RgbCursorError
		charFgColor = visual.RgbBlack
	}

	// 5. Render
	buf.SetWithBg(screenX, screenY, charAtCursor, charFgColor, cursorBgColor)
}