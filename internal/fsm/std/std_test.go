package std_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/fsm"
	"github.com/lixenwraith/vif/internal/fsm/std"
)

// TestAnEscalationCountsKillsSinceItsMark: a script marks where its window opens and
// gates on the kills since, so the status key keeps the game's total.
func TestAnEscalationCountsKillsSinceItsMark(t *testing.T) {
	const doc = `
[regions.storm]
initial = "Count"

[states.Count]
parent = "Root"
transitions = [
    { trigger = "Tick", target = "Escalate", guard = "StatusIntCompare", guard_args = { key = "kills.storm", since = "mark", op = "gte", value = 3 } },
]

[states.Escalate]
parent = "Root"
on_enter = [
    { action = "StatusToVar", payload = { key = "kills.storm", name = "mark" } },
    { action = "IncrementVar", payload = { name = "escalations", delta = 1 } },
]
transitions = [{ trigger = "Tick", target = "Count" }]
`
	event.EnsureRegistry()
	status := map[string]int64{}
	m := fsm.NewMachine[map[string]int64]()
	std.Register(m, std.Host[map[string]int64]{
		StatusInt: func(s map[string]int64, key string) (int64, bool) {
			v, ok := s[key]
			return v, ok
		},
	})
	if err := m.LoadScenario([]byte(doc)); err != nil {
		t.Fatal(err)
	}
	if err := m.Init(status); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct{ kills, escalations int64 }{{2, 0}, {3, 1}, {5, 1}, {6, 2}} {
		status["kills.storm"] = step.kills
		m.Update(status, time.Millisecond)
		m.Update(status, time.Millisecond)
		if got := m.GetVar("escalations"); got != step.escalations {
			t.Fatalf("at %d kills: %d escalations, want %d", step.kills, got, step.escalations)
		}
	}
}

// TestAnInputIsJudgedByTheTickItWasMadeAt: a window marks its open and close ticks,
// a press counts by the tick it carries, however late it arrives, and the miss waits
// out the horizon a press made at the close could still arrive within.
func TestAnInputIsJudgedByTheTickItWasMadeAt(t *testing.T) {
	const doc = `
[regions.round]
initial = "Open"

[states.Open]
parent = "Root"
on_enter = [
    { action = "StatusToVar", payload = { key = "engine.ticks", name = "open" } },
    { action = "CopyVar", payload = { name = "close", source_var = "open" } },
    { action = "IncrementVar", payload = { name = "close", delta = 5 } },
    { action = "StatusToVar", payload = { key = "engine.input_horizon", name = "deadline" } },
    { action = "IncrementVar", payload = { name = "deadline", source_var = "close" } },
]
transitions = [
    { trigger = "EventInputPressed", target = "Hit", guard = "And", guard_args = { guards = [
        { name = "PayloadIntCompareVar", args = { field = "tick", op = "gt", var = "open" } },
        { name = "PayloadIntCompareVar", args = { field = "tick", op = "lte", var = "close" } },
    ] } },
    { trigger = "Tick", target = "Miss", guard = "StatusIntCompare", guard_args = { key = "engine.ticks", since = "deadline", op = "gte", value = 0 } },
]

[states.Hit]
parent = "Root"

[states.Miss]
parent = "Root"
`
	event.EnsureRegistry()
	for _, c := range []struct {
		name    string
		horizon int64
		at      int64  // engine.ticks the press arrives at, none for zero
		tick    uint64 // the tick the press was made at
		want    string
	}{
		{"made at the close", 0, 15, 15, "Hit"},
		{"made at the open", 0, 12, 10, "Miss"},
		{"made past the close", 0, 15, 16, "Miss"},
		{"none", 0, 0, 0, "Miss"},
		{"made at the close, arriving late", 40, 55, 15, "Hit"},
		{"made past the close, arriving late", 40, 30, 16, "Miss"},
	} {
		t.Run(c.name, func(t *testing.T) {
			status := map[string]int64{"engine.ticks": 10, "engine.input_horizon": c.horizon}
			m := fsm.NewMachine[map[string]int64]()
			std.Register(m, std.Host[map[string]int64]{
				StatusInt: func(s map[string]int64, key string) (int64, bool) {
					v, ok := s[key]
					return v, ok
				},
			})
			if err := m.LoadScenario([]byte(doc)); err != nil {
				t.Fatal(err)
			}
			if err := m.Init(status); err != nil {
				t.Fatal(err)
			}
			state := func() string { name, _, _ := m.GetActiveRegionTelemetry(); return name }
			for ; status["engine.ticks"] <= 15+c.horizon && state() == "Open"; status["engine.ticks"]++ {
				if status["engine.ticks"] == c.at {
					m.HandleEvent(status, event.GameEvent{Type: event.EventInputPressed, Payload: &event.InputPayload{Tick: c.tick}})
				}
				m.Update(status, time.Millisecond)
				if state() == "Miss" && status["engine.ticks"] != 15+c.horizon {
					t.Fatalf("missed at %d, before the horizon closed at %d", status["engine.ticks"], 15+c.horizon)
				}
			}
			if got := state(); got != c.want {
				t.Fatalf("judged %s, want %s", got, c.want)
			}
		})
	}
}

