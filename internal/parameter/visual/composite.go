package visual

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
)

// QuasarChars defines the 3×5 visual representation
var QuasarChars = [3][5]rune{
	{'╔', '═', '╦', '═', '╗'},
	{'╠', '═', '╬', '═', '╣'},
	{'╚', '═', '╩', '═', '╝'},
}

// SwarmPatternChars defines visual patterns for swarm composite
var SwarmPatternChars = [2][2][4]rune{
	// Pattern 0: Pulse State O
	{
		{'▄', '▀', '▀', '▄'},
		{'▀', '▄', '▄', '▀'},
	},
	// Pattern 1: Pulse State X
	{
		{'▀', '▄', '▄', '▀'},
		{'▄', '▀', '▀', '▄'},
	},
}

// SwarmPatternChars256 draws the two swarm frames from double-line box drawing, which every
// console font carries: a ring for O and the ring pinched inward for X
var SwarmPatternChars256 = [2][2][4]rune{
	{
		{'╔', '═', '═', '╗'},
		{'╚', '═', '═', '╝'},
	},
	{
		{'═', '╗', '╔', '═'},
		{'═', '╝', '╚', '═'},
	},
}

// LookFrame is a frame as authored, a string per row: its glyphs, a space for none,
// and per cell its foreground and background palette entries (hex, a space for the
// look's colour and for no background) and attribute (B bold, D dim). Fg, Bg and
// Attr are each optional.
type LookFrame struct {
	Glyphs []string `toml:"glyphs"`
	Fg     []string `toml:"fg"`
	Bg     []string `toml:"bg"`
	Attr   []string `toml:"attr"`
}

// LookArt is a look as authored: the palettes its frames index, the colour of a
// glyph without an entry and, where Console, the 256-colour pair
type LookArt struct {
	Fg, Bg       []color.RGB
	Color        color.RGB
	Fg256, Bg256 uint8
	Console      bool
	Frames       []LookFrame
}

// LookPaletteMax is the entries a palette holds, one hex digit's
const LookPaletteMax = 16

// CompileLook resolves art into the cells a renderer draws, refusing a frame of
// another size than width by height and an entry outside its palette
func CompileLook(width, height int, art LookArt) (component.Look, error) {
	look := component.Look{Fg: art.Fg, Bg: art.Bg, Color: art.Color, Fg256: art.Fg256, Bg256: art.Bg256, Console: art.Console}
	var errs []error
	if len(art.Fg) > LookPaletteMax || len(art.Bg) > LookPaletteMax {
		errs = append(errs, fmt.Errorf("fg and bg hold %d and %d colours, at most %d", len(art.Fg), len(art.Bg), LookPaletteMax))
	}
	if len(art.Frames) == 0 {
		errs = append(errs, errors.New("no frame"))
	}
	for i, f := range art.Frames {
		cells := make([]component.LookCell, width*height)
		for c := range cells {
			cells[c].Fg, cells[c].Bg = -1, -1
		}
		// grid reads one of the frame's grids into its cells; a grid left out leaves them
		grid := func(key string, rows []string, want string, set func(c *component.LookCell, r rune) bool) {
			if rows == nil && key != "glyphs" {
				return
			}
			if len(rows) != height {
				errs = append(errs, fmt.Errorf("frames[%d]: %s has %d rows, want height %d", i, key, len(rows), height))
				return
			}
			for y, row := range rows {
				if n := utf8.RuneCountInString(row); n != width {
					errs = append(errs, fmt.Errorf("frames[%d]: %s[%d] %q has %d cells, want width %d", i, key, y, row, n, width))
					continue
				}
				for x, r := range []rune(row) {
					if !set(&cells[y*width+x], r) {
						errs = append(errs, fmt.Errorf("frames[%d]: %s[%d] %q: %q is not %s", i, key, y, row, r, want))
						break // a row's first fault names it
					}
				}
			}
		}
		grid("glyphs", f.Glyphs, "", func(c *component.LookCell, r rune) bool {
			if r != ' ' {
				c.Rune = r
			}
			return true
		})
		grid("fg", f.Fg, fmt.Sprintf("a space or an entry of fg's %d", len(art.Fg)), func(c *component.LookCell, r rune) (ok bool) {
			c.Fg, ok = lookEntry(r, len(art.Fg))
			return ok
		})
		grid("bg", f.Bg, fmt.Sprintf("a space or an entry of bg's %d", len(art.Bg)), func(c *component.LookCell, r rune) (ok bool) {
			c.Bg, ok = lookEntry(r, len(art.Bg))
			return ok
		})
		grid("attr", f.Attr, "B, D or a space", func(c *component.LookCell, r rune) bool {
			switch r {
			case 'B':
				c.Attr = terminal.AttrBold
			case 'D':
				c.Attr = terminal.AttrDim
			}
			return strings.ContainsRune("BD ", r)
		})
		look.Frames = append(look.Frames, cells)
	}
	if len(errs) > 0 {
		return component.Look{}, errors.Join(errs...)
	}
	return look, nil
}

// lookEntry reads a cell's palette entry, a hex digit below n, or a space for none
func lookEntry(r rune, n int) (int8, bool) {
	if r == ' ' {
		return -1, true
	}
	i := strings.IndexRune("0123456789abcdef", unicode.ToLower(r))
	return int8(i), i >= 0 && i < n
}
