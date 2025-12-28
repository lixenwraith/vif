package renderers

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// GlyphRenderer draws typeable spawned content entities
type GlyphRenderer struct {
	gameCtx    *engine.GameContext
	glyphStore *engine.Store[component.GlyphComponent]
}

// NewGlyphRenderer creates a new glyph renderer
func NewGlyphRenderer(gameCtx *engine.GameContext) *GlyphRenderer {
	return &GlyphRenderer{
		gameCtx:    gameCtx,
		glyphStore: engine.GetStore[component.GlyphComponent](gameCtx.World),
	}
}

// Render draws all glyph entities
func (r *GlyphRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	entities := r.glyphStore.All()
	if len(entities) == 0 {
		return
	}

	buf.SetWriteMask(constant.MaskTypeable)

	for _, entity := range entities {
		glyph, ok := r.glyphStore.Get(entity)
		if !ok {
			continue
		}

		pos, ok := r.gameCtx.World.Positions.Get(entity)
		if !ok {
			continue
		}

		screenX := ctx.GameX + pos.X
		screenY := ctx.GameY + pos.Y

		if screenX < ctx.GameX || screenX >= ctx.Width ||
			screenY < ctx.GameY || screenY >= ctx.GameY+ctx.GameHeight {
			continue
		}

		fg := resolveGlyphColor(glyph)
		attrs := resolveGlyphStyle(glyph.Style)

		buf.SetFgOnly(screenX, screenY, glyph.Rune, fg, attrs)
	}
}

// resolveGlyphColor maps GlyphType and GlyphLevel to RGB
func resolveGlyphColor(g component.GlyphComponent) render.RGB {
	switch g.Type {
	case component.GlyphBlue:
		switch g.Level {
		case component.GlyphDark:
			return render.RgbSequenceBlueDark
		case component.GlyphNormal:
			return render.RgbSequenceBlueNormal
		case component.GlyphBright:
			return render.RgbSequenceBlueBright
		}
	case component.GlyphRed:
		switch g.Level {
		case component.GlyphDark:
			return render.RgbSequenceRedDark
		case component.GlyphNormal:
			return render.RgbSequenceRedNormal
		case component.GlyphBright:
			return render.RgbSequenceRedBright
		}
	default: // GlyphGreen
		switch g.Level {
		case component.GlyphDark:
			return render.RgbSequenceGreenDark
		case component.GlyphNormal:
			return render.RgbSequenceGreenNormal
		case component.GlyphBright:
			return render.RgbSequenceGreenBright
		}
	}
	return render.RgbSequenceGreenNormal
}

// resolveGlyphStyle maps TextStyle to terminal attributes
func resolveGlyphStyle(style component.TextStyle) terminal.Attr {
	switch style {
	case component.StyleBold:
		return terminal.AttrBold
	case component.StyleDim:
		return terminal.AttrDim
	case component.StyleUnderline:
		return terminal.AttrUnderline
	case component.StyleBlink:
		return terminal.AttrBlink
	default:
		return terminal.AttrNone
	}
}