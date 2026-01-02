package renderers

// @lixen: #dev{feature[drain(render,system)],feature[dust(render,system)],feature[quasar(render,system)]}

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// CursorRenderer draws the cursor with complex entity overlap handling
type CursorRenderer struct {
	gameCtx     *engine.GameContext
	cursorStore *engine.Store[component.CursorComponent]
	glyphStore  *engine.Store[component.GlyphComponent]
	sigilStore  *engine.Store[component.SigilComponent]
	nuggetStore *engine.Store[component.NuggetComponent]
	drainStore  *engine.Store[component.DrainComponent]
	decayStore  *engine.Store[component.DecayComponent]
}

// NewCursorRenderer creates a new cursor renderer
func NewCursorRenderer(gameCtx *engine.GameContext) *CursorRenderer {
	return &CursorRenderer{
		gameCtx:     gameCtx,
		cursorStore: engine.GetStore[component.CursorComponent](gameCtx.World),
		glyphStore:  engine.GetStore[component.GlyphComponent](gameCtx.World),
		sigilStore:  engine.GetStore[component.SigilComponent](gameCtx.World),
		nuggetStore: engine.GetStore[component.NuggetComponent](gameCtx.World),
		drainStore:  engine.GetStore[component.DrainComponent](gameCtx.World),
		decayStore:  engine.GetStore[component.DecayComponent](gameCtx.World),
	}
}

// IsVisible returns true when the cursor should be rendered
func (r *CursorRenderer) IsVisible() bool {
	return !r.gameCtx.IsSearchMode() && !r.gameCtx.IsCommandMode()
}

// Render draws the cursor
func (r *CursorRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(constant.MaskUI)
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
	if r.gameCtx.IsInsertMode() {
		cursorBgColor = render.RgbCursorInsert
	} else {
		cursorBgColor = render.RgbCursorNormal
	}

	var charFgColor = render.RgbBlack

	// 2. Scan entities at cursor position (Zero allocation)
	var entitiesBuf [constant.MaxEntitiesPerCell]core.Entity
	count := r.gameCtx.World.Positions.GetAllAtInto(ctx.CursorX, ctx.CursorY, entitiesBuf[:])

	var glyphEntity core.Entity
	var sigilEntity core.Entity

	for i := 0; i < count; i++ {
		e := entitiesBuf[i]

		// Priority 1: Glyph (Interactable)
		// Stop immediately if found (first found takes precedence)
		if r.glyphStore.Has(e) {
			glyphEntity = e
			break
		}

		// Priority 2: Sigil (Visual/Enemy)
		// Store candidate but continue searching for glyphs
		if sigilEntity == 0 && r.sigilStore.Has(e) {
			sigilEntity = e
		}
	}

	// 3. Resolve Visuals
	if glyphEntity != 0 {
		if glyph, ok := r.glyphStore.Get(glyphEntity); ok {
			charAtCursor = glyph.Rune
			fg := resolveGlyphColor(glyph)

			// Cursor background takes the entity's foreground color
			cursorBgColor = fg

			// Check for Nugget (special coloring)
			if r.nuggetStore.Has(glyphEntity) {
				cursorBgColor = render.RgbNuggetOrange
				charFgColor = render.RgbNuggetDark
			} else {
				charFgColor = render.RgbBlack
			}
		}
	} else if sigilEntity != 0 {
		if sigil, ok := r.sigilStore.Get(sigilEntity); ok {
			charAtCursor = sigil.Rune
			// Cursor background takes the sigil's color
			cursorBgColor = resolveSigilColor(sigil.Color)
			charFgColor = render.RgbBlack
		}
	}

	// 4. Error Flash Overlay
	cursorComp, ok := r.cursorStore.Get(r.gameCtx.CursorEntity)
	if ok && cursorComp.ErrorFlashRemaining > 0 {
		cursorBgColor = render.RgbCursorError
		charFgColor = render.RgbBlack
	}

	// 5. Render
	buf.SetWithBg(screenX, screenY, charAtCursor, charFgColor, cursorBgColor)
}