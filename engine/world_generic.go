package engine

import (
	"sync"

	"github.com/lixenwraith/vi-fighter/components"
)

// WorldGeneric is the new generics-based ECS world that eliminates reflection.
// This runs parallel to the existing World during the migration phase.
// All component stores are explicitly typed for compile-time type safety.
type WorldGeneric struct {
	mu           sync.RWMutex
	nextEntityID Entity

	// Component Stores (Public for direct system access)
	Positions      *PositionStore
	Characters     *Store[components.CharacterComponent]
	Sequences      *Store[components.SequenceComponent]
	GoldSequences  *Store[components.GoldSequenceComponent]
	FallingDecays  *Store[components.FallingDecayComponent]
	Cleaners       *Store[components.CleanerComponent]
	RemovalFlashes *Store[components.RemovalFlashComponent]
	Nuggets        *Store[components.NuggetComponent]
	Drains         *Store[components.DrainComponent]

	// Lifecycle registry - all stores implement AnyStore for uniform cleanup
	allStores []AnyStore
}

// NewWorldGeneric creates a new generics-based ECS world with all component stores initialized.
func NewWorldGeneric() *WorldGeneric {
	w := &WorldGeneric{
		nextEntityID:   1,
		Positions:      NewPositionStore(),
		Characters:     NewStore[components.CharacterComponent](),
		Sequences:      NewStore[components.SequenceComponent](),
		GoldSequences:  NewStore[components.GoldSequenceComponent](),
		FallingDecays:  NewStore[components.FallingDecayComponent](),
		Cleaners:       NewStore[components.CleanerComponent](),
		RemovalFlashes: NewStore[components.RemovalFlashComponent](),
		Nuggets:        NewStore[components.NuggetComponent](),
		Drains:         NewStore[components.DrainComponent](),
	}

	// Register all stores for lifecycle operations
	// Note: PositionStore.Store is registered (the underlying generic store)
	w.allStores = []AnyStore{
		w.Positions.Store,
		w.Characters,
		w.Sequences,
		w.GoldSequences,
		w.FallingDecays,
		w.Cleaners,
		w.RemovalFlashes,
		w.Nuggets,
		w.Drains,
	}

	return w
}

// CreateEntity reserves a new entity ID without adding any components.
// Use EntityBuilder (to be implemented) for transactional entity creation.
func (w *WorldGeneric) CreateEntity() Entity {
	w.mu.Lock()
	defer w.mu.Unlock()

	id := w.nextEntityID
	w.nextEntityID++
	return id
}

// DestroyEntity removes all components associated with an entity.
// This iterates through all stores and removes the entity from each.
func (w *WorldGeneric) DestroyEntity(e Entity) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Remove from all stores
	for _, store := range w.allStores {
		store.Remove(e)
	}
}

// EntityCount returns the approximate number of entities in the world.
// This is calculated from the highest entity ID, not the actual count of
// entities with components. For accurate counts, query specific stores.
func (w *WorldGeneric) EntityCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return int(w.nextEntityID - 1)
}

// Clear removes all entities and components from the world.
// This is useful for resetting game state or cleaning up during tests.
func (w *WorldGeneric) Clear() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.nextEntityID = 1
	for _, store := range w.allStores {
		store.Clear()
	}
}

// HasAnyComponent checks if an entity has at least one component.
// This is useful for validating entity existence.
func (w *WorldGeneric) HasAnyComponent(e Entity) bool {
	for _, store := range w.allStores {
		if store.Has(e) {
			return true
		}
	}
	return false
}
