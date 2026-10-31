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

// TestTheMenuCrossesTheBridge: with the worker defining vifMenu the page draws the
// menu, a snapshot reaches it as its JSON and a closed menu as the empty string, and
// a menu message from the page reaches the host's MenuOp with its fields; a page
// without vifMenu draws none.
func TestTheMenuCrossesTheBridge(t *testing.T) {
	if HasMenu() {
		t.Fatal("a menu without vifMenu")
	}
	var handed []string
	fn := js.FuncOf(func(_ js.Value, args []js.Value) any {
		handed = append(handed, args[0].String())
		return nil
	})
	defer fn.Release()
	js.Global().Set(jsMenu, fn)
	defer js.Global().Delete(jsMenu)
	if !HasMenu() {
		t.Fatal("no menu with vifMenu defined")
	}
	h := &Host{}
	h.Menu([]byte(`{"title":"Menu"}`))
	h.Menu(nil)
	if len(handed) != 2 || handed[0] != `{"title":"Menu"}` || handed[1] != "" {
		t.Fatalf("the page was handed %q", handed)
	}
	var got [3]string
	h.MenuOp = func(op, id, arg string) { got = [3]string{op, id, arg} }
	msg := js.Global().Get("Object").New()
	for k, v := range map[string]string{"type": "menu", "op": "set", "id": "settings/#Controls/auto", "arg": "main"} {
		msg.Set(k, v)
	}
	h.handle(js.Undefined(), []js.Value{msg})
	if got != [3]string{"set", "settings/#Controls/auto", "main"} {
		t.Fatalf("the op crossed as %q", got)
	}
}
