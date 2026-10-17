//go:build vif_gui && (linux || freebsd)

package window

import (
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/pkg/glyph"
)

// TestAKeyPressReportsWhatATerminalReports: a keysym and its modifiers become the
// event the terminal module parses from the bytes a terminal sends for that key
func TestAKeyPressReportsWhatATerminalReports(t *testing.T) {
	const shift, ctrl, alt = stateShift, stateControl, stateMod1
	key := func(k terminal.Key, m terminal.Modifier) terminal.Event {
		return terminal.Event{Type: terminal.EventKey, Key: k, Modifiers: m}
	}
	char := func(r rune, m terminal.Modifier) terminal.Event {
		return terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: r, Modifiers: m}
	}
	for _, c := range []struct {
		keysym uint64
		state  uint32
		want   terminal.Event
	}{
		{'a', 0, char('a', 0)},
		{'A', shift, char('A', 0)},
		{' ', 0, char(' ', 0)},
		{'x', alt, char('x', terminal.ModAlt)},
		{0x010003bb, 0, char('λ', 0)},
		{'c', ctrl, key(terminal.KeyCtrlC, 0)},
		{'C', ctrl | shift, key(terminal.KeyCtrlC, 0)},
		{'h', ctrl, key(terminal.KeyBackspace, 0)},
		{'[', ctrl, key(terminal.KeyEscape, 0)},
		{0xff0d, 0, key(terminal.KeyEnter, 0)},
		{0xff1b, 0, key(terminal.KeyEscape, 0)},
		{0xfe20, shift, key(terminal.KeyBacktab, terminal.ModShift)},
		{0xff52, shift, key(terminal.KeyUp, terminal.ModShift)},
		{0xffc9, ctrl | alt, key(terminal.KeyF12, terminal.ModCtrl|terminal.ModAlt)},
	} {
		if got, ok := keyEvent(c.keysym, c.state); !ok || got != c.want {
			t.Errorf("keysym %#x state %#x: %+v %v, want %+v", c.keysym, c.state, got, ok, c.want)
		}
	}
	if ev, ok := keyEvent(0xffe1, shift); ok { // Shift_L alone
		t.Errorf("a modifier alone reported %+v", ev)
	}
}

// TestAPixelMapsToTheCellDrawnUnderIt: at scale 2 a cell covers twice the atlas's
// pixels each way, the frame drawn centred, and a pixel in the margin takes the
// nearest edge cell
func TestAPixelMapsToTheCellDrawnUnderIt(t *testing.T) {
	atlas, err := glyph.Parse([]byte("[font]\nwidth = 4\nheight = 8\n"))
	if err != nil {
		t.Fatal(err)
	}
	w := newWindow(Config{Atlas: atlas, Cols: 10, Rows: 5, Scale: 2})
	w.raster.Flush(make([]terminal.Cell, 10*5), 10, 5)
	w.winW, w.winH = 10*8+6, 5*16+4 // a margin of 3 pixels left and right, 2 above and below
	for _, c := range []struct{ px, py, x, y int }{
		{3, 2, 0, 0}, {3 + 8*4 + 7, 2 + 16*2 + 15, 4, 2}, {3 + 8*5, 2 + 16, 5, 1},
		{0, 0, 0, 0}, {w.winW - 1, w.winH - 1, 9, 4},
	} {
		if x, y := w.cellAt(c.px, c.py); x != c.x || y != c.y {
			t.Errorf("pixel %d,%d is cell %d,%d, want %d,%d", c.px, c.py, x, y, c.x, c.y)
		}
	}
}

// BenchmarkPresent times a frame through the window: copy, raster, whole-texture
// upload, draw and swap, finished on the GPU, at 15 % and at every cell changed.
// Needs an X display with EGL and GLES 2.
func BenchmarkPresent(b *testing.B) {
	if os.Getenv("DISPLAY") == "" {
		b.Skip("no X display")
	}
	data, err := os.ReadFile("../../internal/asset/font/mono.toml")
	if err != nil {
		b.Skip("shipped atlas not beside the package")
	}
	atlas, err := glyph.Parse(data)
	if err != nil {
		b.Fatal(err)
	}
	if err := load(); err != nil {
		b.Skip(err)
	}
	for _, grid := range [][2]int{{120, 40}, {200, 60}} {
		for _, every := range []int{7, 1} {
			b.Run(fmt.Sprintf("%dx%d/changed=%d%%", grid[0], grid[1], 100/every), func(b *testing.B) {
				runtime.LockOSThread()
				defer runtime.UnlockOSThread()
				cfg := Config{Title: "bench", Atlas: atlas, Cols: grid[0], Rows: grid[1]}
				w := newWindow(cfg)
				if err := w.open(cfg); err != nil {
					b.Skip(err)
				}
				defer w.release()
				eglSwapInterval(w.gpu.display, 0) // the work, not the display's refresh
				frames := gameFrames(grid[0]*grid[1], every)
				w.Flush(frames[0], grid[0], grid[1])
				w.present()
				b.ResetTimer()
				for i := range b.N {
					w.Flush(frames[(i+1)%2], grid[0], grid[1])
					w.present()
					glFinish()
				}
			})
		}
	}
}

// gameFrames are two frames where every cell has a glyph and a background, as a
// game frame does, differing in one cell of every
func gameFrames(n, every int) [2][]terminal.Cell {
	frames := [2][]terminal.Cell{make([]terminal.Cell, n), make([]terminal.Cell, n)}
	for i := range n {
		frames[0][i] = terminal.Cell{Rune: rune('!' + i%90), Fg: color.RGB{R: uint8(i), G: 200}, Bg: color.RGB{B: uint8(i / 7)}}
		frames[1][i] = frames[0][i]
		if i%every == 0 {
			frames[1][i].Rune, frames[1][i].Fg = '#', color.RGB{G: uint8(i)}
		}
	}
	return frames
}
