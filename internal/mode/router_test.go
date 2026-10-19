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
	press(11, 11)
	if main, special := fired(); main != 1 || special != 1 {
		t.Fatalf("a press a cell off, a finger's jitter, fired main %d special %d, want 1 and 1", main, special)
	}
	press(14, 11)
	press(12, 10)
	if main, special := fired(); main != 2 || special != 0 {
		t.Fatalf("presses past the slack fired main %d special %d, want 2 and 0", main, special)
	}
	r.leftPressAt = r.leftPressAt.Add(-parameter.MouseDoubleClickWindow - time.Millisecond)
	press(12, 10)
	if main, special := fired(); main != 1 || special != 0 {
		t.Fatalf("a press past the window fired main %d special %d, want 1 and 0", main, special)
	}
}

// TestTheMenuKeepsThePointerWithTheMouseDisabled: with mouse input disabled a
// press on the map is nothing, a press held on the status bar still opens the
// menu, which turns pointer reporting on for its rows, and a press there selects.
func TestTheMenuKeepsThePointerWithTheMouseDisabled(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContext(w, 80, 24)
	ctx.SessionCtl = &menuSession{}
	machine := input.NewMachine()
	r := NewRouter(ctx, machine)
	var reporting bool
	r.SetMouseModeApplier(func(enabled, _ bool) { reporting = enabled })
	ctx.MouseDisabled.Store(true)
	ctx.AutoFire.Store(engine.AutoFireOff) // the tick's own fire requests would count as the press's
	r.ProcessInputTick()
	if reporting {
		t.Fatal("the mouse disabled, yet reported")
	}
	down := func(x, y int) {
		t.Helper()
		if intent := machine.Process(terminal.Event{Type: terminal.EventMouse, MouseBtn: terminal.MouseBtnLeft, MouseAction: terminal.MouseActionPress, MouseX: x, MouseY: y}); intent != nil {
			r.Handle(intent)
		}
	}
	down(10, 10)
	if evs := w.Resources.Event.Queue.Consume(); len(evs) != 0 || r.mouseLeftHeld {
		t.Fatalf("a press on the map was taken with the mouse disabled: held %v, events %d %v", r.mouseLeftHeld, len(evs), evs)
	}
	down(10, ctx.Height-1)
	if !r.menuHold {
		t.Fatal("a press on the status bar did not start the hold")
	}
	r.menuSince = r.menuSince.Add(-parameter.MenuHoldDuration)
	r.ProcessInputTick()
	r.ProcessInputTick() // the tick after the menu opened applies its reporting
	if configMenu(ctx) == nil || !reporting {
		t.Fatalf("after the hold: menu %v, reporting %v", configMenu(ctx) != nil, reporting)
	}
	g := ctx.OverlayGeometry()
	down(g.X+g.ContentX, g.Y+g.ContentY+2)
	if ctx.GetOverlaySelection() != "multiplayer" {
		t.Fatalf("a press on the third row selected %q", ctx.GetOverlaySelection())
	}
}
