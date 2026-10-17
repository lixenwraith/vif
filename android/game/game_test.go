package game

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
)

func testView(t *testing.T, raster bool) *view {
	t.Helper()
	f, err := loadFont()
	if err != nil {
		t.Fatal(err)
	}
	return newView(f, raster)
}

// TestATapPressesAndReleasesTheCellUnderIt: pixels map to the cell the drawer paints
// there; a press off the grid is dropped and its release kept on the edge; nothing
// is reported while the run takes no pointer.
func TestATapPressesAndReleasesTheCellUnderIt(t *testing.T) {
	v := testView(t, false)
	v.setGrid(10*12+5, 4*24+20) // a margin short of a cell on each side
	if ev, ok := v.touch(30, 30, true); ok {
		t.Fatalf("a touch with the pointer off was reported: %+v", ev)
	}
	v.SetPointerMode(true, false)
	ev, ok := v.touch(12*3+11, 24*2, true)
	want := terminal.Event{Type: terminal.EventMouse, MouseBtn: terminal.MouseBtnLeft,
		MouseAction: terminal.MouseActionPress, MouseX: 3, MouseY: 2}
	if !ok || ev != want {
		t.Fatalf("press is %+v %v, want %+v", ev, ok, want)
	}
	if ev, ok := v.touch(12*10+2, 10, true); ok {
		t.Fatalf("a press in the margin was reported: %+v", ev)
	}
	ev, ok = v.touch(12*10+2, 24*4+3, false)
	want.MouseAction, want.MouseX, want.MouseY = terminal.MouseActionRelease, 9, 3
	if !ok || ev != want {
		t.Fatalf("release in the margin is %+v %v, want %+v", ev, ok, want)
	}
}

// TestKeysArriveAsATerminalReportsThem: a named key, Ctrl with a letter as its
// control byte, Shift-Tab as back-tab and a typed character are the events a
// terminal parses for them; a key the game has no use for is left to the system.
func TestKeysArriveAsATerminalReportsThem(t *testing.T) {
	key := func(k terminal.Key, mods terminal.Modifier) terminal.Event {
		return terminal.Event{Type: terminal.EventKey, Key: k, Modifiers: mods}
	}
	for _, c := range []struct {
		name       string
		code, meta int32
		char       rune
		want       terminal.Event
	}{
		{"back", 4, 0, 0, key(terminal.KeyEscape, 0)},
		{"enter", 66, 0, '\n', key(terminal.KeyEnter, 0)},
		{"backspace", 67, 0, 0, key(terminal.KeyBackspace, 0)},
		{"shift tab", 61, metaShift, '\t', key(terminal.KeyBacktab, terminal.ModShift)},
		{"ctrl c", 31, metaCtrl, 'c', key(terminal.KeyCtrlC, 0)},
		{"ctrl h", 36, metaCtrl, 'h', key(terminal.KeyBackspace, 0)},
		{"ctrl space", 62, metaCtrl, ' ', key(terminal.KeyCtrlSpace, 0)},
		{"shifted letter", 29, metaShift, 'A', terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: 'A'}},
		{"alt letter", 29, metaAlt, 'a', terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: 'a', Modifiers: terminal.ModAlt}},
		{"space", 62, 0, ' ', terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: ' '}},
	} {
		if got, ok := keyEvent(c.code, c.char, c.meta); !ok || got != c.want {
			t.Errorf("%s: %+v %v, want %+v", c.name, got, ok, c.want)
		}
	}
	if ev, ok := keyEvent(24, 0, 0); ok { // KEYCODE_VOLUME_UP
		t.Errorf("volume up was taken as %+v", ev)
	}
}

// TestTheDrawerTakesTheNewestFrameAsChangesFromTheOneItHolds: a frame not taken is
// replaced by the next, the take carries every cell that differs from what the
// drawer holds, a resize or a Sync makes it whole, and Blocked counts from the
// oldest frame waiting.
func TestTheDrawerTakesTheNewestFrameAsChangesFromTheOneItHolds(t *testing.T) {
	v := testView(t, false)
	if v.take() != nil || v.Blocked() != 0 {
		t.Fatal("a view nothing was flushed to has a frame")
	}
	cells := make([]terminal.Cell, 4*2)
	for i := range cells {
		cells[i] = terminal.Cell{Rune: 'a', Fg: color.RGB{R: 200}}
	}
	v.Flush(cells, 4, 2)
	if n := records(t, v.take()); n != 8 {
		t.Fatalf("the first frame carried %d cells, want all 8", n)
	}

	cells[1].Rune = 'b'
	v.Flush(cells, 4, 2)
	time.Sleep(5 * time.Millisecond)
	cells[1].Rune, cells[6].Rune = 'a', 'c' // the drawer never saw 'b'
	v.Flush(cells, 4, 2)
	if v.Blocked() < 5*time.Millisecond {
		t.Fatalf("blocked %v counts from the newest frame, not the oldest waiting", v.Blocked())
	}
	if n := records(t, v.take()); n != 1 {
		t.Fatalf("the take carried %d cells, want the one that differs from the drawer's", n)
	}
	if v.take() != nil || v.Blocked() != 0 {
		t.Fatal("a frame was taken twice")
	}

	v.Flush(cells, 4, 2)
	v.Sync()
	if n := records(t, v.take()); n != 8 {
		t.Fatalf("after Sync the take carried %d cells, want all 8", n)
	}
	v.Flush(cells[:6], 3, 2)
	if n := records(t, v.take()); n != 6 {
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

// TestACellFramePaintedFromTheAtlasIsTheRastersPixels: painting each record's
// background and then its glyph from the atlas texture in its foreground, as a
// cell drawer does, gives the raster's pixels for the probe and for a live frame.
func TestACellFramePaintedFromTheAtlasIsTheRastersPixels(t *testing.T) {
	p, err := NewProbe(16)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := paint(t, p.font.mask, p.Frame()), p.Pixels(); !bytes.Equal(got, want) {
		t.Fatal("the probe painted from the atlas differs from the raster")
	}
	cell, raster := testView(t, false), testView(t, true)
	for _, v := range []*view{cell, raster} {
		v.Flush(p.cells, p.cols, p.rows)
	}
	frame := raster.take()
	if got := paint(t, p.font.mask, cell.take()); !bytes.Equal(got, frame[8:]) {
		t.Fatal("a live cell frame differs from the raster drawer's")
	}
}

// paint is a cell drawer in Go: RGBA from a whole cell frame and the atlas texture
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
