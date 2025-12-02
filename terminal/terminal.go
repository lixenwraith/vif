package terminal

import (
	"io"
	"os"
	"sync"
	"sync/atomic"

	"golang.org/x/term"
)

// Attr represents text attributes (bitmask)
type Attr uint8

const (
	AttrNone      Attr = 0
	AttrBold      Attr = 1 << 0
	AttrDim       Attr = 1 << 1
	AttrItalic    Attr = 1 << 2
	AttrUnderline Attr = 1 << 3
	AttrBlink     Attr = 1 << 4
	AttrReverse   Attr = 1 << 5
	AttrFg256     Attr = 1 << 6 // Fg.R is 256-color palette index
	AttrBg256     Attr = 1 << 7 // Bg.R is 256-color palette index
)

// AttrStyle masks only the style bits (excludes color mode flags)
const AttrStyle Attr = AttrBold | AttrDim | AttrItalic | AttrUnderline | AttrBlink | AttrReverse

// Cell represents a single terminal cell
type Cell struct {
	Rune  rune
	Fg    RGB
	Bg    RGB
	Attrs Attr
}

// Terminal provides low-level terminal access
type Terminal interface {
	// Init enters raw mode, alternate screen buffer, hides cursor
	Init() error

	// Fini restores terminal state. Safe to call multiple times.
	Fini()

	// Size returns current terminal dimensions
	Size() (width, height int)

	// ResizeChan returns channel that receives resize events
	ResizeChan() <-chan ResizeEvent

	// ColorMode returns detected color capability
	ColorMode() ColorMode

	// Flush writes cell buffer to terminal
	// Cells are row-major: cells[y*width + x]
	Flush(cells []Cell, width, height int)

	// Clear fills screen with specified background color
	Clear(bg RGB)

	// SetCursorVisible shows/hides cursor
	SetCursorVisible(visible bool)

	// MoveCursor positions cursor (0-indexed)
	MoveCursor(x, y int)

	// Sync forces full redraw
	Sync()

	// PollEvent blocks until next input event
	PollEvent() Event

	// PostEvent injects a synthetic event
	PostEvent(Event)
}

// term implements Terminal
type termImpl struct {
	in  *os.File
	out *os.File

	inFd  int
	outFd int

	oldState *term.State // Original terminal state

	colorMode ColorMode
	width     int
	height    int

	output      *outputBuffer
	input       *inputReader
	resize      *resizeHandler
	syntheticCh chan Event

	cursorVisible atomic.Bool

	mu          sync.Mutex
	initialized bool
	finalized   bool
}

// New creates a new Terminal instance
func New() Terminal {
	return &termImpl{
		in:          os.Stdin,
		out:         os.Stdout,
		inFd:        int(os.Stdin.Fd()),
		outFd:       int(os.Stdout.Fd()),
		syntheticCh: make(chan Event, 16),
	}
}

// NewWithFiles creates a Terminal with custom input/output files
func NewWithFiles(in, out *os.File) Terminal {
	return &termImpl{
		in:          in,
		out:         out,
		inFd:        int(in.Fd()),
		outFd:       int(out.Fd()),
		syntheticCh: make(chan Event, 16),
	}
}

// Init enters raw mode and sets up terminal
func (t *termImpl) Init() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.initialized {
		return nil
	}

	// Detect color mode
	t.colorMode = DetectColorMode()

	// Get initial size
	t.width, t.height = getTerminalSize(t.outFd)

	// Enter raw mode
	oldState, err := term.MakeRaw(t.inFd)
	if err != nil {
		return err
	}
	t.oldState = oldState

	// Create output buffer
	t.output = newOutputBuffer(t.out, t.colorMode)
	t.output.resize(t.width, t.height)

	// Create input reader
	t.input = newInputReader(t.inFd)

	// Create resize handler
	t.resize = newResizeHandler(t.outFd)

	// Enter alternate screen, hide cursor
	t.writeRaw(csiAltScreenEnter)
	t.writeRaw(csiCursorHide)
	t.cursorVisible.Store(false)

	// Clear screen
	t.output.clear(RGBBlack)

	// Start input and resize handlers
	t.input.start()
	t.resize.start()

	t.initialized = true
	return nil
}

