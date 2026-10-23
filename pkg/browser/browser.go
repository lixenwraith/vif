//go:build js && wasm

// Package browser is a web page as the run's screen and speakers. The Go program
// runs in a Worker; the page draws the cell grid from the frames the Worker hands
// it, one at a time and the newest once the page has drawn the last, plays the PCM
// it posts, and reports its keys, its pointer and its size as the events a
// terminal would.
package browser

import (
	"errors"
	"io"
	"math"
	"sync"
	"sync/atomic"
	"syscall/js"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/pkg/glyph"
	"github.com/lixenwraith/vif/pkg/hostkey"
)

// eventQueue matches the terminal's; a post never waits, since the poster is the
// Worker's event loop, which a blocked callback would hold
const eventQueue = 256

// fitWait bounds Open's wait for the page to fit its grid to the zoom: a round trip
// to the page's thread, so only a page that never answers runs out of it
const fitWait = time.Second

// The Worker's side of the bridge, web/worker.js: vifGrid is the grid the page
// measured before the program started and vifSettings the settings document it
// kept; vifAtlas, vifZoom, vifFrame, vifPointer, vifAudio and vifKeepSettings post
// the texture, the zoom, a frame, the pointer mode, a PCM buffer and the settings
// to keep to the page; goEvent is what the page sends back, one object a message
const (
	jsGrid     = "vifGrid"
	jsSettings = "vifSettings"
	jsAtlas    = "vifAtlas"
	jsZoom     = "vifZoom"
	jsFrame    = "vifFrame"
	jsPointer  = "vifPointer"
	jsAudio    = "vifAudio"
	jsKeep     = "vifKeepSettings"
	jsEvent    = "goEvent"
)

// Config is what the run tells the page: the status bar's rows, whose hold a
// finger starts even with the pointer off, and the zoom, a cell's height in CSS
// pixels the page opens at (0 for its own) and the range and step a zoom keeps to
type Config struct {
	BarRows                             int
	Cell, CellLeast, CellMost, CellStep float64
}

// Host is a Surface over the page. Frames and Sync may be called from any
// goroutine; the page's messages arrive on the Worker's event loop.
type Host struct {
	*glyph.Frames
	tex     *glyph.Texture
	events  chan terminal.Event
	grid    atomic.Uint64 // cols<<32 | rows
	event   js.Func
	keys    hostkey.Report
	barRows int           // the bottom rows a finger still presses with the pointer off
	pointer atomic.Bool   // the run takes pointer events
	covered atomic.Bool   // a keyboard covers the page's bottom rows
	cell    atomic.Uint64 // the zoom, float64 bits
	finger  bool          // a finger's press was taken; the event loop's alone
	fitted  chan struct{} // closed once the page has fitted its grid to the zoom
	fitOnce sync.Once
	away    chan bool // the player left for the page's start screen, or came back

	mu       sync.Mutex
	inflight bool // a frame the page has not reported drawn
}

// Open presents on the page: the texture and the zoom go over first, then frames
// as the page takes them. The host lives as long as the Worker, so nothing closes it.
// The grid the page measured at its own cell is not the one it shows at a zoom kept
// from an earlier visit, and a joiner reports its size before the run takes a
// resize, so Open returns once the page has fitted the zoom and answered: the run
// starts at, and a dedicated host sizes a shared map from, the grid on screen.
func Open(a *glyph.Atlas, c Config) (*Host, error) {
	if a == nil {
		return nil, errors.New("browser: an atlas is required")
	}
	g := js.Global()
	for _, name := range []string{jsAtlas, jsZoom, jsFrame, jsPointer} {
		if g.Get(name).Type() != js.TypeFunction {
			return nil, errors.New("browser: the worker defines no " + name)
		}
	}
	h := &Host{tex: glyph.NewTexture(a), events: make(chan terminal.Event, eventQueue), barRows: c.BarRows,
		fitted: make(chan struct{}), away: make(chan bool, eventQueue)}
	h.cell.Store(math.Float64bits(c.Cell))
	h.keys.Post = h.post
	h.Frames = glyph.NewFrames(h.tex, false)
	cols, rows := 80, 24
	if grid := g.Get(jsGrid); grid.Type() == js.TypeObject {
		cols, rows = grid.Get("cols").Int(), grid.Get("rows").Int()
	}
	h.setGrid(cols, rows)
	h.event = js.FuncOf(h.handle)
	g.Set(jsEvent, h.event)
	g.Call(jsAtlas, bytes(h.tex.Bytes()))
	g.Call(jsZoom, c.Cell, c.CellLeast, c.CellMost, c.CellStep)
	select {
	case <-h.fitted:
	case <-time.After(fitWait): // [wall] a page that never answers starts at its own grid
	}
	return h, nil
}

func (h *Host) Events() <-chan terminal.Event { return h.events }

// Away reports the player leaving for the page's start screen and returning, as
// Android reports its app sent to the background: a solo run waits meanwhile
func (h *Host) Away() <-chan bool { return h.away }

// Size is the grid of whole cells the page holds
func (h *Host) Size() (cols, rows int) {
	g := h.grid.Load()
	return int(g >> 32), int(uint32(g))
}

func (h *Host) setGrid(cols, rows int) {
	h.grid.Store(uint64(max(cols, 1))<<32 | uint64(uint32(max(rows, 1))))
}

// Flush keeps the frame as the newest and hands it to the page if none is on its way
func (h *Host) Flush(cells []terminal.Cell, width, height int) {
	h.Frames.Flush(cells, width, height)
	h.present()
}