// TestARandomDrawIsAFunctionOfItsSeedAndIndex: a sequence drawn by index replays from
// its seed alone, stays in range, and moves with the host's seed.
func TestARandomDrawIsAFunctionOfItsSeedAndIndex(t *testing.T) {
	const doc = `
[regions.r]
initial = "Draw"

[states.Draw]
parent = "Root"
on_enter = [
    { action = "SetVar", payload = { name = "s", value = 41 } },
    { action = "IncrementVar", payload = { name = "i" } },
    { action = "RandomVar", payload = { name = "x", min = -2, max = 3, seed_var = "s", index_var = "i" } },
]
transitions = [{ trigger = "Tick", target = "Again" }]

[states.Again]
parent = "Root"
transitions = [{ trigger = "Tick", target = "Draw" }]
`
	draws := func(root uint64) []int64 {
		m := fsm.NewMachine[struct{}]()
		std.Register(m, std.Host[struct{}]{Seed: func(struct{}) uint64 { return root }})
		if err := m.LoadScenario([]byte(doc)); err != nil {
			t.Fatal(err)
		}
		if err := m.Init(struct{}{}); err != nil {
			t.Fatal(err)
		}
		var out []int64
		for range 64 {
			if x := m.GetVar("x"); x < -2 || x > 3 {
				t.Fatalf("draw %d is outside [-2, 3]", x)
			}
			out = append(out, m.GetVar("x"))
			m.Update(struct{}{}, time.Millisecond)
			m.Update(struct{}{}, time.Millisecond)
		}
		return out
	}
	a, b, c := draws(1), draws(1), draws(2)
	if !slices.Equal(a, b) {
		t.Fatalf("one seed drew two sequences:\n%v\n%v", a, b)
	}
	if slices.Equal(a, c) {
		t.Fatal("two host seeds drew one sequence")
	}
	seen := map[int64]bool{}
	for _, x := range a {
		seen[x] = true
	}
	if len(seen) != 6 {
		t.Fatalf("64 draws covered %d of 6 values", len(seen))
	}
}

// TestAStringPayloadVarFillsItsBraces: a message carries a variable's value where it
// says {}, and the compiled message stays a template for the next emit.
func TestAStringPayloadVarFillsItsBraces(t *testing.T) {
	const doc = `
[regions.r]
initial = "Boot"

[states.Boot]
parent = "Root"
on_enter = [{ action = "SetVar", payload = { name = "level", value = 7 } }]
transitions = [{ trigger = "Tick", target = "Say" }]

[states.Say]
parent = "Root"
on_enter = [
    { action = "EmitEvent", event = "EventMetaStatusMessageRequest", payload = { message = "Level {} of {}" }, payload_vars = { message = "level" } },
]
transitions = [{ trigger = "Tick", target = "Next", actions = [{ action = "IncrementVar", payload = { name = "level" } }] }]

[states.Next]
parent = "Root"
transitions = [{ trigger = "Tick", target = "Say" }]
`
	event.EnsureRegistry()
	var said []string
	m := fsm.NewMachine[struct{}]()
	std.Register(m, std.Host[struct{}]{Emit: func(_ struct{}, _ event.EventType, p any) {
		said = append(said, p.(*event.MetaStatusMessagePayload).Message)
	}})
	if err := m.LoadScenario([]byte(doc)); err != nil {
		t.Fatal(err)
	}
	if err := m.Init(struct{}{}); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		m.Update(struct{}{}, time.Millisecond)
	}
	if want := []string{"Level 7 of 7", "Level 8 of 8"}; !slices.Equal(said, want) {
		t.Fatalf("said %q, want %q", said, want)
	}
}