// Fini restores terminal state
func (t *termImpl) Fini() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.initialized || t.finalized {
		return
	}

	// Stop handlers
	if t.input != nil {
		t.input.stop()
	}
	if t.resize != nil {
		t.resize.stop()
	}

	// Show cursor
	t.writeRaw(csiCursorShow)

	// Exit alternate screen
	t.writeRaw(csiAltScreenExit)

	// Reset attributes
	t.writeRaw(csiSGR0)

	// Restore terminal state
	if t.oldState != nil {
		term.Restore(t.inFd, t.oldState)
	}

	t.finalized = true
}

// Size returns current terminal dimensions
func (t *termImpl) Size() (int, int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.width, t.height
}

// ResizeChan returns the resize event channel
func (t *termImpl) ResizeChan() <-chan ResizeEvent {
	return t.resize.events()
}

// ColorMode returns detected color capability
func (t *termImpl) ColorMode() ColorMode {
	return t.colorMode
}

// Flush writes cell buffer to terminal
// Holds lock for entire operation to prevent race with Clear/MoveCursor
func (t *termImpl) Flush(cells []Cell, width, height int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.initialized || t.finalized {
		return
	}

	if width != t.width || height != t.height {
		t.width = width
		t.height = height
	}

	t.output.flush(cells, width, height)
}

// Clear fills screen with background color
func (t *termImpl) Clear(bg RGB) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.initialized || t.finalized {
		return
	}

	t.output.clear(bg)
}

// SetCursorVisible shows/hides cursor
func (t *termImpl) SetCursorVisible(visible bool) {
	if t.cursorVisible.Swap(visible) == visible {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.initialized || t.finalized {
		return
	}

	w := t.output.writer
	if visible {
		w.Write(csiCursorShow)
	} else {
		w.Write(csiCursorHide)
	}
	w.Flush()
}

// MoveCursor positions cursor (0-indexed)
func (t *termImpl) MoveCursor(x, y int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.initialized || t.finalized {
		return
	}

	if t.output != nil {
		t.output.invalidateCursor()
	}

	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if x >= t.width {
		x = t.width - 1
	}
	if y >= t.height {
		y = t.height - 1
	}

	// Write through buffered writer to maintain stream order
	w := t.output.writer
	writeCursorPos(w, x, y)
	w.Flush()
}

// Sync forces full redraw
func (t *termImpl) Sync() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.initialized || t.finalized {
		return
	}

	w, h := getTerminalSize(t.outFd)
	t.width = w
	t.height = h

	t.output.resize(w, h)
	t.output.forceFullRedraw()
}

// PollEvent blocks until next input event
func (t *termImpl) PollEvent() Event {
	// Check synthetic events first
	select {
	case ev := <-t.syntheticCh:
		return ev
	default:
	}

	// Wait for input or resize
	select {
	case ev := <-t.syntheticCh:
		return ev
	case ev := <-t.input.events():
		return ev
	case re := <-t.resize.events():
		t.mu.Lock()
		t.width = re.Width
		t.height = re.Height
		t.mu.Unlock()
		return Event{
			Type:   EventResize,
			Width:  re.Width,
			Height: re.Height,
		}
	}
}

// PostEvent injects a synthetic event
func (t *termImpl) PostEvent(ev Event) {
	select {
	case t.syntheticCh <- ev:
	default:
		// Channel full, drop
	}
}

// writeRaw writes raw bytes to output
func (t *termImpl) writeRaw(data []byte) {
	t.out.Write(data)
}

// EmergencyReset attempts to restore terminal to sane state
// Call this from panic recovery if Fini() cannot be called normally
func EmergencyReset(w io.Writer) {
	w.Write(csiCursorShow)
	w.Write(csiAltScreenExit)
	w.Write(csiSGR0)
	w.Write(csiRIS) // Full reset as last resort
}