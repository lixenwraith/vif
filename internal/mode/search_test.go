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

// TestStarAndHashFindTheWordAtTheCursorWhole: * and # search for the word under the
// cursor, passing over a match inside a longer word, and # from the word's start, so
// it leaves the word it is in; n keeps the way the search went and N turns it.
func TestStarAndHashFindTheWordAtTheCursorWhole(t *testing.T) {
	ctx, keys := lineWorld(t, "ab xab ab abc ab", 0)
	for _, c := range []struct {
		keys string
		x    int
	}{{"*", 7}, {"n", 14}, {"n", 0}, {"#", 14}, {"n", 7}, {"N", 14}, {"l#", 7}} {
		keys(c.keys)
		if pos, _ := ctx.World.LocalCursor(); pos.X != c.x {
			t.Fatalf("%q left the cursor at %d, want %d", c.keys, pos.X, c.x)
		}
	}
}
