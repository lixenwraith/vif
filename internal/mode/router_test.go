package mode

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/core"
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

// TestAHeldKeyCrossesOneReleaseHoweverItEnds: a captured key held where keys release
// crosses its own release; play stopping, or the capture giving the key back, ends a
// hold as lost, and a keyboard reporting presses alone taps. Keys coming to be
// captured, and each answer while they are, cross the slot's report.
func TestAHeldKeyCrossesOneReleaseHoweverItEnds(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContext(w, 80, 24)
	machine := input.NewMachine()
	r := NewRouter(ctx, machine)
	var asks []bool
	r.SetKeyReportApplier(func(console bool) { asks = append(asks, console) })
	crossed := func() (out []string) {
		for _, ev := range w.Resources.Event.Queue.Consume() {
			switch p := ev.Payload.(type) {
			case *event.InputPayload:
				if ev.Type == event.EventInputPressed {
					out = append(out, "press "+p.Key)
				} else if ev.Type == event.EventInputReleased {
					out = append(out, fmt.Sprintf("release %s lost=%v", p.Key, p.Lost))
				}
			case *event.InputReportPayload:
				out = append(out, fmt.Sprintf("report releases=%v", p.Releases))
			}
		}
		return out
	}
	expect := func(step string, want ...string) {
		t.Helper()
		if got := crossed(); !slices.Equal(got, want) {
			t.Fatalf("%s crossed %q, want %q", step, got, want)
		}
	}
	key := func(k rune, action terminal.KeyAction) {
		if in := machine.Process(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: k, Action: action}); in != nil {
			r.Handle(in)
		}
	}

	ctx.KeyCapture.Set("12", true)
	r.ProcessInputTick()
	r.KeyReported(true)
	expect("the answer to the console ask", "report releases=true")
	key('1', terminal.KeyPress)
	key('1', terminal.KeyRepeat)
	key('1', terminal.KeyRelease)
	expect("a press held and let go", "press 1", "release 1 lost=false")

	key('2', terminal.KeyPress)
	r.Handle(&input.Intent{Type: input.IntentModeSwitch, ModeTarget: input.ModeTargetCommand, Count: 1})
	r.ProcessInputTick()
	key('2', terminal.KeyRelease)
	expect("a hold as the command line opens", "press 2", "release 2 lost=true")

	r.transitionMode(core.ModeNormal)
	key('1', terminal.KeyPress)
	ctx.KeyCapture.Set("2", true)
	r.ProcessInputTick()
	expect("a hold the capture gives back", "press 1", "release 1 lost=true")

	r.KeyReported(false)
	key('2', terminal.KeyPress)
	expect("a press with presses alone reported", "report releases=false", "press 2", "release 2 lost=true")

	ctx.KeyCapture.Set("", false)
	r.ProcessInputTick()
	if !slices.Equal(asks, []bool{true, false}) {
		t.Fatalf("asked for the console keyboard %v, want it while the capture asked and not after", asks)
	}
}
