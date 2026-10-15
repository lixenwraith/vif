package mode

import (
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/input"
)

// Search takes any rune, so Backspace removes one rune, not one byte: a byte cut
// left "\xc3" after "/é", which PerformSearch read as U+FFFD and never matched.
func TestSearchBackspaceDeletesARune(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	machine := input.NewMachine()
	r := NewRouter(ctx, machine)
	key := func(k terminal.Key, ch rune) {
		t.Helper()
		if intent := machine.Process(terminal.Event{Type: terminal.EventKey, Key: k, Rune: ch}); intent != nil {
			r.Handle(intent)
		}
	}
	for _, ch := range "/aé" {
		key(terminal.KeyRune, ch)
	}
	key(terminal.KeyBackspace, 0)
	if got := ctx.GetSearchText(); got != "a" {
		t.Fatalf("search text %q after Backspace, want %q", got, "a")
	}
	key(terminal.KeyBackspace, 0)
	key(terminal.KeyBackspace, 0)
	if got := ctx.GetSearchText(); got != "" {
		t.Fatalf("search text %q after deleting past the start, want empty", got)
	}
}
