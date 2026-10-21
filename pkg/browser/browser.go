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
	"sync"
	"sync/atomic"
	"syscall/js"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/pkg/glyph"
	"github.com/lixenwraith/vif/pkg/hostkey"
)

// eventQueue matches the terminal's; a post never waits, since the poster is the
// Worker's event loop, which a blocked callback would hold
const eventQueue = 256

// The Worker's side of the bridge, web/worker.js: vifGrid is the grid the page
// measured before the program started; vifAtlas, vifFrame, vifPointer and vifAudio
// post the texture, a frame, the pointer mode and a PCM buffer to the page; goEvent
// is what the page sends back, one object a message
const (
	jsGrid    = "vifGrid"
	jsAtlas   = "vifAtlas"
	jsFrame   = "vifFrame"
	jsPointer = "vifPointer"
	jsAudio   = "vifAudio"
	jsEvent   = "goEvent"
)

// Host is a Surface over the page. Frames and Sync may be called from any
// goroutine; the page's messages arrive on the Worker's event loop.
type Host struct {
	*glyph.Frames
	tex    *glyph.Texture
	events chan terminal.Event
	grid   atomic.Uint64 // cols<<32 | rows
	event  js.Func
	keys   hostkey.Report

	mu       sync.Mutex
	inflight bool // a frame the page has not reported drawn
}

// Open presents on the page: the texture goes over first, then frames as the page
// takes them. The host lives as long as the Worker, so nothing closes it.
func Open(a *glyph.Atlas) (*Host, error) {
	if a == nil {
		return nil, errors.New("browser: an atlas is required")
	}
	g := js.Global()
	for _, name := range []string{jsAtlas, jsFrame, jsPointer} {
		if g.Get(name).Type() != js.TypeFunction {
			return nil, errors.New("browser: the worker defines no " + name)
		}
	}
	h := &Host{tex: glyph.NewTexture(a), events: make(chan terminal.Event, eventQueue)}
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
	return h, nil
}

func (h *Host) Events() <-chan terminal.Event { return h.events }

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
	js.Global().Call(jsPointer, enabled, motion)
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
// going elsewhere, text, the pointer, a new size or a request for a whole frame
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
			msg.Get("action").Int(), uint8(msg.Get("mods").Int()), msg.Get("touch").Truthy()); ok {
			h.post(ev)
		}
	case "resize":
		cols, rows := max(msg.Get("cols").Int(), 1), max(msg.Get("rows").Int(), 1)
		if c, r := h.Size(); c != cols || r != rows {
			h.setGrid(cols, rows)
			h.post(terminal.Event{Type: terminal.EventResize, Width: cols, Height: rows})
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

// bytes copies b into a Uint8Array of its own, which the Worker may transfer
func bytes(b []byte) js.Value {
	arr := js.Global().Get("Uint8Array").New(len(b))
	js.CopyBytesToJS(arr, b)
	return arr
}
