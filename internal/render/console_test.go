package render

import (
	"testing"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
)

// ubuntuPalette is the vtrgb palette Ubuntu's setvtrgb loads at boot
var ubuntuPalette = consolePalette(8,
	0x010101, 0xde382b, 0x39b54a, 0xffc706, 0x006fb8, 0x762671, 0x2cb5e9, 0xcccccc,
	0x808080, 0xff0000, 0x00ff00, 0xffff00, 0x0000ff, 0xff00ff, 0x00ffff, 0xffffff)

var testPalettes = map[string]engine.ConsolePalette{
	"vga": VGAPalette, "ubuntu": ubuntuPalette, "freebsd": FreeBSDPalette,
}

// TestPaletteIndexKeepsItsConsoleHue: a console entry is itself, a bright background falls to
// its normal counterpart where backgrounds have only eight, and an xterm index lands in its hue.
func TestPaletteIndexKeepsItsConsoleHue(t *testing.T) {
	t.Parallel()
	families := map[uint8]uint8{
		color.P256Red: 1, color.P256Green: 2, color.P256Yellow: 3, 21: 4, 201: 5,
		color.P256Cyan: 6, 232: 0, 255: 7, color.P256Crimson: 1,
	}
	for name, p := range testPalettes {
		c := ConsoleFor(p)
		for i := range uint8(16) {
			wantBg := i
			if int(i) >= p.BgColors {
				wantBg = i - 8
			}
			if fg, bg := c.fgEntry(color.RGB{R: i}, true), c.bgEntry(color.RGB{R: i}, true); fg != i || bg != wantBg {
				t.Errorf("%s: entry %d shows as fg %d bg %d, want %d and %d", name, i, fg, bg, i, wantBg)
			}
		}
		for i, family := range families {
			if fg, bg := c.fgEntry(color.RGB{R: i}, true), c.bgEntry(color.RGB{R: i}, true); fg&7 != family || bg&7 != family {
				t.Errorf("%s: xterm %d shows as fg %d bg %d, want hue %d", name, i, fg, bg, family)
			}
		}
	}
}

// TestQuantizedTextStaysVisible: text on a background of its own entry takes one that shows,
// UI text is held to a contrast, a full block keeps its color, and the theme background under
// text is the console's black even where a dark gray is nearer.
func TestQuantizedTextStaysVisible(t *testing.T) {
	t.Parallel()
	yellow := color.RGB{R: color.P256Yellow}
	for name, p := range testPalettes {
		c := ConsoleFor(p)
		b := NewRenderBuffer(terminal.ColorMode256, 4, 1)
		b.SetConsole(c)
		b.SetWriteMask(visual.MaskGlyph)
		b.Set(0, 0, 'a', yellow, yellow, BlendReplace, 1, terminal.AttrFg256|terminal.AttrBg256)
		b.Set(1, 0, '█', yellow, yellow, BlendReplace, 1, terminal.AttrFg256|terminal.AttrBg256)
		b.SetWriteMask(visual.MaskUI)
		b.SetWithBg(2, 0, 'u', visual.RgbWhite, color.RGB{R: 200, G: 200, B: 200})
		b.SetFgOnly(3, 0, 'p', c.Color(1), terminal.AttrNone)
		b.finalize()

		glyph, block, ui := b.CellAt(0, 0), b.CellAt(1, 0), b.CellAt(2, 0)
		if playfield := b.CellAt(3, 0); playfield.Bg.R != 0 {
			t.Errorf("%s: the theme background shows as %d", name, playfield.Bg.R)
		}
		if labDistance(c.lab[glyph.Fg.R], c.lab[glyph.Bg.R]) < consoleClashDE {
			t.Errorf("%s: glyph %d on %d vanishes", name, glyph.Fg.R, glyph.Bg.R)
		}
		if block.Fg.R != c.fgEntry(yellow, true) {
			t.Errorf("%s: full block took %d, want its own %d", name, block.Fg.R, c.fgEntry(yellow, true))
		}
		if got := c.Contrast(ui.Fg.R, ui.Bg.R); got < consoleUIContrast {
			t.Errorf("%s: UI text %d on %d has contrast %.2f", name, ui.Fg.R, ui.Bg.R, got)
		}
	}
}

// TestQuantizeLeavesNoStyleToRecolor: the console recolors styled text (dim grays it, italic
// greens it), so reverse is swapped in, dim takes a darker entry and no style is sent.
func TestQuantizeLeavesNoStyleToRecolor(t *testing.T) {
	t.Parallel()
	c := ConsoleFor(VGAPalette)
	b := NewRenderBuffer(terminal.ColorMode256, 2, 1)
	b.SetConsole(c)
	b.Set(0, 0, 'r', c.Color(15), c.Color(4), BlendReplace, 1, terminal.AttrReverse|terminal.AttrBold)
	b.Set(1, 0, 'd', c.Color(14), c.Color(0), BlendReplace, 1, terminal.AttrDim|terminal.AttrItalic)
	b.finalize()

	// Reversed, bright white becomes a background, which VGA shows as light gray
	for x, want := range [2][2]uint8{{4, 7}, {6, 0}} {
		cell := b.CellAt(x, 0)
		if cell.Fg.R != want[0] || cell.Bg.R != want[1] || cell.Attrs != terminal.AttrFg256|terminal.AttrBg256 {
			t.Errorf("cell %d: fg %d bg %d attrs %#x, want fg %d bg %d and palette flags only",
				x, cell.Fg.R, cell.Bg.R, cell.Attrs, want[0], want[1])
		}
	}
}
