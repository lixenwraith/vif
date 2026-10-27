package mode

import (
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/input"
)

// Playback waits in Command and Overlay mode, so a recording that kept the `:`,
// the command's characters and its confirm stalled at the open command line and
// played the rest in Normal mode once it was left.
func TestARecordingLeavesCommandAndOverlayModeOut(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	machine := input.NewMachine()
	r := NewRouter(ctx, machine)
	key := func(k terminal.Key, ch rune) { press(machine, r, k, ch) }
	key(terminal.KeyRune, 'q')
	key(terminal.KeyRune, 'a')
	key(terminal.KeyRune, 'j')
	for _, ch := range ":auto off" {
		key(terminal.KeyRune, ch)
	}
	key(terminal.KeyEnter, 0)
	key(terminal.KeyCtrlG, 0)
	key(terminal.KeyEscape, 0)
	key(terminal.KeyRune, 'k')
	key(terminal.KeyRune, 'q')
	if r.macro.IsRecording() || ctx.GetMode() != 0 {
		t.Fatalf("recording=%v mode=%v after the closing q", r.macro.IsRecording(), ctx.GetMode())
	}
	got := r.macro.buffers['a']
	if len(got) != 2 || got[0].Type != input.IntentMotion || got[1].Type != input.IntentMotion {
		t.Fatalf("register a holds %+v, want the two motions alone", got)
	}
}

// An overlay's pointer intents drive Overlay mode alone, so a recording keeps none,
// not even from Search mode, whose pointer the parser hands the overlay too.
func TestARecordingLeavesAnOverlaysPointerOut(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	machine := input.NewMachine()
	r := NewRouter(ctx, machine)
	key := func(k terminal.Key, ch rune) { press(machine, r, k, ch) }
	pointer := func(t input.IntentType) bool {
		return t == input.IntentOverlayPoint || t == input.IntentOverlayDrag || t == input.IntentOverlayWheel
	}
	key(terminal.KeyRune, 'q')
	key(terminal.KeyRune, 'a')
	key(terminal.KeyRune, '/')
	for _, ev := range []terminal.Event{
		{Type: terminal.EventMouse, MouseBtn: terminal.MouseBtnWheelDown, MouseAction: terminal.MouseActionPress, MouseX: 5, MouseY: 5},
		{Type: terminal.EventMouse, MouseBtn: terminal.MouseBtnLeft, MouseAction: terminal.MouseActionPress, MouseX: 5, MouseY: 5},
		{Type: terminal.EventMouse, MouseBtn: terminal.MouseBtnLeft, MouseAction: terminal.MouseActionDrag, MouseX: 5, MouseY: 6},
	} {
		intent := machine.Process(ev)
		if intent == nil || !pointer(intent.Type) {
			t.Fatalf("search mode made %+v of %v %v", intent, ev.MouseBtn, ev.MouseAction)
		}
		r.Handle(intent)
	}
	key(terminal.KeyEscape, 0)
	key(terminal.KeyRune, 'q')
	for _, intent := range r.macro.buffers['a'] {
		if pointer(intent.Type) {
			t.Fatalf("register a holds %v: %+v", intent.Type, r.macro.buffers['a'])
		}
	}
}

// press parses one key as the game would and routes what it makes of it.
func press(machine *input.Machine, r *Router, k terminal.Key, ch rune) {
	if intent := machine.Process(terminal.Event{Type: terminal.EventKey, Key: k, Rune: ch}); intent != nil {
		r.Handle(intent)
	}
}
