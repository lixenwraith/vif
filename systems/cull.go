// @lixen: #focus{lifecycle[cull,destroy]}
// @lixen: #interact{state[death,protection],end[entity]}
package systems

import (
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// CullSystem removes entities marked for destruction
// It runs last in the tick to allow other systems to react to the tagged state
type CullSystem struct{}

// NewCullSystem creates a new cull system
func NewCullSystem() *CullSystem {
	return &CullSystem{}
}

// Priority returns the system's priority (highest value = runs last)
func (s *CullSystem) Priority() int {
	return constants.PriorityCleanup
}

// Update iterates through tagged entities and destroys them
func (s *CullSystem) Update(world *engine.World, dt time.Duration) {
	// Query all entities tagged MarkedForDeath
	entities := world.MarkedForDeaths.All()

	for _, entity := range entities {
		// Check protection flags
		if prot, ok := world.Protections.Get(entity); ok {
			// If entity is protected from culling or is immortal, remove the OOB tag but don't destroy
			// This prevents the system from checking it repeatedly or destroying cursor
			if prot.Mask.Has(components.ProtectFromCull) || prot.Mask == components.ProtectAll {
				world.MarkedForDeaths.Remove(entity)
				continue
			}
		}

		// Safe to destroy
		world.DestroyEntity(entity)
	}
}