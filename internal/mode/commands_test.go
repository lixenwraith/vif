package mode_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
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
		{"auto gun", engine.AutoFireMain},
		{"auto invalid", engine.AutoFireMain}, {"auto on extra", engine.AutoFireMain},
	} {
		mode.ExecuteCommand(ctx, tc.command)
		if ctx.AutoFire.Load() != tc.state {
			t.Fatalf("%q selected %d, want %d", tc.command, ctx.AutoFire.Load(), tc.state)
		}
	}
	machine.SetMode(input.ModeInsert)
	if intent := machine.Process(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: 'a'}); intent == nil || intent.Type != input.IntentTextChar || intent.Char != 'a' {
		t.Fatalf("insert-mode a = %+v, want text", intent)
	}
}

// TestEmitSpellsAPayloadsEnumsByName: :emit decodes its payload as a scenario's
// EmitEvent does, so a loot kind takes its name, the code's or one the scenario
// declares, and a key no field takes is refused
func TestEmitSpellsAPayloadsEnumsByName(t *testing.T) {
	event.EnsureRegistry()
	w := engine.NewWorld()
	w.Resources.Loot = component.LootSet{Kinds: []component.LootKind{{LootSpec: component.LootSpec{Name: "upgrade"}}}}
	ctx := engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	for _, tc := range []struct {
		name string
		want event.LootSpawnRequestPayload
	}{
		{"gun", event.LootSpawnRequestPayload{Type: component.LootGun, X: 3, Y: 4}},
		{"upgrade", event.LootSpawnRequestPayload{Type: component.LootCount, X: 5, Y: 6}},
	} {
		mode.ExecuteCommand(ctx, fmt.Sprintf(`emit LootSpawnRequest { type = %q, x = %d, y = %d }`, tc.name, tc.want.X, tc.want.Y))
		events := w.Resources.Event.Queue.Consume()
		if len(events) != 1 {
			t.Fatalf(":emit %s pushed %d events, want one: %q", tc.name, len(events), ctx.GetStatusMessage())
		}
		if p, ok := events[0].Payload.(*event.LootSpawnRequestPayload); !ok || *p != tc.want {
			t.Fatalf(":emit %s pushed %+v, want %+v", tc.name, events[0].Payload, tc.want)
		}
	}

	mode.ExecuteCommand(ctx, `emit LootSpawnRequest { kind = "upgrade" }`)
	if n := len(w.Resources.Event.Queue.Consume()); n != 0 || !strings.Contains(ctx.GetStatusMessage(), `"kind"`) {
		t.Fatalf("an unknown key pushed %d events and answered %q", n, ctx.GetStatusMessage())
	}
}
