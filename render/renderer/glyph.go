package renderer

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// GlyphRenderer draws typeable spawned content entities
type GlyphRenderer struct {
	gameCtx *engine.GameContext
}

// NewGlyphRenderer creates a new glyph renderer
func NewGlyphRenderer(gameCtx *engine.GameContext) *GlyphRenderer {
	return &GlyphRenderer{
		gameCtx: gameCtx,
	}
}

// Render draws all glyph entities
func (r *GlyphRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(constant.MaskGlyph)

	entities := r.gameCtx.World.Components.Glyph.GetAllEntities()
	for _, entity := range entities {
		glyph, ok := r.gameCtx.World.Components.Glyph.GetComponent(entity)
		if !ok {
			continue
		}

		// Gold is handled in its own composite renderer with a different mask
		if glyph.Type == component.GlyphGold {
			continue
		}

		pos, ok := r.gameCtx.World.Positions.GetPosition(entity)
		if !ok {
			continue
		}

		screenX := ctx.GameXOffset + pos.X
		screenY := ctx.GameYOffset + pos.Y

		if screenX < ctx.GameXOffset || screenX >= ctx.ScreenWidth ||
			screenY < ctx.GameYOffset || screenY >= ctx.GameYOffset+ctx.GameHeight {
			continue
		}

		fg := resolveGlyphColor(glyph)

		buf.SetFgOnly(screenX, screenY, glyph.Rune, fg, terminal.AttrNone)
	}
}

// resolveGlyphColor maps GlyphType and GlyphLevel to RGB
func resolveGlyphColor(g component.GlyphComponent) render.RGB {
	switch g.Type {
	case component.GlyphBlue:
		switch g.Level {
		case component.GlyphDark:
			return render.RgbGlyphBlueDark
		case component.GlyphNormal:
			return render.RgbGlyphBlueNormal
		case component.GlyphBright:
			return render.RgbGlyphBlueBright
		}
	case component.GlyphGreen:
		switch g.Level {
		case component.GlyphDark:
			return render.RgbGlyphGreenDark
		case component.GlyphNormal:
			return render.RgbGlyphGreenNormal
		case component.GlyphBright:
			return render.RgbGlyphGreenBright
		}
	case component.GlyphRed:
		switch g.Level {
		case component.GlyphDark:
			return render.RgbGlyphRedDark
		case component.GlyphNormal:
			return render.RgbGlyphRedNormal
		case component.GlyphBright:
			return render.RgbGlyphRedBright
		}
	case component.GlyphGold:
		return render.RgbGlyphGold
	}

	// Debug
	return render.RgbShieldBase
}