// TestAnEmittedPayloadSpellsItsEnumsByName: EmitEvent decodes a payload as the
// host's names spell its enums, a sub-type by its species and a program as its data
// species, and a name its type lacks fails the load.
func TestAnEmittedPayloadSpellsItsEnumsByName(t *testing.T) {
	load := func(payloads string) (*fsm.Machine[struct{}], *[]any, error) {
		var sent []any
		m := fsm.NewMachine[struct{}]()
		std.Register(m, std.Host[struct{}]{
			Emit:  func(_ struct{}, _ event.EventType, p any) { sent = append(sent, p) },
			Names: event.PayloadNames(component.Declared{Programs: []string{"invader"}}),
		})
		return m, &sent, m.LoadScenario([]byte(`
[regions.r]
initial = "Boot"

[states.Boot]
parent = "Root"
on_enter = [` + payloads + `]
`))
	}
	event.EnsureRegistry()
	m, sent, err := load(`
    { action = "EmitEvent", event = "EventGatewaySpawnRequest", payload = { species = "eye", sub_type = "flame" } },
    { action = "EmitEvent", event = "EventGeneticRegisterSpecies", payload = { species = "invader" } },`)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Init(struct{}{}); err != nil {
		t.Fatal(err)
	}
	gw, _ := (*sent)[0].(*event.GatewaySpawnRequestPayload)
	ga, _ := (*sent)[1].(*event.GeneticRegisterSpeciesPayload)
	if gw == nil || gw.Species != component.SpeciesEye || gw.SubType != component.SubType(component.EyeTypeFlame) ||
		ga == nil || ga.Species != component.SpeciesDataFirst {
		t.Fatalf("sent %+v, %+v", gw, ga)
	}
	_, _, err = load(`{ action = "EmitEvent", event = "EventGatewaySpawnRequest", payload = { species = "tower", sub_type = "flame" } },`)
	if err == nil || !strings.Contains(err.Error(), `sub_type: "flame" is not one of cyan, gold, violet, emerald`) {
		t.Fatalf("error %v", err)
	}
}

// TestAPickEmitsTheEntryItsVariableNames: a pick writes the entry its variable
// names into its field, the value modulo the list's length and a negative one
// counting back from the end, beside the payload's other fields and payload_vars;
// an enum entry takes its name, a program among the species, and a nested field
// its dot path.
func TestAPickEmitsTheEntryItsVariableNames(t *testing.T) {
	const doc = `
[regions.r]
initial = "Say"

[states.Say]
parent = "Root"
on_enter = [
    { action = "EmitEvent", event = "EventBehaviorSpawnRequest", payload = { y = 3 }, payload_vars = { x = "v" }, pick = { field = "program", var = "v", from = ["invader", "scarab", "construct"] } },
    { action = "EmitEvent", event = "EventGeneticRegisterSpecies", pick = { field = "species", var = "v", from = ["eye", "invader"] } },
    { action = "EmitEvent", event = "EventMazeSpawnRequest", payload = { cell_width = 5 }, pick = { field = "visual.box_style", var = "v", from = ["single", "double"] } },
]
transitions = [{ trigger = "Tick", target = "Wait" }]

[states.Wait]
parent = "Root"
transitions = [{ trigger = "Tick", target = "Say" }]
`
	event.EnsureRegistry()
	var sent []any
	m := fsm.NewMachine[struct{}]()
	std.Register(m, std.Host[struct{}]{
		Emit:  func(_ struct{}, _ event.EventType, p any) { sent = append(sent, p) },
		Names: event.PayloadNames(component.Declared{Programs: []string{"invader"}}),
	})
	if err := m.LoadScenario([]byte(doc)); err != nil {
		t.Fatal(err)
	}
	if err := m.Init(struct{}{}); err != nil {
		t.Fatal(err)
	}
	vs := []int64{0, 1, 2, 3, 7, -1, -4}
	for _, v := range vs[1:] {
		m.Update(struct{}{}, time.Millisecond)
		m.SetVar("v", v)
		m.Update(struct{}{}, time.Millisecond)
	}
	eye, invader := component.SpeciesEye, component.SpeciesDataFirst
	single, double := component.BoxDrawSingle, component.BoxDrawDouble
	want := []struct {
		program string
		species component.SpeciesType
		style   component.BoxDrawStyle
	}{
		{"invader", eye, single},
		{"scarab", invader, double},
		{"construct", eye, single},
		{"invader", invader, double},
		{"scarab", invader, double},
		{"construct", invader, double},
		{"construct", eye, single},
	}
	if len(sent) != 3*len(want) {
		t.Fatalf("sent %d payloads, want %d", len(sent), 3*len(want))
	}
	for i, w := range want {
		spawn, _ := sent[3*i].(*event.BehaviorSpawnRequestPayload)
		ga, _ := sent[3*i+1].(*event.GeneticRegisterSpeciesPayload)
		maze, _ := sent[3*i+2].(*event.MazeSpawnRequestPayload)
		if spawn == nil || ga == nil || maze == nil {
			t.Fatalf("v %d sent %T, %T, %T", vs[i], sent[3*i], sent[3*i+1], sent[3*i+2])
		}
		if spawn.Program != w.program || spawn.X != int(vs[i]) || spawn.Y != 3 || ga.Species != w.species ||
			maze.Visual.BoxStyle != w.style || maze.CellWidth != 5 {
			t.Errorf("v %d sent %+v, %+v, %+v; want %s, %v, %v", vs[i], *spawn, *ga, maze.Visual, w.program, w.species, w.style)
		}
	}
}

