package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// GlyphRenderer draws typeable spawned content entities and the scenery drifting
// behind them
type GlyphRenderer struct {
	gameCtx       *engine.GameContext
	renderCell    glyphCellRenderer
	renderScenery glyphCellRenderer
	levels256     [5][3]uint8     // console entry per [GlyphType][GlyphLevel]
	sceneryTC     [5][3]color.RGB // scenery per [GlyphType][GlyphLevel], hazed by depth

	// Per frame, each layer's glyphs at the cells they are drawn in: scenery by level
	// far to near, then typeable text
	layers [typeableLayer + 1][]drawnGlyph
}

// drawnGlyph is a glyph at the cell it is drawn in this frame
type drawnGlyph struct {
	x, y  int
	glyph component.GlyphComponent
}

// typeableLayer is the layer typeable text draws in, over every scenery level
const typeableLayer = len(parameter.DriftLayerFactor)

// glyphCellRenderer draws one glyph in the colour mode chosen at construction
type glyphCellRenderer func(buf *render.RenderBuffer, screenX, screenY int, glyph *component.GlyphComponent)

// NewGlyphRenderer creates a new glyph renderer
func NewGlyphRenderer(gameCtx *engine.GameContext) *GlyphRenderer {
	r := &GlyphRenderer{gameCtx: gameCtx}
	if cfg := gameCtx.World.Resources.Config; cfg.ColorMode == terminal.ColorMode256 {
		r.renderCell, r.levels256 = r.cell256, glyphLevels256(render.ConsoleFor(cfg.ConsolePalette))
		r.renderScenery = r.cell256
	} else {
		r.renderCell, r.renderScenery = r.cellTrueColor, r.sceneryTrueColor
		for typ, tc := range visual.GlyphColorLUT {
			for level, mix := range visual.SceneryDepthMix {
				r.sceneryTC[typ][level] = color.Lerp(visual.RgbBackground, tc[component.GlyphBright], mix)
			}
		}
	}
	return r
}

// Render draws each cell's top typeable glyph in its cell, and scenery where its
// layer stands at the frame's moment, carried on from its cells by the part of a
// tick since, with the text still entering past the edge. The layers draw far to
// near, typeable text last, each over the ones behind it. Rock and walls draw first,
// and a glyph in them would print over them, so it is not shown.
func (r *GlyphRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	world := r.gameCtx.World
	glyphs, entering := world.Components.Glyph, world.Resources.Entry.Lines
	if glyphs.CountEntities() == 0 && len(entering) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskGlyph)
	positions := world.Positions
	width, height := world.Resources.Config.MapWidth, world.Resources.Config.MapHeight
	carry := r.carry(ctx)
	for i := range r.layers {
		r.layers[i] = r.layers[i][:0]
	}
	for i := range entering {
		line := &entering[i]
		r.addEntering(line, world.Resources.Drift.Step[line.Level], carry[line.Level], width, height)
	}

	glyphs.Each(func(entity core.Entity, glyph *component.GlyphComponent) bool {
		// Gold is handled in its own composite renderer with a different mask
		if glyph.Type == component.GlyphGold {
			return true
		}

		pos, ok := positions.GetPosition(entity)
		if !ok {
			return true
		}
		if positions.IsTypeable(entity) {
			if positions.TopGlyphIs(entity, pos.X, pos.Y) {
				r.layers[typeableLayer] = append(r.layers[typeableLayer], drawnGlyph{x: pos.X, y: pos.Y, glyph: *glyph})
			}
			return true
		}
		x, y := pos.X+carry[glyph.Level][0], pos.Y+carry[glyph.Level][1]
		if x >= 0 && x < width && y >= 0 && y < height {
			r.layers[glyph.Level] = append(r.layers[glyph.Level], drawnGlyph{x: x, y: y, glyph: *glyph})
		}
		return true
	})

	for layer, drawn := range r.layers {
		renderCell := r.renderScenery
		if layer == typeableLayer {
			renderCell = r.renderCell
		}
		for i := range drawn {
			d := &drawn[i]
			if positions.HasBlockingWallAt(d.x, d.y, component.WallBlockSpawn) {
				continue
			}
			if screenX, screenY, visible := ctx.MapToScreen(d.x, d.y); visible {
				renderCell(buf, screenX, screenY, &d.glyph)
			}
		}
	}
}

// addEntering adds the runes of a line still entering that its layer brings onto the
// map by the frame's moment, under the glyphs already there. The spawner left them
// where their layer stood before the tick's step, and scenery carries on from there.
func (r *GlyphRenderer) addEntering(line *engine.EntryLine, step, carry [2]int, width, height int) {
	layer, x, y := typeableLayer, line.X+step[0], line.Y+step[1]
	if line.Scenery {
		layer, x, y = int(line.Level), x+carry[0], y+carry[1]
	}
	if y < 0 || y >= height {
		return
	}
	glyph := component.GlyphComponent{Type: line.Type, Level: line.Level}
	for _, ch := range line.Text {
		if x >= 0 && x < width {
			if ch != ' ' {
				glyph.Rune = ch
				r.layers[layer] = append(r.layers[layer], drawnGlyph{x: x, y: y, glyph: glyph})
			}
		} else if x >= width && line.Dir > 0 || x < 0 && line.Dir < 0 {
			return // the rest lies further past the edge
		}
		x += line.Dir
	}
}

// carry is how far each drift layer stands past its cells at the frame's moment:
// from the tick the frame follows, a tick on at most, and none before a first tick
func (r *GlyphRenderer) carry(ctx render.RenderContext) (c [len(parameter.DriftLayerFactor)][2]int) {
	t := r.gameCtx.World.Resources.Time
	if t.PaceTime.IsZero() {
		return c
	}
	since := min(max(ctx.GameTime.Sub(t.PaceTime), 0), t.DeltaTime)
	for level := range c {
		c[level][0], c[level][1] = r.gameCtx.World.Resources.Drift.Carry(level, since)
	}
	return c
}

func (r *GlyphRenderer) cellTrueColor(buf *render.RenderBuffer, screenX, screenY int, glyph *component.GlyphComponent) {
	buf.SetFgOnly(screenX, screenY, glyph.Rune, visual.GlyphColorLUT[glyph.Type][glyph.Level], terminal.AttrNone)
}

func (r *GlyphRenderer) sceneryTrueColor(buf *render.RenderBuffer, screenX, screenY int, glyph *component.GlyphComponent) {
	buf.SetFgOnly(screenX, screenY, glyph.Rune, r.sceneryTC[glyph.Type][glyph.Level], terminal.AttrNone)
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
