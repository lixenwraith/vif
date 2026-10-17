package game

import (
	"math"
	"sync"
	"sync/atomic"

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

// view is the activity's SurfaceView as an app.Presentation. Frames wait in the
// embedded glyph.Frames for the drawer to take them; the grid is the cells the
// view's pixels hold, and the drawer stretches it over them, so a touch maps in
// proportion; touches and keys arrive as terminal events.
type view struct {
	*glyph.Frames
	tex          *glyph.Texture
	cellW, cellH int // the pixels a cell takes at least, sized by the density

	events  chan terminal.Event
	away    chan bool
	pointer atomic.Bool
	finger  struct { // the cell a pressed finger last reported; the UI thread's alone
		down bool
		x, y int
	}

	mu         sync.Mutex
	cols, rows int // the grid the view's pixels hold
	w, h       int // those pixels
}

func newView(tex *glyph.Texture, raster bool, density float32) *view {
	a := tex.Atlas()
	w := max(a.Width, int(math.Round(float64(density)*cellDP*float64(a.Width)/float64(a.Height))))
	return &view{Frames: glyph.NewFrames(tex, raster), tex: tex, cellW: w, cellH: w * a.Height / a.Width,
		events: make(chan terminal.Event, eventQueue), away: make(chan bool, eventQueue)}
}

func (v *view) Name() string           { return "android" }
func (v *view) Dependencies() []string { return nil }
func (v *view) Init() error            { return nil }
func (v *view) Start() error           { return nil }
func (v *view) Stop() error            { return nil }

func (v *view) Surface() render.Surface       { return v }
func (v *view) Events() <-chan terminal.Event { return v.events }
func (v *view) Away() <-chan bool             { return v.away }

func (v *view) CellAspect() vmath.Aspect { return v.tex.Atlas().Aspect() }

// SetPointerMode drops touches while the run takes no pointer; a finger has no
// motion of its own to report
func (v *view) SetPointerMode(enabled, _ bool) { v.pointer.Store(enabled) }

func (v *view) Size() (int, int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.cols, v.rows
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

// Probe is glyph.Probe over the shipped atlas: the oracle the instrumented test
// checks a Java drawer against, same frame, same pixels
type Probe struct{ probe *glyph.Probe }

// NewProbe lays the probe out cols cells wide
func NewProbe(cols int32) (*Probe, error) {
	tex, err := loadTexture()
	if err != nil {
		return nil, err
	}
	return &Probe{glyph.NewProbe(tex, int(cols))}, nil
}

// Frame is the probe as a whole cell frame
func (p *Probe) Frame() []byte { return p.probe.Frame() }

// Pixels is the frame as glyph.Raster draws it, RGBA rows top first
func (p *Probe) Pixels() []byte { return p.probe.Pixels() }
