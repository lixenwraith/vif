package registry

import (
	"fmt"
	"sort"

	"github.com/lixenwraith/vi-fighter/pkg/genetic/persistence"
)

// SpeciesPopulation is one registered species' population, named by the species
// name rather than by slot index so an export survives the registration order
// changing between the instance that produced it and the one that installs it.
type SpeciesPopulation struct {
	ID   uint8                     `toml:"id" json:"id"`
	Name string                    `toml:"name" json:"name"`
	Pool persistence.PopulationDTO `toml:"pool" json:"pool"`
}

// Export returns every registered species' population, in species-ID order.
//
// This is the transfer contract the registry did not have. Persistence could
// already write a population to a file, one species at a time, through a Store
// the caller supplies; a transfer needs all of them as one value, with no store
// involved and no lock reaching the caller. The mutex stays inside: callers
// receive plain data that is safe to hold, compare and serialize, and the deep
// copy comes from the engine's own Snapshot.
//
// The order is canonical because a capture is compared as well as installed.
func (r *Registry) Export() []SpeciesPopulation {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]SpeciesPopulation, 0, 8)
	for i := range r.slots {
		ts := r.slots[i].Load()
		if ts == nil {
			continue
		}
		out = append(out, SpeciesPopulation{
			ID:   uint8(i),
			Name: ts.Config.Name,
			Pool: persistence.FromPool(ts.Engine.Snapshot()),
		})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out
}

// Import installs exported populations, replacing each named species' archive
// and generation. A species the receiving registry has not registered is
// reported rather than skipped: it means the two sides disagree about which
// species exist, and a population that silently fails to install leaves the
// receiver evolving from its own archive while believing it adopted the
// sender's.
//
// Species are matched by name, and the ID is checked against it. A name that
// resolves to a different slot than the export recorded is a build mismatch, not
// something to reconcile.
func (r *Registry) Import(populations []SpeciesPopulation) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	byName := make(map[string]int, len(r.slots))
	for i := range r.slots {
		if ts := r.slots[i].Load(); ts != nil {
			byName[ts.Config.Name] = i
		}
	}

	for _, sp := range populations {
		slot, ok := byName[sp.Name]
		if !ok {
			return fmt.Errorf("genetic: species %q is not registered in this build", sp.Name)
		}
		if uint8(slot) != sp.ID {
			return fmt.Errorf("genetic: species %q is slot %d here and %d in the capture",
				sp.Name, slot, sp.ID)
		}
		ts := r.slots[slot].Load()
		if ts == nil {
			return fmt.Errorf("genetic: species %q vanished during import", sp.Name)
		}
		ts.Engine.Inject(sp.Pool.ToPool(), sp.Pool.Generation)
	}
	return nil
}
