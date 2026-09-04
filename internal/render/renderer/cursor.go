package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
)

// cursorCellContent resolves what a cursor is standing on: the glyph that cell
// holds, or failing that the sigil. A glyph wins and stops the scan — it is the
// interactable, and the first one found takes precedence — while a sigil is only
// a candidate until the whole cell has been read.
//
// Both cursor renderers read it, so the local cursor and a peer's agree about what
// is under them rather than answering the question twice.
func cursorCellContent(gameCtx *engine.GameContext, x, y int) (glyph, sigil core.Entity) {
	var entities [parameter.MaxEntitiesPerCell]core.Entity
	count := gameCtx.World.Positions.GetAllEntitiesAtInto(x, y, entities[:])
	for i := range count {
		e := entities[i]
		if gameCtx.World.Components.Glyph.HasEntity(e) {
			return e, 0
		}
		if sigil == 0 && gameCtx.World.Components.Sigil.HasEntity(e) {
			sigil = e
		}
	}
	return 0, sigil
}

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

// IsVisible returns true when a local cursor exists and the mode draws it
func (r *CursorRenderer) IsVisible() bool {
	return r.gameCtx.World.Resources.Player.Valid() &&
		!r.gameCtx.IsSearchMode() && !r.gameCtx.IsCommandMode()
}

// Render draws the cursor
func (r *CursorRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(visual.MaskUI)

	// Transform cursor position to screen coords
	screenX, screenY, visible := ctx.MapToScreen(ctx.CursorX, ctx.CursorY)
	if !visible {
		return
	}

	// 1. Determine default state (Empty Cell)
	var charAtCursor = ' '
	var cursorBgColor color.RGB

	// Default background based on mode
	if r.gameCtx.IsInsertMode() {
		cursorBgColor = visual.RgbCursorInsert
	} else {
		cursorBgColor = visual.RgbCursorNormal
	}

	var charFgColor = visual.RgbBlack

	// 2. Scan entities at cursor position
	glyphEntity, sigilEntity := cursorCellContent(r.gameCtx, ctx.CursorX, ctx.CursorY)

	// 3. Resolve Visuals
	if glyphEntity != 0 {
		if glyph, ok := r.gameCtx.World.Components.Glyph.GetPtr(glyphEntity); ok {
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
		if sigil, ok := r.gameCtx.World.Components.Sigil.GetPtr(sigilEntity); ok {
			charAtCursor = sigil.Rune
			// Cursor background takes the sigil's color
			cursorBgColor = sigil.Color
			charFgColor = visual.RgbBlack
		}
	}

	// 4. Error Flash Overlay
	cursorViewComp, ok := r.gameCtx.World.Components.CursorView.GetPtr(r.gameCtx.World.Resources.Player.Entity)
	if ok && cursorViewComp.ErrorFlashRemaining > 0 {
		cursorBgColor = visual.RgbCursorError
		charFgColor = visual.RgbBlack
	}

	// 5. Render
	buf.SetWithBg(screenX, screenY, charAtCursor, charFgColor, cursorBgColor)
}
