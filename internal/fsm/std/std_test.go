package std_test

import (
	"slices"
	"testing"
	"time"

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
