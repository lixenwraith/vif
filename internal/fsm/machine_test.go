package fsm_test

import (
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/fsm"
	"github.com/lixenwraith/vif/internal/fsm/std"
)

func loadMachine(t *testing.T, doc string) (*fsm.Machine[*eventTrace], *eventTrace, error) {
	t.Helper()
	event.EnsureRegistry()
	trace := new(eventTrace)
	m := fsm.NewMachine[*eventTrace]()
	std.Register(m, std.Host[*eventTrace]{
		Emit: func(dst *eventTrace, et event.EventType, _ any) { *dst = append(*dst, et) },
	})
	return m, trace, m.LoadScenario([]byte(doc))
}

func TestLoadRefusesParentCycle(t *testing.T) {
	for name, doc := range map[string]string{
		"pair": "[regions.r]\ninitial = \"A\"\n[states.A]\nparent = \"B\"\n[states.B]\nparent = \"A\"\n",
		"self": "[regions.r]\ninitial = \"A\"\n[states.A]\nparent = \"A\"\n",
	} {
		_, _, err := loadMachine(t, doc)
		if err == nil || !strings.Contains(err.Error(), "'A'") {
			t.Fatalf("%s: load error = %v, want a parent cycle naming A", name, err)
		}
	}
}

func TestLoadFailsWithoutStates(t *testing.T) {
	_, _, err := loadMachine(t, "[regions.r]\ninitial = \"A\"\n")
	if err == nil || !strings.Contains(err.Error(), "'A'") {
		t.Fatalf("load error = %v, want unknown initial state A", err)
	}
}

func TestOnExitTerminatesOwnRegion(t *testing.T) {
	const doc = `
[regions.r]
initial = "A"
[states.A]
on_exit = [{ action = "TerminateRegion", region = "r" }]
transitions = [{ trigger = "Tick", target = "B" }]
[states.B]
on_enter = [{ action = "EmitEvent", event = "EventStrobeRequest" }]
`
	m, trace, err := loadMachine(t, doc)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := m.Init(trace); err != nil {
		t.Fatalf("init: %v", err)
	}
	m.Update(trace, time.Millisecond)
	if m.HasRegion("r") || len(*trace) != 0 {
		t.Fatalf("after exit: region r = %v, events = %v; want gone and no enter", m.HasRegion("r"), *trace)
	}
	if err := m.Init(trace); err != nil {
		t.Fatalf("re-init: %v", err)
	}
	if err := m.Reset(trace); err != nil || m.GetRegionState("r") != "A" {
		t.Fatalf("reset: err = %v, state = %q, want A", err, m.GetRegionState("r"))
	}
}
