package glyph

import (
	"encoding/binary"
	"slices"
	"sync"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
)

// Frames hands the frames a Surface receives to a drawer in another runtime, a Java
// view's thread or a browser page: Flush keeps the newest, Take encodes the cells
// changed since the frame the drawer holds, and Blocked is how long the oldest frame
// not taken has waited. Any goroutine may call any method.
type Frames struct {
	tex    *Texture
	raster *Raster // set, Take hands over the raster's pixels instead of cells

	mu     sync.Mutex
	next   []terminal.Cell // the newest frame flushed
	fw, fh int             // its size
	shown  []terminal.Cell // the frame the drawer holds
	fresh  bool            // next is not taken yet
	whole  bool            // the next take is every cell
	since  time.Time       // when the oldest frame not taken was flushed
	out    []byte          // the encoded frame, reused between takes
}

// NewFrames takes frames for tex's drawer; raster makes Take hand over the pixels
// Raster draws instead, the fallback of a drawer that cannot paint cells
func NewFrames(tex *Texture, raster bool) *Frames {
	f := &Frames{tex: tex}
	if raster {
		f.raster = NewRaster(tex.atlas)
	}
	return f
}

// ColorMode is truecolor: the drawer paints RGB, as the raster does
func (f *Frames) ColorMode() terminal.ColorMode { return terminal.ColorModeTrueColor }

// Flush keeps a frame of width by height cells as the newest, replacing one not yet
// taken; a frame short of its own size is dropped, as a terminal drops one
func (f *Frames) Flush(cells []terminal.Cell, width, height int) {
	n := width * height
	if width < 0 || height < 0 || len(cells) < n {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if width != f.fw || height != f.fh {
		f.fw, f.fh, f.whole = width, height, true
	}
	f.next = append(f.next[:0], cells[:n]...)
	if !f.fresh {
		f.fresh, f.since = true, time.Now() // [wall]
	}
}

// Sync makes the next take every cell
func (f *Frames) Sync() {
	f.mu.Lock()
	f.whole = true
	f.mu.Unlock()
}

// Blocked is how long the oldest frame the drawer has not taken has waited
func (f *Frames) Blocked() time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.since.IsZero() {
		return 0
	}
	return time.Since(f.since) // [wall]
}

// Take encodes the newest frame for the drawer, little-endian 32-bit words: columns,
// rows, then for a cell drawer the count of changed cells and per cell its index,
// slot in the Texture (-1 for background only), foreground and background ARGB; for
// the raster, RGBA rows top first. It is every cell after a resize or a Sync, and
// nil when no cell changed since the frame the drawer holds.
func (f *Frames) Take() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.fresh {
		return nil
	}
	whole := f.whole || len(f.shown) != len(f.next)
	f.fresh, f.whole, f.since = false, false, time.Time{}
	if !whole && slices.Equal(f.shown, f.next) {
		return nil
	}
	if f.raster != nil {
		if whole {
			f.raster.Sync()
		}
		f.raster.Flush(f.next, f.fw, f.fh)
		f.out = appendWords(f.out[:0], uint32(f.fw), uint32(f.fh))
		f.out = append(f.out, f.raster.Pix()...)
	} else {
		f.out = encode(f.out[:0], f.tex, f.fw, f.fh, f.next, f.shown, whole)
	}
	f.shown = append(f.shown[:0], f.next...)
	return f.out
}

// encode appends a cell frame as Take describes it: the cells that differ from
// shown, or all of them
func encode(dst []byte, tex *Texture, cols, rows int, cells, shown []terminal.Cell, whole bool) []byte {
	whole = whole || len(shown) != len(cells)
	at := len(dst)
	dst = appendWords(dst, uint32(cols), uint32(rows), 0)
	n := 0
	for i, c := range cells {
		if !whole && shown[i] == c {
			continue
		}
		fg, bg := Colors(c)
		dst = appendWords(dst, uint32(i), uint32(tex.Slot(c.Rune)), argb(fg), argb(bg))
		n++
	}
	binary.LittleEndian.PutUint32(dst[at+8:], uint32(n))
	return dst
}

func argb(c color.RGB) uint32 {
	return 0xff000000 | uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
}

// Probe is a frame of every glyph in the atlas under each color rule the raster
// applies, with the raster's pixels for it: the oracle a drawer is checked against,
// same frame, same pixels
type Probe struct {
	tex        *Texture
	cols, rows int
	cells      []terminal.Cell
}

// NewProbe lays the probe out cols cells wide
func NewProbe(tex *Texture, cols int) *Probe {
	attrs := [...]terminal.Attr{0, terminal.AttrReverse, terminal.AttrDim,
		terminal.AttrFg256 | terminal.AttrBg256, terminal.AttrFgDefault | terminal.AttrBgDefault}
	runes := append(tex.atlas.Runes(), ' ', '一') // blank, and a rune the atlas lacks
	p := &Probe{tex: tex, cols: max(1, cols)}
	p.rows = (len(runes) + p.cols - 1) / p.cols
	p.cells = make([]terminal.Cell, p.cols*p.rows)
	for i, r := range runes {
		p.cells[i] = terminal.Cell{
			Rune:  r,
			Fg:    color.RGB{R: uint8(i * 37), G: uint8(i * 91), B: uint8(255 - i)},
			Bg:    color.RGB{R: uint8(i * 13), G: 40, B: uint8(i * 7)},
			Attrs: attrs[i%len(attrs)],
		}
	}
	return p
}

// Cells is the probe's frame and its size, to Flush to a Surface
func (p *Probe) Cells() (cells []terminal.Cell, cols, rows int) { return p.cells, p.cols, p.rows }

// Frame is the probe as a whole cell frame, as Take encodes one
func (p *Probe) Frame() []byte {
	return encode(nil, p.tex, p.cols, p.rows, p.cells, nil, true)
}

// Pixels is the frame as Raster draws it, RGBA rows top first
func (p *Probe) Pixels() []byte {
	r := NewRaster(p.tex.atlas)
	r.Flush(p.cells, p.cols, p.rows)
	return r.Pix()
}