// SetKeyReport asks for each key's release, which the page's keyups give; a key
// the page has no keyup for, typed on a soft keyboard or the key strip, lets go as
// it presses
func (h *Host) SetKeyReport(report terminal.KeyReport, allow terminal.KeySource) error {
	h.keys.Ask(report, allow, true)
	return nil
}

// SetPointerMode tells the page which pointer events to report: none, buttons and
// drags, or every motion as well
func (h *Host) SetPointerMode(enabled, motion bool) {
	h.pointer.Store(enabled)
	js.Global().Call(jsPointer, enabled, motion)
}

// Covered reports a keyboard over the page's bottom rows, where the page draws the
// last row's left half again above it
func (h *Host) Covered() bool { return h.covered.Load() }

// Cell is the zoom the page was left at, a cell's height in CSS pixels, 0 for its own
func (h *Host) Cell() float64 { return math.Float64frombits(h.cell.Load()) }

// admit is the pointer mode applied to what the page reports, as Android's view
// applies it: with the pointer off, a finger still presses on the status bar, the
// hold that opens the menu where the mouse is enabled again, and lifts from a press
// that was taken
func (h *Host) admit(ev terminal.Event) bool {
	switch {
	case ev.MouseAction == terminal.MouseActionPress && ev.MouseBtn == terminal.MouseBtnLeft:
		_, rows := h.Size()
		h.finger = h.pointer.Load() || ev.Touch && ev.MouseY >= rows-h.barRows
		return h.finger
	case ev.MouseAction == terminal.MouseActionRelease && ev.MouseBtn == terminal.MouseBtnLeft:
		taken := h.finger || h.pointer.Load()
		h.finger = false
		return taken
	}
	return h.pointer.Load()
}

// present hands the page the newest frame once the one it was handed is drawn
func (h *Host) present() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.inflight {
		return
	}
	frame := h.Take()
	if frame == nil {
		return
	}
	h.inflight = true
	js.Global().Call(jsFrame, bytes(frame))
}

// handle takes what the page sent: a frame drawn, a key going down or up, its keys
// going elsewhere, text, the pointer, a keyboard over it, a zoom, a new size, the
// grid fitted to the zoom Open sent, the player leaving or returning, or a request
// for a whole frame
func (h *Host) handle(_ js.Value, args []js.Value) any {
	if len(args) == 0 {
		return nil
	}
	msg := args[0]
	switch msg.Get("type").String() {
	case "drawn":
		h.mu.Lock()
		h.inflight = false
		h.mu.Unlock()
		h.present()
	case "key":
		if ev, ok := keyEvent(msg.Get("key").String(), uint8(msg.Get("mods").Int())); ok {
			if code := msg.Get("code"); code.Type() == js.TypeString && code.String() != "" {
				h.keys.Down(keyCode(code.String()), ev, msg.Get("repeat").Truthy())
			} else {
				h.keys.Tap(ev)
			}
		}
	case "keyup":
		h.keys.Up(keyCode(msg.Get("code").String()))
	case "blur":
		h.keys.Lost()
	case "text":
		for _, r := range msg.Get("text").String() {
			h.keys.Tap(textEvent(r))
		}
	case "mouse":
		if ev, ok := mouseEvent(msg.Get("x").Int(), msg.Get("y").Int(), msg.Get("button").Int(),
			msg.Get("action").Int(), uint8(msg.Get("mods").Int()), msg.Get("touch").Truthy()); ok && h.admit(ev) {
			h.post(ev)
		}
	case "covered":
		h.covered.Store(msg.Get("covered").Truthy())
	case "zoom":
		h.cell.Store(math.Float64bits(msg.Get("cell").Float()))
	case "resize":
		cols, rows := max(msg.Get("cols").Int(), 1), max(msg.Get("rows").Int(), 1)
		if c, r := h.Size(); c != cols || r != rows {
			h.setGrid(cols, rows)
			h.post(terminal.Event{Type: terminal.EventResize, Width: cols, Height: rows})
		}
	case "fitted": // after the resize the fit sent, if it changed the grid
		h.fitOnce.Do(func() { close(h.fitted) })
	case "away":
		select {
		case h.away <- msg.Get("away").Truthy():
		default:
		}
	case "sync":
		h.Sync()
	}
	return nil
}

// post queues an event for the run; with the queue full the event is lost, which an
// event loop that must not wait on the game prefers
func (h *Host) post(ev terminal.Event) {
	select {
	case h.events <- ev:
	default:
	}
}

// Speaker is the page's speakers, the writer the mixer streams to: each buffer goes
// to the page but a silent one, so a muted game posts nothing and the page opens no
// AudioContext until a sound plays. Nil when the worker defines no vifAudio.
func Speaker() io.WriteCloser {
	if js.Global().Get(jsAudio).Type() != js.TypeFunction {
		return nil
	}
	return speaker{}
}

type speaker struct{}

func (speaker) Write(p []byte) (int, error) {
	for _, b := range p {
		if b != 0 {
			js.Global().Call(jsAudio, bytes(p))
			break
		}
	}
	return len(p), nil
}

func (speaker) Close() error { return nil }

// Settings is the settings document the page kept from an earlier visit, nil for none
func Settings() []byte {
	if doc := js.Global().Get(jsSettings); doc.Type() == js.TypeString && doc.String() != "" {
		return []byte(doc.String())
	}
	return nil
}

// KeepSettings hands the page the settings document to keep for the next visit
func KeepSettings(doc []byte) {
	if keep := js.Global().Get(jsKeep); keep.Type() == js.TypeFunction {
		keep.Invoke(string(doc))
	}
}

// bytes copies b into a Uint8Array of its own, which the Worker may transfer
func bytes(b []byte) js.Value {
	arr := js.Global().Get("Uint8Array").New(len(b))
	js.CopyBytesToJS(arr, b)
	return arr
}
