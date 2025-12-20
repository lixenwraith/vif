package system

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
)

// CullSystem removes entities marked for destruction
// It runs last in the tick to allow other systems to react to the tagged state
type CullSystem struct {
	world *engine.World
	res   engine.Resources

	deathStore *engine.Store[component.DeathComponent]
	protStore  *engine.Store[component.ProtectionComponent]
}

// NewCullSystem creates a new cull system
func NewCullSystem(world *engine.World) engine.System {
	return &CullSystem{
		world: world,
		res:   engine.GetResources(world),

		deathStore: engine.GetStore[component.DeathComponent](world),
		protStore:  engine.GetStore[component.ProtectionComponent](world),
	}
}

// Init
func (s *CullSystem) Init() {}

// Priority returns the system's priority (highest value = runs last)
func (s *CullSystem) Priority() int {
	return constant.PriorityCleanup
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
			if prot.Mask.Has(component.ProtectFromCull) || prot.Mask == component.ProtectAll {
				s.deathStore.Remove(entity)
				continue
			}
		}

		// Safe to destroy
		s.world.DestroyEntity(entity)
	}
}