// @lixen: #dev{feature[shield(render,system)],feature[spirit(render,system)]}
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
	buf.SetWriteMask(constant.MaskGlyph)

	entities := r.glyphStore.All()
	for _, entity := range entities {
		glyph, ok := r.glyphStore.Get(entity)
		if !ok {
			continue
		}

		// Gold is handled in its own composite renderer with a different mask
		if glyph.Type == component.GlyphGold {
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

		buf.SetFgOnly(screenX, screenY, glyph.Rune, fg, terminal.AttrNone)
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
	case component.GlyphGreen:
		switch g.Level {
		case component.GlyphDark:
			return render.RgbSequenceGreenDark
		case component.GlyphNormal:
			return render.RgbSequenceGreenNormal
		case component.GlyphBright:
			return render.RgbSequenceGreenBright
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
	case component.GlyphGold:
		return render.RgbSequenceGold
	}

	// Debug
	return render.RgbShieldBase
}