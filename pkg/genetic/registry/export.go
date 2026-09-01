package registry

import (
	"fmt"
	"math/rand/v2"
	"sort"

	"github.com/lixenwraith/vi-fighter/pkg/genetic"
)

// SpeciesState is one registered species' complete continuation point. Species
// are named as well as numbered so an import detects a registration-layout
// mismatch instead of installing one population into another.
type SpeciesState struct {
	ID           uint8                                      `toml:"id" json:"id"`
	Name         string                                     `toml:"name" json:"name"`
	Engine       genetic.StreamingState[[]float64, float64] `toml:"engine" json:"engine"`
	ProbeRNG     []byte                                     `toml:"probe_rng" json:"probe_rng"`
	ProbeCounter uint64                                     `toml:"probe_counter" json:"probe_counter"`
}

// Export returns every registered species' complete state, in species-ID order.
// It carries the streaming engine position rather than only its scored archive:
// queued offspring, pending evaluations, IDs and both PCG streams all decide the
// next genotype. Callers receive plain, deep-copied data with no mutex exposed.
func (r *Registry) Export() ([]SpeciesState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]SpeciesState, 0, 8)
	for i := range r.slots {
		ts := r.slots[i].Load()
		if ts == nil {
			continue
		}
		state, err := ts.checkpoint()
		if err != nil {
			return nil, fmt.Errorf("genetic: export species %q: %w", ts.Config.Name, err)
		}
		state.ID = uint8(i)
		state.Name = ts.Config.Name
		out = append(out, state)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out, nil
}

// Import installs complete exported states. The registered species set must
// match exactly; omitting one would leave that engine evolving from receiver-local
// state while the caller believed the registry had been restored.
func (r *Registry) Import(states []SpeciesState) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	registered := 0
	for i := range r.slots {
		if r.slots[i].Load() != nil {
			registered++
		}
	}
	if len(states) != registered {
		return fmt.Errorf("genetic: state carries %d species, registry has %d", len(states), registered)
	}

	resolved := make([]*TrackedSpecies, len(states))
	seen := make(map[int]bool, len(states))
	probeSources := make([]*rand.PCG, len(states))
	for i, state := range states {
		slot := int(state.ID)
		if seen[slot] {
			return fmt.Errorf("genetic: species slot %d appears more than once", slot)
		}
		seen[slot] = true
		ts := r.slots[slot].Load()
		if ts == nil {
			return fmt.Errorf("genetic: species %q uses unregistered slot %d", state.Name, slot)
		}
		if ts.Config.Name != state.Name {
			return fmt.Errorf("genetic: slot %d is species %q here and %q in the capture",
				slot, ts.Config.Name, state.Name)
		}
		source, err := ts.validateState(state)
		if err != nil {
			return fmt.Errorf("genetic: import species %q: %w", state.Name, err)
		}
		resolved[i], probeSources[i] = ts, source
	}

	// Validation above is complete. Each restore now performs only copies and a
	// PCG assignment, so a malformed later species cannot leave a partial import.
	for i, state := range states {
		if err := resolved[i].restore(state, probeSources[i]); err != nil {
			return fmt.Errorf("genetic: import species %q: %w", state.Name, err)
		}
	}
	return nil
}
