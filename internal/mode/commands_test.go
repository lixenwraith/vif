package mode_test

import (
	"strings"
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"

	"github.com/lixenwraith/vif/internal/help"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/mode"
)

// TestCommandsDocumented asserts every documented command exists. The reverse
// direction is not asserted: aliases are deliberately undocumented.
func TestCommandsDocumented(t *testing.T) {
	known := make(map[string]struct{}, len(mode.CommandNames()))
	for _, n := range mode.CommandNames() {
		known[n] = struct{}{}
	}

	for _, topic := range help.Topics(input.DefaultKeyTable()) {
		for _, e := range topic.Entries {
			for tok := range strings.FieldsSeq(e.Keys) {
				name, ok := strings.CutPrefix(tok, ":")
				if !ok || name == "" {
					continue // Bare ':' is the mode-switch key, not a command
				}
				if strings.ContainsAny(name, "{<") {
					continue // Placeholder, e.g. :{command}
				}
				if _, exists := known[name]; !exists {
					t.Errorf("%s: documented command %q does not exist", topic.Title, tok)
				}
			}
		}
	}

}

func TestAutoFireCycleRoutesOnlySelectedWeapons(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	machine := input.NewMachine()
	router := mode.NewRouter(ctx, machine)
	if _, ok := input.ActionEntry("append"); ok {
		t.Fatal("deprecated append action is still registered")
	}
	for _, tc := range []struct {
		state         uint32
		main, special bool
	}{
		{engine.AutoFireBoth, true, true},
		{engine.AutoFireOff, false, false},
		{engine.AutoFireMain, true, false},
		{engine.AutoFireBoth, true, true},
	} {
		if ctx.AutoFire.Load() != tc.state {
			t.Fatalf("auto-fire = %d, want %d", ctx.AutoFire.Load(), tc.state)
		}
		ctx.TimeCtl.Step(parameter.AutoFireInterval)
		router.ProcessInputTick()
		var main, special int
		for _, ev := range w.Resources.Event.Queue.Consume() {
			switch ev.Type {
			case event.EventWeaponFireRequest:
				main++
			case event.EventFireSpecialRequest:
				special++
			}
		}
		if (main == 1) != tc.main || (special == 1) != tc.special || main > 1 || special > 1 {
			t.Fatalf("mode %d: main=%d special=%d", tc.state, main, special)
		}
		intent := machine.Process(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: 'a'})
		if intent == nil || intent.Type != input.IntentToggleAutoFire {
			t.Fatalf("a intent = %+v", intent)
		}
		router.Handle(intent)
		if !ctx.IsNormalMode() || len(w.Resources.Event.Queue.Consume()) != 0 {
			t.Fatal("a changed mode or emitted movement")
		}
	}
	for _, tc := range []struct {
		command string
		state   uint32
	}{
		{"auto main", engine.AutoFireMain}, {"auto on", engine.AutoFireBoth},
		{"auto off", engine.AutoFireOff}, {"auto", engine.AutoFireMain},
		{"auto cleaner", engine.AutoFireMain},
		{"auto invalid", engine.AutoFireMain}, {"auto on extra", engine.AutoFireMain},
	} {
		mode.ExecuteCommand(ctx, tc.command)
		if ctx.AutoFire.Load() != tc.state {
			t.Fatalf("%q selected %d, want %d", tc.command, ctx.AutoFire.Load(), tc.state)
		}
		if tc.state == engine.AutoFireMain && ctx.GetLastCommand() != ":auto main" {
			t.Fatalf("main-fire command label = %q", ctx.GetLastCommand())
		}
	}
	machine.SetMode(input.ModeInsert)
	if intent := machine.Process(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: 'a'}); intent == nil || intent.Type != input.IntentTextChar || intent.Char != 'a' {
		t.Fatalf("insert-mode a = %+v, want text", intent)
	}
}
