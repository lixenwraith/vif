package glyph

import (
	"os"
	"testing"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
)

const tiny = `
[font]
name = "tiny"
width = 4
height = 2

[[glyph]]
char = "x"
rows = ["#..#", ".##."]

[[glyph]]
char = "\""
rows = ["####", "...."]
`

// TestARasterDrawsTheAtlasInTheCellsColors: a glyph's set bits take the cell's
// foreground and the rest its background, reverse swaps them, a palette index
// resolves to xterm's color, and a cell unchanged since the last frame is kept.
func TestARasterDrawsTheAtlasInTheCellsColors(t *testing.T) {
	a, err := Parse([]byte(tiny))
	if err != nil {
		t.Fatal(err)
	}
	if a.Len() != 2 || a.Width != 4 || a.Height != 2 {
		t.Fatalf("atlas %+v holds %d glyphs", a, a.Len())
	}
	r := NewRaster(a)
	fg, bg := color.RGB{R: 255}, color.RGB{B: 255}
	frame := []terminal.Cell{
		{Rune: 'x', Fg: fg, Bg: bg},
		{Rune: 'x', Fg: fg, Bg: bg, Attrs: terminal.AttrReverse},
		{Rune: ' ', Fg: color.RGB{R: 196}, Attrs: terminal.AttrFg256 | terminal.AttrBg256}, // index 196 is pure red; bg index 0 is black
	}
	r.Flush(frame, 3, 1)
	if w, h := r.Bounds(); w != 12 || h != 2 {
		t.Fatalf("bounds %dx%d, want 12x2", w, h)
	}
	at := func(x, y int) color.RGB {
		i := (y*12 + x) * 4
		return color.RGB{R: r.pix[i], G: r.pix[i+1], B: r.pix[i+2]}
	}
	if at(0, 0) != fg || at(1, 0) != bg || at(1, 1) != fg || at(0, 1) != bg {
		t.Fatalf("cell 0 drew %v %v / %v %v", at(0, 0), at(1, 0), at(0, 1), at(1, 1))
	}
	if at(4, 0) != bg || at(5, 0) != fg {
		t.Fatalf("reversed cell drew %v %v", at(4, 0), at(5, 0))
	}
	if got := at(8, 0); got != (color.RGB{}) {
		t.Fatalf("palette background drew %v, want black", got)
	}

	// The second frame changes one cell; the raster must keep the others as drawn
	frame[2] = terminal.Cell{Rune: '"', Fg: color.RGB{R: 196}, Attrs: terminal.AttrFg256}
	r.Flush(frame, 3, 1)
	if at(8, 0) != (color.RGB{R: 0xff}) || at(0, 0) != fg {
		t.Fatalf("second frame drew %v and kept %v", at(8, 0), at(0, 0))
	}
}

// BenchmarkRasterFlush draws a 120x40 frame of the shipped atlas of which 15 % of
// the cells changed
func BenchmarkRasterFlush(b *testing.B) {
	data, err := os.ReadFile("../../internal/asset/font/mono.toml")
	if err != nil {
		b.Skip("shipped atlas not beside the package")
	}
	a, err := Parse(data)
	if err != nil {
		b.Fatal(err)
	}
	r := NewRaster(a)
	const w, h = 120, 40
	frames := [2][]terminal.Cell{make([]terminal.Cell, w*h), make([]terminal.Cell, w*h)}
	for i := range frames[0] {
		frames[0][i] = terminal.Cell{Rune: 'x', Fg: color.RGB{R: uint8(i)}, Bg: color.RGB{B: 20}}
		frames[1][i] = frames[0][i]
		if i%7 == 0 {
			frames[1][i].Rune = '"'
		}
	}
	r.Flush(frames[0], w, h)
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		r.Flush(frames[i%2], w, h)
	}
}