// TestAPickIsCheckedAtLoad: a pick names its field, its variable and at least one
// entry, leaves its field to itself, and each entry decodes as its field's value in
// its payload, or the load fails naming the entry.
func TestAPickIsCheckedAtLoad(t *testing.T) {
	event.EnsureRegistry()
	for _, c := range []struct{ action, want string }{
		{`{ action = "EmitEvent", event = "EventBehaviorSpawnRequest", pick = { field = "program", from = ["invader"] } }`,
			"pick: requires 'field' and 'var'"},
		{`{ action = "EmitEvent", event = "EventBehaviorSpawnRequest", pick = { field = "program", var = "v", from = [] } }`,
			"pick: requires 'from'"},
		{`{ action = "EmitEvent", event = "EventBehaviorSpawnRequest", payload_vars = { program = "v" }, pick = { field = "program", var = "v", from = ["invader"] } }`,
			"pick: payload_vars sets 'program' as well"},
		{`{ action = "EmitEvent", event = "EventBehaviorSpawnRequest", pick = { field = "program", var = "v", from = ["invader", 3] } }`,
			"pick: from[1]: payload for event 'EventBehaviorSpawnRequest': program: cannot convert int64 to string"},
		{`{ action = "EmitEvent", event = "EventGeneticRegisterSpecies", pick = { field = "species", var = "v", from = ["eye", "wyvern"] } }`,
			`pick: from[1]: payload for event 'EventGeneticRegisterSpecies': species: "wyvern" is not one of`},
		{`{ action = "EmitEvent", event = "EventGatewaySpawnRequest", payload = { sub_type = "flame" }, pick = { field = "species", var = "v", from = ["eye", "snake"] } }`,
			"pick: from[1]: payload for event 'EventGatewaySpawnRequest': sub_type: snake takes no names"},
		{`{ action = "EmitEvent", event = "EventBehaviorSpawnRequest", pick = { field = "programme", var = "v", from = ["invader"] } }`,
			`pick: from[0]: payload for event 'EventBehaviorSpawnRequest': unknown key "programme"`},
		{`{ action = "EmitEvent", event = "EventBehaviorSpawnRequest", payload = { x = 1 }, pick = { field = "x.y", var = "v", from = [1] } }`,
			"pick: field 'x.y': 'y' is under int64, not a table"},
		{`{ action = "EmitEvent", event = "EventMazeSpawnRequest", payload = { rooms = [{ width = 3 }] }, pick = { field = "rooms.1.width", var = "v", from = [5] } }`,
			"pick: field 'rooms.1.width': '1' is not an entry of a list of 1"},
		{`{ action = "SetVar", payload = { name = "v", value = 1 }, pick = { field = "value", var = "v", from = [1] } }`,
			"action 'SetVar' takes no pick"},
		{`{ action = "EmitEvent", event = "EventBehaviorSpawnRequest", pick = { field = "program", var = "v", from = ["invader"], by = "w" } }`,
			`unknown key "on_enter.pick.by"`},
	} {
		m := fsm.NewMachine[struct{}]()
		std.Register(m, std.Host[struct{}]{
			Emit:  func(struct{}, event.EventType, any) {},
			Names: event.PayloadNames(component.Declared{}),
		})
		err := m.LoadScenario([]byte(`
[regions.r]
initial = "Boot"

[states.Boot]
parent = "Root"
on_enter = [` + c.action + `]
`))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s:\nerror %v, want it to hold %q", c.action, err, c.want)
		}
	}
}
