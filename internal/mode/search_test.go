package mode

import (
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/input"
)

// Search takes any rune, so Backspace removes one rune, not one byte.
func TestSearchBackspaceDeletesARune(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	machine := input.NewMachine()
	r := NewRouter(ctx, machine)
	for _, ch := range "/aé" {
		press(machine, r, terminal.KeyRune, ch)
	}
	press(machine, r, terminal.KeyBackspace, 0)
	if got := ctx.GetSearchText(); got != "a" {
		t.Fatalf("search text %q after Backspace, want %q", got, "a")
	}
}
