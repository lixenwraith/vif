package engine

import (
	"sync"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
)

// Entity is a unique identifier for an entity
type Entity uint64

// Component is a marker interface for all components
type Component interface{}

// System is an interface that all systems must implement
type System interface {
	Update(world *World, dt time.Duration)
	Priority() int // Lower values run first
}

// World contains all entities and their components using compile-time typed stores.
type World struct {
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

	allStores []AnyStore // All stores for uniform lifecycle operations

	systems     []System
	updateMutex sync.Mutex // Prevents concurrent updates
	isUpdating  bool
}

// NewWorld creates a new ECS world with all component stores initialized.
func NewWorld() *World {
	w := &World{
		nextEntityID:   1,
		systems:        make([]System, 0),
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
	w.allStores = []AnyStore{
		w.Positions,
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

// CreateEntity reserves a new entity ID. Use NewEntity() builder for transactional creation.
func (w *World) CreateEntity() Entity {
	w.mu.Lock()
	defer w.mu.Unlock()

	id := w.nextEntityID
	w.nextEntityID++
	return id
}

// DestroyEntity removes all components associated with an entity.
func (w *World) DestroyEntity(e Entity) {
	// Remove from all stores (PositionStore.Remove handles spatial index cleanup internally)
	for _, store := range w.allStores {
		store.Remove(e)
	}
}

// AddSystem adds a system to the world and sorts by priority
func (w *World) AddSystem(system System) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.systems = append(w.systems, system)

	// Sort systems by priority (bubble sort is fine for small number of systems)
	for i := 0; i < len(w.systems)-1; i++ {
		for j := 0; j < len(w.systems)-i-1; j++ {
			if w.systems[j].Priority() > w.systems[j+1].Priority() {
				w.systems[j], w.systems[j+1] = w.systems[j+1], w.systems[j]
			}
		}
	}
}

// Update runs all systems sequentially. Only one update cycle can run at a time.
func (w *World) Update(dt time.Duration) {
	// Acquire update mutex to ensure only one update runs at a time
	w.updateMutex.Lock()
	defer w.updateMutex.Unlock()

	w.mu.RLock()
	systems := make([]System, len(w.systems))
	copy(systems, w.systems)
	w.mu.RUnlock()

	for _, system := range systems {
		system.Update(w, dt)
	}
}

// GetEntityAtPosition returns the entity at a given position (0 if none)
func (w *World) GetEntityAtPosition(x, y int) Entity {
	return w.Positions.GetEntityAt(x, y)
}

// EntityCount returns approximate entity count from highest ID.
// For accurate counts, query specific stores.
func (w *World) EntityCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return int(w.nextEntityID - 1)
}

// MoveEntitySafe atomically moves an entity with collision detection via spatial transactions.
func (w *World) MoveEntitySafe(entity Entity, oldX, oldY, newX, newY int) CollisionResult {
	// Begin transaction
	tx := w.BeginSpatialTransaction()

	// Attempt move
	result := tx.Move(entity, oldX, oldY, newX, newY)

	// If no collision, commit the transaction
	if !result.HasCollision {
		tx.Commit()
	}

	return result
}

// Clear removes all entities and components from the world.
func (w *World) Clear() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.nextEntityID = 1
	for _, store := range w.allStores {
		store.Clear()
	}
}

// HasAnyComponent checks if an entity has at least one component.
func (w *World) HasAnyComponent(e Entity) bool {
	for _, store := range w.allStores {
		if store.Has(e) {
			return true
		}
	}
	return false
}
