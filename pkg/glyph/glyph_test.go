package glyph

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
	"time"

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

// TestTheDrawerTakesTheNewestFrameAsChangesFromTheOneItHolds: a frame not taken is
// replaced by the next, the take carries every cell that differs from what the
// drawer holds and nothing when none does, a resize or a Sync makes it whole, and
// Blocked counts from the oldest frame waiting.
func TestTheDrawerTakesTheNewestFrameAsChangesFromTheOneItHolds(t *testing.T) {
	a, err := Parse([]byte(tiny))
	if err != nil {
		t.Fatal(err)
	}
	f := NewFrames(NewTexture(a), false)
	if f.Take() != nil || f.Blocked() != 0 {
		t.Fatal("frames nothing was flushed to hold one")
	}
	cells := make([]terminal.Cell, 4*2)
	for i := range cells {
		cells[i] = terminal.Cell{Rune: 'x', Fg: color.RGB{R: 200}}
	}
	f.Flush(cells, 4, 2)
	if n := records(t, f.Take()); n != 8 {
		t.Fatalf("the first frame carried %d cells, want all 8", n)
	}

	cells[1].Rune = '"'
	f.Flush(cells, 4, 2)
	time.Sleep(5 * time.Millisecond)
	cells[1].Rune, cells[6].Rune = 'x', ' ' // the drawer never saw the quote
	f.Flush(cells, 4, 2)
	if f.Blocked() < 5*time.Millisecond {
		t.Fatalf("blocked %v counts from the newest frame, not the oldest waiting", f.Blocked())
	}
	if n := records(t, f.Take()); n != 1 {
		t.Fatalf("the take carried %d cells, want the one that differs from the drawer's", n)
	}
	if f.Take() != nil || f.Blocked() != 0 {
		t.Fatal("a frame was taken twice")
	}

	f.Flush(cells, 4, 2)
	if fr := f.Take(); fr != nil {
		t.Fatalf("a frame the drawer already holds was handed over again: %d cells", records(t, fr))
	}
	f.Flush(cells, 4, 2)
	f.Sync()
	if n := records(t, f.Take()); n != 8 {
		t.Fatalf("after Sync the take carried %d cells, want all 8", n)
	}
	f.Flush(cells[:6], 3, 2)
	if n := records(t, f.Take()); n != 6 {
		t.Fatalf("after a resize the take carried %d cells, want all 6", n)
	}
}

func records(t *testing.T, frame []byte) int {
	t.Helper()
	if len(frame) < 12 {
		t.Fatalf("frame of %d bytes has no header", len(frame))
	}
	n := int(binary.LittleEndian.Uint32(frame[8:]))
	if len(frame) != 12+16*n {
		t.Fatalf("frame of %d bytes holds %d records", len(frame), n)
	}
	return n
}

// TestACellFramePaintedFromTheTextureIsTheRastersPixels: painting each record's
// background and then its glyph from the texture in its foreground, as a cell
// drawer does, gives the raster's pixels for the probe and for a live frame; the
// texture names each slot's rune after the masks.
func TestACellFramePaintedFromTheTextureIsTheRastersPixels(t *testing.T) {
	a, err := Parse([]byte(tiny))
	if err != nil {
		t.Fatal(err)
	}
	tex := NewTexture(a)
	mask := tex.Bytes()
	if n := binary.LittleEndian.Uint32(mask[8:]); n != 2 || len(mask) != 12+2*4*2+2*4 {
		t.Fatalf("texture of %d bytes holds %d slots", len(mask), n)
	}
	if r := rune(binary.LittleEndian.Uint32(mask[len(mask)-8:])); tex.Slot(r) != 0 || tex.Slot('y') != -1 {
		t.Fatalf("slot 0 is %q, slot of y is %d", r, tex.Slot('y'))
	}
	p := NewProbe(tex, 3)
	if got, want := paint(t, mask, p.Frame()), p.Pixels(); !bytes.Equal(got, want) {
		t.Fatal("the probe painted from the texture differs from the raster")
	}
	cell, raster := NewFrames(tex, false), NewFrames(tex, true)
	for _, f := range []*Frames{cell, raster} {
		f.Flush(p.Cells())
	}
	frame := raster.Take()
	if got := paint(t, mask, cell.Take()); !bytes.Equal(got, frame[8:]) {
		t.Fatal("a live cell frame differs from the raster drawer's")
	}
}

// paint is a cell drawer in Go: RGBA from a whole cell frame and the texture
func paint(t *testing.T, mask, frame []byte) []byte {
	t.Helper()
	word := func(b []byte, i int) uint32 { return binary.LittleEndian.Uint32(b[4*i:]) }
	w, h := int(word(mask, 0)), int(word(mask, 1))
	cols, rows, n := int(word(frame, 0)), int(word(frame, 1)), int(word(frame, 2))
	if n != cols*rows {
		t.Fatalf("frame of %d records for %dx%d cells", n, cols, rows)
	}
	pix := make([]byte, cols*w*rows*h*4)
	for r := range n {
		idx, slot, fg, bg := int(word(frame, 3+4*r)), int32(word(frame, 4+4*r)), word(frame, 5+4*r), word(frame, 6+4*r)
		for y := range h {
			for x := range w {
				c := bg
				if slot >= 0 && mask[12+(int(slot)*h+y)*w+x] != 0 {
					c = fg
				}
				at := (((idx/cols)*h+y)*cols*w + (idx%cols)*w + x) * 4
				pix[at], pix[at+1], pix[at+2], pix[at+3] = byte(c>>16), byte(c>>8), byte(c), byte(c>>24)
			}
		}
	}
	return pix
}
