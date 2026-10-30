package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/position"
)

// cellContent resolves what a cell shows under a cursor or a marker: its top glyph,
// the one typing acts on, or failing that the first sigil.
func cellContent(gameCtx *engine.GameContext, x, y int) (glyph, sigil core.Entity) {
	if glyph, ok := gameCtx.World.Positions.TopGlyphAt(x, y); ok {
		return glyph, 0
	}
	var entities [parameter.MaxEntitiesPerCell]core.Entity
	count := copy(entities[:], gameCtx.World.Positions.EntitiesAt(x, y, position.ScopeAll))
	for i := range count {
		if gameCtx.World.Components.Sigil.HasEntity(entities[i]) {
			return 0, entities[i]
		}
	}
	return 0, 0
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
	glyphEntity, sigilEntity := cellContent(r.gameCtx, ctx.CursorX, ctx.CursorY)

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
