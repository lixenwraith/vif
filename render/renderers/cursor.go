package renderers

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
	nuggetStore *engine.Store[component.NuggetComponent]
	drainStore  *engine.Store[component.DrainComponent]
	decayStore  *engine.Store[component.DecayComponent]
	resolver    *engine.ZIndexResolver
}

// NewCursorRenderer creates a new cursor renderer
func NewCursorRenderer(gameCtx *engine.GameContext) *CursorRenderer {
	return &CursorRenderer{
		gameCtx:     gameCtx,
		cursorStore: engine.GetStore[component.CursorComponent](gameCtx.World),
		glyphStore:  engine.GetStore[component.GlyphComponent](gameCtx.World),
		nuggetStore: engine.GetStore[component.NuggetComponent](gameCtx.World),
		drainStore:  engine.GetStore[component.DrainComponent](gameCtx.World),
		decayStore:  engine.GetStore[component.DecayComponent](gameCtx.World),
		resolver:    engine.MustGetResource[*engine.ZIndexResolver](gameCtx.World.Resources),
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

	// 2. Get entities at cursor position using z-index selection

	// Get all entities at cursor position
	entities := r.gameCtx.World.Positions.GetAllAt(ctx.CursorX, ctx.CursorY)

	// Check for Drain (highest priority overlay, masks everything)
	isDrain := false
	for _, e := range entities {
		if r.drainStore.Has(e) {
			isDrain = true
			break
		}
	}

	// Get the highest priority character entity for display (excluding cursor and non-character entities)
	displayEntity := r.resolver.SelectTopEntityFiltered(entities, func(e core.Entity) bool {
		// Exclude cursor itself and non-character entities
		if e == r.gameCtx.CursorEntity {
			return false
		}
		// Only consider entities with characters
		return r.glyphStore.Has(e)
	})

	hasChar := displayEntity != 0
	isNugget := false
	var charFg render.RGB

	if hasChar {
		if glyph, ok := r.glyphStore.Get(displayEntity); ok {
			charAtCursor = glyph.Rune
			charFg = resolveGlyphColor(glyph)
			charAtCursor = glyph.Rune
			charFg = resolveGlyphColor(glyph)
		}
		if r.nuggetStore.Has(displayEntity) {
			isNugget = true
			charFg = render.RgbNuggetOrange
		}
	}

	// Priority 3: Decay (Lowest Priority)
	hasDecay := false
	if !isDrain && !hasChar {
		for _, e := range entities {
			if decay, ok := r.decayStore.Get(e); ok {
				charAtCursor = decay.Char
				hasDecay = true
				break
			}
		}
	}

	// 3. Resolve Final Visuals based on priority
	if isDrain {
		// Drain overrides everything
		charAtCursor = constant.DrainChar
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

	// 4. Error Flash Overlay
	cursorComp, ok := r.cursorStore.Get(r.gameCtx.CursorEntity)
	if ok && cursorComp.ErrorFlashRemaining > 0 {
		cursorBgColor = render.RgbCursorError
		charFgColor = render.RgbBlack
	}

	// 5. Render
	buf.SetWithBg(screenX, screenY, charAtCursor, charFgColor, cursorBgColor)
}