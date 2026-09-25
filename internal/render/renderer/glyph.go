package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
)

// GlyphRenderer draws typeable spawned content entities
type GlyphRenderer struct {
	gameCtx    *engine.GameContext
	renderCell glyphCellRenderer
	levels256  [5][3]uint8 // console entry per [GlyphType][GlyphLevel]
}

// glyphCellRenderer draws one glyph in the colour mode chosen at construction
type glyphCellRenderer func(buf *render.RenderBuffer, screenX, screenY int, glyph *component.GlyphComponent)

// NewGlyphRenderer creates a new glyph renderer
func NewGlyphRenderer(gameCtx *engine.GameContext) *GlyphRenderer {
	r := &GlyphRenderer{gameCtx: gameCtx}
	if cfg := gameCtx.World.Resources.Config; cfg.ColorMode == terminal.ColorMode256 {
		r.renderCell, r.levels256 = r.cell256, glyphLevels256(render.ConsoleFor(cfg.ConsolePalette))
	} else {
		r.renderCell = r.cellTrueColor
	}
	return r
}

// Render draws all glyph entities
func (r *GlyphRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	glyphs := r.gameCtx.World.Components.Glyph
	if glyphs.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskGlyph)

	glyphs.Each(func(entity core.Entity, glyph *component.GlyphComponent) bool {
		// Gold is handled in its own composite renderer with a different mask
		if glyph.Type == component.GlyphGold {
			return true
		}

		pos, ok := r.gameCtx.World.Positions.GetPosition(entity)
		if !ok {
			return true
		}

		screenX, screenY, visible := ctx.MapToScreen(pos.X, pos.Y)
		if !visible {
			return true
		}

		r.renderCell(buf, screenX, screenY, glyph)
		return true
	})
}

func (r *GlyphRenderer) cellTrueColor(buf *render.RenderBuffer, screenX, screenY int, glyph *component.GlyphComponent) {
	buf.SetFgOnly(screenX, screenY, glyph.Rune, visual.GlyphColorLUT[glyph.Type][glyph.Level], terminal.AttrNone)
}

func (r *GlyphRenderer) cell256(buf *render.RenderBuffer, screenX, screenY int, glyph *component.GlyphComponent) {
	buf.SetFgOnly(screenX, screenY, glyph.Rune, color.RGB{R: r.levels256[glyph.Type][glyph.Level]}, terminal.AttrFg256)
}

// glyphLevels256 gives each glyph type its hue's two console entries, darker for Dark and
// lighter for Normal and Bright, ordered by how they look on this palette; a console hue has
// no third entry to keep Normal apart from Bright
func glyphLevels256(c *render.Console) (levels [5][3]uint8) {
	for typ, tc := range visual.GlyphColorLUT {
		dark, light := c.Family(tc[component.GlyphNormal])
		if tc[component.GlyphDark] == tc[component.GlyphNormal] {
			dark = c.Nearest(tc[component.GlyphNormal], dark, light)
			light = dark
		}
		levels[typ] = [3]uint8{dark, light, light}
	}
	return levels
}
