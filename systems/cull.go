package systems

import (
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// CullSystem removes entities marked for destruction
// It runs last in the tick to allow other systems to react to the tagged state
type CullSystem struct {
	world *engine.World
	res   engine.CoreResources

	deathStore *engine.Store[components.DeathComponent]
	protStore  *engine.Store[components.ProtectionComponent]
}

// NewCullSystem creates a new cull system
func NewCullSystem(world *engine.World) engine.System {
	return &CullSystem{
		world: world,
		res:   engine.GetCoreResources(world),

		deathStore: engine.GetStore[components.DeathComponent](world),
		protStore:  engine.GetStore[components.ProtectionComponent](world),
	}
}

// Init
func (s *CullSystem) Init() {}

// Priority returns the system's priority (highest value = runs last)
func (s *CullSystem) Priority() int {
	return constants.PriorityCleanup
}

// Update iterates through tagged entities and destroys them
func (s *CullSystem) Update() {
	// Query all entities tagged MarkedForDeath
	entities := s.deathStore.All()

	for _, entity := range entities {
		// Check protection flags
		if prot, ok := s.protStore.Get(entity); ok {
			// If entity is protected from culling or is immortal, remove the OOB tag but don't destroy
			// This prevents the system from checking it repeatedly or destroying cursor
			if prot.Mask.Has(components.ProtectFromCull) || prot.Mask == components.ProtectAll {
				s.deathStore.Remove(entity)
				continue
			}
		}

		// Safe to destroy
		s.world.DestroyEntity(entity)
	}
}