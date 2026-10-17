// Package glyph is a bitmap font atlas and the software rasterizer that draws a
// cell grid with it: the reference drawer of any graphical host and the fallback
// of every one. The atlas is a TOML document, one bit per pixel of each glyph.
package glyph

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/lixenwraith/toml"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// Atlas is a bitmap font of one cell size. A glyph's rows hold one bit per column,
// column 0 the highest, so a cell is at most 32 pixels wide.
type Atlas struct {
	Name          string
	Width, Height int
	glyphs        map[rune][]uint32
}

type atlasDoc struct {
	Font struct {
		Name   string `toml:"name"`
		Width  int    `toml:"width"`
		Height int    `toml:"height"`
	} `toml:"font"`
	Glyph []struct {
		Char string   `toml:"char"`
		Rows []string `toml:"rows"`
	} `toml:"glyph"`
}

// Parse reads an atlas document, as tool/font-atlas writes it
func Parse(data []byte) (*Atlas, error) {
	var doc atlasDoc
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("font atlas: %w", err)
	}
	f := doc.Font
	if f.Width < 1 || f.Width > 32 || f.Height < 1 {
		return nil, fmt.Errorf("font atlas: cell %dx%d is not 1-32 wide and at least 1 high", f.Width, f.Height)
	}
	a := &Atlas{Name: f.Name, Width: f.Width, Height: f.Height, glyphs: make(map[rune][]uint32, len(doc.Glyph))}
	for _, g := range doc.Glyph {
		runes := []rune(g.Char)
		if len(runes) != 1 {
			return nil, fmt.Errorf("font atlas: glyph %q is not one rune", g.Char)
		}
		if len(g.Rows) != f.Height {
			return nil, fmt.Errorf("font atlas: glyph %q has %d rows, want %d", g.Char, len(g.Rows), f.Height)
		}
		rows := make([]uint32, f.Height)
		for y, row := range g.Rows {
			if len(row) != f.Width || strings.Trim(row, ".#") != "" {
				return nil, fmt.Errorf("font atlas: glyph %q row %d is not %d of . and #", g.Char, y, f.Width)
			}
			for x := range f.Width {
				if row[x] == '#' {
					rows[y] |= 1 << (f.Width - 1 - x)
				}
			}
		}
		a.glyphs[runes[0]] = rows
	}
	return a, nil
}

// Rows is a glyph's bitmap, one word per row, and whether the atlas holds it
func (a *Atlas) Rows(r rune) ([]uint32, bool) {
	rows, ok := a.glyphs[r]
	return rows, ok
}

// Len is how many glyphs the atlas holds
func (a *Atlas) Len() int { return len(a.glyphs) }

// Runes lists the glyphs the atlas holds in code point order, for a host that
// uploads them as one texture
func (a *Atlas) Runes() []rune { return slices.Sorted(maps.Keys(a.glyphs)) }

// Aspect is the cell's width over its height: what a host drawing with this atlas
// reports as its cell aspect
func (a *Atlas) Aspect() vmath.Aspect { return vmath.Aspect(float64(a.Width) / float64(a.Height)) }
