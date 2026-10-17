//go:build js && wasm

package browser

import (
	"syscall/js"
	"testing"
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
