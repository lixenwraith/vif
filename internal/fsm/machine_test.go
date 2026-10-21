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

func TestLoadRefusesOnlySystemsBesideDisabled(t *testing.T) {
	for where, doc := range map[string]string{
		"[systems]":    "[systems]\nonly_systems = [\"a\"]\ndisabled_systems = [\"b\"]\n[regions.r]\ninitial = \"A\"\n[states.A]\n",
		"region \"r\"": "[regions.r]\ninitial = \"A\"\nonly_systems = [\"a\"]\ndisabled_systems = [\"b\"]\n[states.A]\n",
	} {
		if _, _, err := loadMachine(t, doc); err == nil || !strings.Contains(err.Error(), where) {
			t.Errorf("%s: load error = %v, want only_systems beside disabled_systems refused", where, err)
		}
	}
}

// TestRunningRegionsDecideTheSystems: the union of the running regions'
// only_systems lists stands in for the baseline, their enables add, their
// disables take away, and a paused region decides nothing
func TestRunningRegionsDecideTheSystems(t *testing.T) {
	const doc = `
[systems]
disabled_systems = ["gold"]
[regions.main]
initial = "A"
enabled_systems = ["glyph"]
[regions.puzzle]
only_systems = ["typing"]
[regions.calm]
only_systems = ["timer"]
[regions.quiet]
disabled_systems = ["typing"]
[states.A]
`
	m, trace, err := loadMachine(t, doc)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := m.Init(trace); err != nil {
		t.Fatalf("init: %v", err)
	}
	a, _ := m.GetStateID("A")
	expect := func(when string, wanted, decided map[string]bool) {
		t.Helper()
		for name, want := range wanted {
			if got := m.SystemWanted(name); got != want {
				t.Errorf("%s: %s wanted = %v, want %v", when, name, got, want)
			}
		}
		for name, want := range decided {
			if got := m.SystemDecided(name); got != want {
				t.Errorf("%s: %s decided = %v, want %v", when, name, got, want)
			}
		}
	}

	expect("baseline", map[string]bool{"gold": false, "particle": true, "glyph": true},
		map[string]bool{"glyph": true, "particle": false, "gold": false})
	if err := m.SpawnRegion(trace, "puzzle", a); err != nil {
		t.Fatal(err)
	}
	expect("narrowed", map[string]bool{"typing": true, "particle": false, "glyph": true, "timer": false},
		map[string]bool{"particle": true})
	if err := m.SpawnRegion(trace, "calm", a); err != nil {
		t.Fatal(err)
	}
	expect("two narrowed", map[string]bool{"typing": true, "timer": true, "particle": false}, nil)
	m.PauseRegion("puzzle")
	expect("one paused", map[string]bool{"typing": false, "timer": true}, nil)
	if err := m.SpawnRegion(trace, "quiet", a); err != nil {
		t.Fatal(err)
	}
	m.ResumeRegion("puzzle")
	expect("disabled beside", map[string]bool{"typing": false, "timer": true}, map[string]bool{"typing": true})
	for _, r := range []string{"calm", "puzzle", "quiet"} {
		if err := m.TerminateRegion(trace, r); err != nil {
			t.Fatal(err)
		}
	}
	expect("handed back", map[string]bool{"gold": false, "particle": true, "typing": true, "timer": true},
		map[string]bool{"particle": false, "typing": false})
}
