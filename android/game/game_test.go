package game

import (
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/parameter"
)

func testView(t *testing.T, raster bool, density float32) *view {
	t.Helper()
	tex, err := loadTexture()
	if err != nil {
		t.Fatal(err)
	}
	return newView(tex, raster, density, 0)
}

// TestATapPressesAndReleasesTheCellUnderIt: a 420 dpi phone's cell is 16x32
// pixels, and the grid they fit is stretched over the view, so a pixel maps to the
// cell drawn there in proportion, one past the edge to the edge cell; nothing is
// reported while the run takes no pointer.
func TestATapPressesAndReleasesTheCellUnderIt(t *testing.T) {
	v := testView(t, false, 2.625)
	v.setGrid(10*16+15, 4*32+31) // 10x4 cells, stretched by most of a cell each way
	if cols, rows := v.Size(); cols != 10 || rows != 4 {
		t.Fatalf("the grid is %dx%d, want 10x4", cols, rows)
	}
	if ev, ok := v.touch(30, 30, true); ok {
		t.Fatalf("a touch with the pointer off was reported: %+v", ev)
	}
	v.SetPointerMode(true, false)
	for _, c := range []struct {
		x, y, cx, cy int
		down         bool
	}{
		{0, 0, 0, 0, true}, {17, 31, 0, 0, true}, {18, 32, 1, 0, true}, {174, 158, 9, 3, true},
		{175, 159, 9, 3, false}, {-4, 400, 0, 3, false},
	} {
		want := terminal.Event{Type: terminal.EventMouse, MouseBtn: terminal.MouseBtnLeft,
			MouseAction: terminal.MouseActionRelease, MouseX: c.cx, MouseY: c.cy, Touch: true}
		if c.down {
			want.MouseAction = terminal.MouseActionPress
		}
		if ev, ok := v.touch(c.x, c.y, c.down); !ok || ev != want {
			t.Errorf("touch at %d,%d is %+v %v, want %+v", c.x, c.y, ev, ok, want)
		}
	}
}

// TestAFingerDragsOncePerCell: a pressed finger moving reports a left drag each
// time it enters another cell and nothing within one, past the view's edge on its
// edge cell; a finger not pressed drags nothing.
func TestAFingerDragsOncePerCell(t *testing.T) {
	const w, h = 12, 24
	v := testView(t, false, 1)
	v.setGrid(10*w, 4*h)
	v.SetPointerMode(true, false)
	drag := func(x, y int) terminal.Event {
		return terminal.Event{Type: terminal.EventMouse, MouseBtn: terminal.MouseBtnLeft,
			MouseAction: terminal.MouseActionDrag, MouseX: x, MouseY: y, Touch: true}
	}
	if ev, ok := v.drag(w*2, h); ok {
		t.Fatalf("a finger never pressed dragged: %+v", ev)
	}
	v.touch(w*3+1, h*2+1, true)
	for _, c := range []struct {
		x, y int
		want terminal.Event
		ok   bool
	}{
		{w*3 + 9, h*2 + 20, terminal.Event{}, false},
		{w*4 + 1, h*2 + 1, drag(4, 2), true},
		{w*4 + 11, h * 2, terminal.Event{}, false},
		{w * 4, h*3 + 2, drag(4, 3), true},
		{w*10 + 4, h * 3, drag(9, 3), true},
		{w*10 + 9, h * 3, terminal.Event{}, false},
	} {
		if ev, ok := v.drag(c.x, c.y); ok != c.ok || ev != c.want {
			t.Errorf("drag to %d,%d is %+v %v, want %+v %v", c.x, c.y, ev, ok, c.want, c.ok)
		}
	}
	v.touch(w*9, h*3, false)
	if ev, ok := v.drag(w, h); ok {
		t.Fatalf("a released finger dragged: %+v", ev)
	}
}

// TestKeysArriveAsATerminalReportsThem: a named key, Ctrl with a letter as its
// control byte, Shift-Tab as back-tab and a typed character are the events a
// terminal parses for them, modifiers riding only the editing, navigation and
// function keys; a key the game has no use for is left to the system.
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
		{"ctrl enter", 66, metaCtrl, '\n', key(terminal.KeyEnter, 0)},
		{"shift up", 19, metaShift, 0, key(terminal.KeyUp, terminal.ModShift)},
		{"ctrl caret", 75, metaCtrl, '^', key(terminal.KeyCtrlCaret, 0)},
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

// TestTheStatusBarTakesATapWithThePointerOff: with the run taking no pointer a
// press on the status bar's rows is reported with its release, and one anywhere
// else is not; a release with no press taken is nothing.
func TestTheStatusBarTakesATapWithThePointerOff(t *testing.T) {
	v := testView(t, false, 1)
	v.setGrid(10*12, 4*24)
	if _, ok := v.touch(30, 30, true); ok {
		t.Fatal("a press on the map was taken")
	}
	if _, ok := v.touch(30, 30, false); ok {
		t.Fatal("a release with no press was taken")
	}
	if ev, ok := v.touch(30, 4*24-1, true); !ok || ev.MouseY != 3 {
		t.Fatalf("a press on the status bar: %+v %v", ev, ok)
	}
	if ev, ok := v.touch(30, 10, false); !ok || ev.MouseAction != terminal.MouseActionRelease {
		t.Fatalf("its release: %+v %v", ev, ok)
	}
}

// TestAPinchZoomsTheCellWithinBounds: a pinch scales the cell in CellStep steps,
// and the grid the same pixels hold follows; it stops at CellMost, and short of
// CellLeast at the atlas, which a 420 dpi phone reaches at 9.5 dp.
func TestAPinchZoomsTheCellWithinBounds(t *testing.T) {
	v := testView(t, false, 2.625)
	v.setGrid(1008, 2244)
	cols, rows := v.Size()
	v.zoom(2)
	v.setGrid(1008, 2244)
	if c, r := v.Size(); v.Cell() != 2*cellDP || c*2 != cols && c*2 != cols-1 || r >= rows {
		t.Fatalf("at %g dp the grid is %dx%d, was %dx%d at %d", v.Cell(), c, r, cols, rows, cellDP)
	}
	if v.zoom(10); v.Cell() != parameter.CellMost {
		t.Fatalf("zoomed in past the bound to %g dp", v.Cell())
	}
	v.zoom(0.01)
	if a := v.tex.Atlas(); v.Cell() != 9.5 || v.cellW != a.Width || v.cellH != a.Height {
		t.Fatalf("zoomed out to %g dp, a %dx%d cell, where the atlas is %dx%d", v.Cell(), v.cellW, v.cellH, a.Width, a.Height)
	}
}
