package glyph

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
)

// Raster draws a cell grid into an RGBA framebuffer with an Atlas. It is a frame
// sink of the orchestrator's kind: Flush takes a whole frame and draws the cells
// that changed since the last one.
type Raster struct {
	atlas      *Atlas
	cols, rows int
	pix        []byte          // RGBA, cols*Width by rows*Height
	prev       []terminal.Cell // the frame drawn, so an unchanged cell is skipped
}

// Default colors for a cell that asks for the terminal's own
var (
	defaultFg = color.RGB{R: 0xc0, G: 0xc0, B: 0xc0}
	defaultBg = color.RGB{}
)

func NewRaster(a *Atlas) *Raster { return &Raster{atlas: a} }

// ColorMode is truecolor: the compositor never sends the raster a palette index
func (r *Raster) ColorMode() terminal.ColorMode { return terminal.ColorModeTrueColor }

// Sync forgets the frame drawn, so the next one is drawn whole
func (r *Raster) Sync() { r.prev = r.prev[:0] }

// Pix is the framebuffer: RGBA rows of Bounds' width, top row first
func (r *Raster) Pix() []byte { return r.pix }

// Bounds is the framebuffer size in pixels
func (r *Raster) Bounds() (width, height int) {
	return r.cols * r.atlas.Width, r.rows * r.atlas.Height
}

// Flush draws a frame of width by height cells, row-major; a frame short of its
// own size is dropped, as a terminal drops one
func (r *Raster) Flush(cells []terminal.Cell, width, height int) {
	n := width * height
	if width < 0 || height < 0 || len(cells) < n {
		return
	}
	if width != r.cols || height != r.rows {
		r.cols, r.rows = width, height
		r.pix = make([]byte, n*r.atlas.Width*r.atlas.Height*4)
		r.prev = r.prev[:0]
	}
	cells = cells[:n]
	for i, c := range cells {
		if i < len(r.prev) && r.prev[i] == c {
			continue
		}
		r.draw(i%width, i/width, c)
	}
	r.prev = append(r.prev[:0], cells...)
}

// Colors is a cell's foreground and background as a drawer paints them: reverse
// swaps the two and dim halves the foreground; bold and underline have no bitmap
func Colors(c terminal.Cell) (fg, bg color.RGB) {
	fg, bg = cellColor(c.Fg, c.Attrs&terminal.AttrFg256 != 0, c.Attrs&terminal.AttrFgDefault != 0, defaultFg),
		cellColor(c.Bg, c.Attrs&terminal.AttrBg256 != 0, c.Attrs&terminal.AttrBgDefault != 0, defaultBg)
	if c.Attrs&terminal.AttrReverse != 0 {
		fg, bg = bg, fg
	}
	if c.Attrs&terminal.AttrDim != 0 {
		fg = color.Scale(fg, 0.5)
	}
	return fg, bg
}

// draw paints one cell: its background, and its glyph in the foreground where
// the atlas has it
func (r *Raster) draw(cx, cy int, c terminal.Cell) {
	fg, bg := Colors(c)
	rows, _ := r.atlas.Rows(c.Rune)
	w, h := r.atlas.Width, r.atlas.Height
	stride := r.cols * w * 4
	top := r.atlas.Width - 1
	for y := range h {
		var bits uint32
		if y < len(rows) {
			bits = rows[y]
		}
		at := (cy*h+y)*stride + cx*w*4
		for x := range w {
			p := fg
			if bits&(1<<(top-x)) == 0 {
				p = bg
			}
			r.pix[at], r.pix[at+1], r.pix[at+2], r.pix[at+3] = p.R, p.G, p.B, 0xff
			at += 4
		}
	}
}

// cellColor resolves one channel: the terminal's own, a palette index held in R,
// or the RGB as sent
func cellColor(c color.RGB, palette, own bool, def color.RGB) color.RGB {
	switch {
	case own:
		return def
	case palette:
		return palette256(c.R)
	}
	return c
}

// palette256 is xterm's palette: the sixteen system colors as xterm draws them,
// the 6x6x6 cube and the gray ramp
func palette256(i uint8) color.RGB {
	switch {
	case i >= 232:
		v := 8 + 10*(i-232)
		return color.RGB{R: v, G: v, B: v}
	case i >= 16:
		levels := [6]uint8{0, 0x5f, 0x87, 0xaf, 0xd7, 0xff}
		r, g, b := color.CubeRGB256(i)
		return color.RGB{R: levels[r], G: levels[g], B: levels[b]}
	}
	lo, hi := uint8(0), uint8(0xaa)
	if i >= 8 {
		lo, hi = 0x55, 0xff
	}
	channel := func(bit uint8) uint8 {
		if i&bit != 0 {
			return hi
		}
		return lo
	}
	return color.RGB{R: channel(1), G: channel(2), B: channel(4)}
}
