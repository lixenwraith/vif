package game

import (
	"encoding/binary"
	"math"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/glyph"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// eventQueue holds what the view posts while the run's loop is busy; a post never
// waits, since the poster is Android's UI thread
const eventQueue = 256

// cellDP is a cell's height in dp, in whole pixels at the atlas's aspect and never
// under its own: 16x32 on a 420 dpi phone, half again as much map as 9x18 dp shows
const cellDP = 12

// font is the atlas as a cell drawer takes it: one texture, a slot a glyph
type font struct {
	atlas *glyph.Atlas
	mask  []byte
	slots map[rune]int32 // a blank glyph has none and is painted as background
}

func newFont(a *glyph.Atlas) *font {
	f := &font{atlas: a, slots: make(map[rune]int32)}
	f.mask = binary.LittleEndian.AppendUint32(f.mask, uint32(a.Width))
	f.mask = binary.LittleEndian.AppendUint32(f.mask, uint32(a.Height))
	f.mask = binary.LittleEndian.AppendUint32(f.mask, 0)
	for _, r := range a.Runes() {
		rows, _ := a.Rows(r)
		var set uint32
		for _, bits := range rows {
			set |= bits
		}
		if set == 0 {
			continue
		}
		f.slots[r] = int32(len(f.slots))
		for _, bits := range rows {
			for shift := a.Width - 1; shift >= 0; shift-- {
				f.mask = append(f.mask, byte(0xff*(bits>>shift&1)))
			}
		}
	}
	binary.LittleEndian.PutUint32(f.mask[8:], uint32(len(f.slots)))
	return f
}

// view is the activity's SurfaceView as an app.Presentation. A frame waits for the
// drawer to take it, the newest replacing one not yet taken; the grid is the cells
// the view's pixels hold, and the drawer stretches it over them, so a touch maps in
// proportion; touches and keys arrive as terminal events.
type view struct {
	font         *font
	cellW, cellH int           // the pixels a cell takes at least, sized by the density
	raster       *glyph.Raster // the fallback drawer's, nil when a Java drawer takes cells

	events  chan terminal.Event
	away    chan bool
	pointer atomic.Bool

	mu         sync.Mutex
	cols, rows int             // the grid the view's pixels hold
	w, h       int             // those pixels
	next       []terminal.Cell // the newest frame flushed
	fw, fh     int             // its size
	shown      []terminal.Cell // the frame the drawer holds
	fresh      bool            // next is not taken yet
	whole      bool            // the next take is every cell
	since      time.Time       // when the oldest frame not taken was flushed
	out        []byte          // the encoded frame, reused between takes
}

func newView(f *font, raster bool, density float32) *view {
	w := max(f.atlas.Width, int(math.Round(float64(density)*cellDP*float64(f.atlas.Width)/float64(f.atlas.Height))))
	v := &view{font: f, cellW: w, cellH: w * f.atlas.Height / f.atlas.Width,
		events: make(chan terminal.Event, eventQueue), away: make(chan bool, eventQueue)}
	if raster {
		v.raster = glyph.NewRaster(f.atlas)
	}
	return v
}

func (v *view) Name() string           { return "android" }
func (v *view) Dependencies() []string { return nil }
func (v *view) Init() error            { return nil }
func (v *view) Start() error           { return nil }
func (v *view) Stop() error            { return nil }

func (v *view) Surface() render.Surface       { return v }
func (v *view) Events() <-chan terminal.Event { return v.events }
func (v *view) Away() <-chan bool             { return v.away }

func (v *view) CellAspect() vmath.Aspect { return v.font.atlas.Aspect() }

// SetPointerMode drops touches while the run takes no pointer; a finger has no
// motion of its own to report
func (v *view) SetPointerMode(enabled, _ bool) { v.pointer.Store(enabled) }

func (v *view) Size() (int, int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.cols, v.rows
}

// Blocked is how long the oldest frame the drawer has not taken has waited
func (v *view) Blocked() time.Duration {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.since.IsZero() {
		return 0
	}
	return time.Since(v.since) // [wall]
}

// ColorMode is truecolor: the drawer paints RGB, as the raster does
func (v *view) ColorMode() terminal.ColorMode { return terminal.ColorModeTrueColor }

func (v *view) Flush(cells []terminal.Cell, width, height int) {
	n := width * height
	if width < 0 || height < 0 || len(cells) < n {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if width != v.fw || height != v.fh {
		v.fw, v.fh, v.whole = width, height, true
	}
	v.next = append(v.next[:0], cells[:n]...)
	if !v.fresh {
		v.fresh, v.since = true, time.Now() // [wall]
	}
}

func (v *view) Sync() {
	v.mu.Lock()
	v.whole = true
	v.mu.Unlock()
}

// take encodes the newest frame for the drawer: the cells changed since the frame
// it holds, or all of them after a resize or a Sync; nil when none changed
func (v *view) take() []byte {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.fresh {
		return nil
	}
	whole := v.whole || len(v.shown) != len(v.next)
	v.fresh, v.whole, v.since = false, false, time.Time{}
	if !whole && slices.Equal(v.shown, v.next) {
		return nil
	}
	if v.raster != nil {
		if whole {
			v.raster.Sync()
		}
		v.raster.Flush(v.next, v.fw, v.fh)
		v.out = appendWords(v.out[:0], uint32(v.fw), uint32(v.fh))
		v.out = append(v.out, v.raster.Pix()...)
	} else {
		v.out = encode(v.out[:0], v.font.slots, v.fw, v.fh, v.next, v.shown, whole)
	}
	v.shown = append(v.shown[:0], v.next...)
	return v.out
}

// setGrid sizes the grid to the cells the view's pixels hold, at least one each
// way, and reports whether it changed
func (v *view) setGrid(width, height int) bool {
	cols, rows := max(1, width/v.cellW), max(1, height/v.cellH)
	v.mu.Lock()
	defer v.mu.Unlock()
	changed := cols != v.cols || rows != v.rows
	v.cols, v.rows, v.w, v.h = cols, rows, max(1, width), max(1, height)
	return changed
}

func (v *view) setAway(away bool) {
	select {
	case v.away <- away:
	default:
	}
}

// post queues an event for the run; with the queue full the event is lost, which
// a UI thread that must not wait on the game prefers
func (v *view) post(ev terminal.Event) {
	select {
	case v.events <- ev:
	default:
	}
}

// encode appends a cell frame as Frame describes it
func encode(dst []byte, slots map[rune]int32, cols, rows int, cells, shown []terminal.Cell, whole bool) []byte {
	whole = whole || len(shown) != len(cells)
	at := len(dst)
	dst = appendWords(dst, uint32(cols), uint32(rows), 0)
	n := 0
	for i, c := range cells {
		if !whole && shown[i] == c {
			continue
		}
		slot, ok := slots[c.Rune]
		if !ok {
			slot = -1
		}
		fg, bg := glyph.Colors(c)
		dst = appendWords(dst, uint32(i), uint32(slot), argb(fg), argb(bg))
		n++
	}
	binary.LittleEndian.PutUint32(dst[at+8:], uint32(n))
	return dst
}

func appendWords(dst []byte, words ...uint32) []byte {
	for _, w := range words {
		dst = binary.LittleEndian.AppendUint32(dst, w)
	}
	return dst
}

func argb(c color.RGB) uint32 {
	return 0xff000000 | uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
}

// Probe is a frame of every glyph in the atlas under each color rule the raster
// applies, with the raster's pixels for it: the oracle a Java drawer is checked
// against, same frame, same pixels
type Probe struct {
	font       *font
	cols, rows int
	cells      []terminal.Cell
}

// NewProbe lays the probe out cols cells wide
func NewProbe(cols int32) (*Probe, error) {
	f, err := loadFont()
	if err != nil {
		return nil, err
	}
	attrs := [...]terminal.Attr{0, terminal.AttrReverse, terminal.AttrDim,
		terminal.AttrFg256 | terminal.AttrBg256, terminal.AttrFgDefault | terminal.AttrBgDefault}
	runes := append(f.atlas.Runes(), ' ', '一') // blank, and a rune the atlas lacks
	p := &Probe{font: f, cols: max(1, int(cols))}
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
	return p, nil
}

func (p *Probe) Frame() []byte {
	return encode(nil, p.font.slots, p.cols, p.rows, p.cells, nil, true)
}

// Pixels is the frame as glyph.Raster draws it, RGBA rows top first
func (p *Probe) Pixels() []byte {
	r := glyph.NewRaster(p.font.atlas)
	r.Flush(p.cells, p.cols, p.rows)
	return r.Pix()
}
