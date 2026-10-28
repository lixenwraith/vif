package manifest

import (
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/fsm"
)

// scratchWorld builds a world with resources but no services, enough to
// construct every system and read its declarations
func scratchWorld(t *testing.T) *engine.World {
	t.Helper()
	return scratchContext(t).World
}

// scratchContext is scratchWorld's context, which the context systems take
func scratchContext(t *testing.T) *engine.GameContext {
	t.Helper()
	event.EnsureRegistry()
	return engine.NewGameContextWithClock(engine.NewWorld(), 80, 24, engine.NewManualClock())
}

// TestActiveSystemsMatchRuntimeNames asserts the manifest key equals the name
// the system answers to. Config validation reads ActiveSystems while the
// runtime toggle matches System.Name(), so a divergence makes a valid config
// name do nothing and a working name fail validation.
func TestActiveSystemsMatchRuntimeNames(t *testing.T) {
	built := BuildSystems(scratchWorld(t))
	declared := ActiveSystems()

	if len(built) != len(declared) {
		t.Fatalf("BuildSystems returns %d systems, ActiveSystems %d", len(built), len(declared))
	}
	for i, sys := range built {
		if sys.Name() != declared[i] {
			t.Errorf("manifest key %q but System.Name() is %q", declared[i], sys.Name())
		}
	}
}

// TestASpeciesGuardNamesItsSpecies: PayloadSpeciesIn takes code species and the
// scenario's programs by name, a program as the species its compile gave it, and a
// name no species holds fails the load
func TestASpeciesGuardNamesItsSpecies(t *testing.T) {
	load := func(names string) (*fsm.Machine[*engine.World], error) {
		m := fsm.NewMachine[*engine.World]()
		RegisterFSMComponents(m, component.Declared{Programs: []string{"invader", "construct"}})
		return m, m.LoadScenario([]byte(`
[regions.r]
initial = "Wait"

[states.Wait]
parent = "Root"
transitions = [{ trigger = "EventSpeciesKilled", target = "Hit", guard = "PayloadSpeciesIn", guard_args = { field = "species", names = ` + names + ` } }]

[states.Hit]
parent = "Root"
`))
	}
	if _, err := load(`["invader", "raider"]`); err == nil {
		t.Fatal("a name no species holds loaded")
	}
	w := scratchWorld(t)
	for _, c := range []struct {
		species component.SpeciesType
		hit     bool
	}{{component.SpeciesStorm, true}, {component.SpeciesDataFirst + 1, true}, {component.SpeciesDataFirst, false}, {component.SpeciesSwarm, false}} {
		m, err := load(`["storm", "invader"]`)
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Init(w); err != nil {
			t.Fatal(err)
		}
		m.HandleEvent(w, event.GameEvent{Type: event.EventSpeciesKilled, Payload: &event.SpeciesKilledPayload{Species: c.species}})
		if state, _, _ := m.GetActiveRegionTelemetry(); (state == "Hit") != c.hit {
			t.Fatalf("species %d left the guard in %s, want a hit %v", c.species, state, c.hit)
		}
	}
}
