package mode

import (
	"testing"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/parameter"
)

// TestASecondPressOnACellFiresSpecial: two left presses on one cell within the
// window fire main twice and special once; a press on another cell, or one past
// the window, fires main alone, and the third press of a run starts over.
func TestASecondPressOnACellFiresSpecial(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContext(w, 80, 24)
	machine := input.NewMachine()
	r := NewRouter(ctx, machine)
	fired := func() (main, special int) {
		for _, ev := range w.Resources.Event.Queue.Consume() {
			switch ev.Type {
			case event.EventWeaponFireRequest:
				main++
			case event.EventFireSpecialRequest:
				special++
			}
		}
		return main, special
	}
	press := func(x, y int) {
		t.Helper()
		ev := terminal.Event{Type: terminal.EventMouse, MouseBtn: terminal.MouseBtnLeft, MouseAction: terminal.MouseActionPress, MouseX: x, MouseY: y}
		if intent := machine.Process(ev); intent != nil {
			r.Handle(intent)
		}
		r.Handle(machine.Process(terminal.Event{Type: terminal.EventMouse, MouseBtn: terminal.MouseBtnLeft, MouseAction: terminal.MouseActionRelease, MouseX: x, MouseY: y}))
	}
	press(10, 10)
	press(10, 10)
	if main, special := fired(); main != 2 || special != 1 {
		t.Fatalf("a pair on one cell fired main %d special %d, want 2 and 1", main, special)
	}
	press(10, 10)
	if main, special := fired(); main != 1 || special != 0 {
		t.Fatalf("the third press fired main %d special %d, want 1 and 0", main, special)
	}
	press(12, 10)
	if main, special := fired(); main != 1 || special != 0 {
		t.Fatalf("a press on another cell fired main %d special %d, want 1 and 0", main, special)
	}
	r.leftPressAt = r.leftPressAt.Add(-parameter.MouseDoubleClickWindow - time.Millisecond)
	press(12, 10)
	if main, special := fired(); main != 1 || special != 0 {
		t.Fatalf("a press past the window fired main %d special %d, want 1 and 0", main, special)
	}
}
