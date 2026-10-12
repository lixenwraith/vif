package std_test

import (
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
