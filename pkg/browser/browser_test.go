//go:build js && wasm

package browser

import (
	"syscall/js"
	"testing"

	"github.com/lixenwraith/terminal"
)

// The speaker posts a buffer that sounds and nothing else: no vifAudio, no speaker
func TestSpeakerPostsSoundAlone(t *testing.T) {
	if Speaker() != nil {
		t.Fatal("a speaker without vifAudio")
	}
	var posted []int
	fn := js.FuncOf(func(_ js.Value, args []js.Value) any {
		posted = append(posted, args[0].Get("length").Int())
		return nil
	})
	defer fn.Release()
	js.Global().Set(jsAudio, fn)
	defer js.Global().Delete(jsAudio)
	s := Speaker()
	if s == nil {
		t.Fatal("no speaker with vifAudio defined")
	}
	for _, p := range [][]byte{make([]byte, 8), {0, 0, 1, 0}} {
		if n, err := s.Write(p); n != len(p) || err != nil {
			t.Fatalf("Write(%v) = %d, %v", p, n, err)
		}
	}
	if len(posted) != 1 || posted[0] != 4 {
		t.Fatalf("posted %v, want the 4-byte buffer alone", posted)
	}
}

// TestTheStatusBarTakesATapWithThePointerOff: with the run taking no pointer, a
// finger's press on the status bar's rows is taken with its release, as Android's
// view takes it; one on the map, a mouse's anywhere and a lone release are not.
func TestTheStatusBarTakesATapWithThePointerOff(t *testing.T) {
	h := &Host{barRows: 2}
	h.setGrid(10, 4)
	at := func(y int, action terminal.MouseAction, touch bool) bool {
		return h.admit(terminal.Event{Type: terminal.EventMouse, MouseBtn: terminal.MouseBtnLeft,
			MouseAction: action, MouseY: y, Touch: touch})
	}
	if at(1, terminal.MouseActionPress, true) || at(1, terminal.MouseActionRelease, true) {
		t.Fatal("a finger on the map was taken")
	}
	if at(3, terminal.MouseActionPress, false) {
		t.Fatal("a mouse press on the status bar was taken")
	}
	if !at(2, terminal.MouseActionPress, true) || !at(0, terminal.MouseActionRelease, true) {
		t.Fatal("a finger on the status bar was not taken with its release")
	}
}
