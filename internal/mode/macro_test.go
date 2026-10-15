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

// press parses one key as the game would and routes what it makes of it.
func press(machine *input.Machine, r *Router, k terminal.Key, ch rune) {
	if intent := machine.Process(terminal.Event{Type: terminal.EventKey, Key: k, Rune: ch}); intent != nil {
		r.Handle(intent)
	}
}
