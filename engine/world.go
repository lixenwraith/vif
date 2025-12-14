package engine

import (
	"sync"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/core"
)

// World contains all entities and their components using compile-time typed stores
type World struct {
	mu           sync.RWMutex
	nextEntityID core.Entity

	// Global Resources
	Resources *ResourceStore

	// Component Stores (Public for direct system access)
	Positions       *PositionStore
	Characters      *Store[components.CharacterComponent]
	Sequences       *Store[components.SequenceComponent]
	GoldSequences   *Store[components.GoldSequenceComponent]
	Decays          *Store[components.DecayComponent]
	Cleaners        *Store[components.CleanerComponent]
	Flashes         *Store[components.FlashComponent]
	Nuggets         *Store[components.NuggetComponent]
	Drains          *Store[components.DrainComponent]
	Materializers   *Store[components.MaterializeComponent]
	Cursors         *Store[components.CursorComponent]
	Protections     *Store[components.ProtectionComponent]
	Pings           *Store[components.PingComponent]
	Energies        *Store[components.EnergyComponent]
	Shields         *Store[components.ShieldComponent]
	Heats           *Store[components.HeatComponent]
	Splashes        *Store[components.SplashComponent]
	MarkedForDeaths *Store[components.MarkedForDeathComponent]
	Timers          *Store[components.TimerComponent]

	allStores []AnyStore // All stores for uniform lifecycle operations

	systems     []System
	updateMutex sync.Mutex // Prevents concurrent updates
}

// NewWorld creates a new ECS world with all component stores initialized
func NewWorld() *World {
	w := &World{
		nextEntityID:    1,
		Resources:       NewResourceStore(),
		systems:         make([]System, 0),
		Positions:       NewPositionStore(),
		Characters:      NewStore[components.CharacterComponent](),
		Sequences:       NewStore[components.SequenceComponent](),
		GoldSequences:   NewStore[components.GoldSequenceComponent](),
		Decays:          NewStore[components.DecayComponent](),
		Cleaners:        NewStore[components.CleanerComponent](),
		Flashes:         NewStore[components.FlashComponent](),
		Nuggets:         NewStore[components.NuggetComponent](),
		Drains:          NewStore[components.DrainComponent](),
		Materializers:   NewStore[components.MaterializeComponent](),
		Cursors:         NewStore[components.CursorComponent](),
		Protections:     NewStore[components.ProtectionComponent](),
		Pings:           NewStore[components.PingComponent](),
		Energies:        NewStore[components.EnergyComponent](),
		Shields:         NewStore[components.ShieldComponent](),
		Heats:           NewStore[components.HeatComponent](),
		Splashes:        NewStore[components.SplashComponent](),
		Timers:          NewStore[components.TimerComponent](),
		MarkedForDeaths: NewStore[components.MarkedForDeathComponent](),
	}

	// TODO: need to make this a factory
	// Register all stores for lifecycle operations
	w.allStores = []AnyStore{
		w.Positions,
		w.Characters,
		w.Sequences,
		w.GoldSequences, // TODO: updates handled in game state, to be changed, so keeping it though dead code
		w.Decays,
		w.Cleaners,
		w.Flashes,
		w.Nuggets,
		w.Drains,
		w.Materializers,
		w.Cursors,
		w.Protections,
		w.Pings,
		w.Energies,
		w.Shields,
		w.Heats,
		w.Splashes,
		w.Timers,
		w.MarkedForDeaths,
	}

	// Set world reference for z-index lookups
	w.Positions.SetWorld(w)

	return w
}

// CreateEntity reserves a new entity ID
func (w *World) CreateEntity() core.Entity {
	w.mu.Lock()
	defer w.mu.Unlock()

	id := w.nextEntityID
	w.nextEntityID++
	return id
}

// DestroyEntity removes all components associated with an entity
func (w *World) DestroyEntity(e core.Entity) {
	// Check protection before destruction
	if prot, ok := w.Protections.Get(e); ok {
		if prot.Mask == components.ProtectAll {
			// Entity is immortal - reject destruction silently
			// This prevents accidental cursor destruction
			return
		}
	}

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

// RunSafe executes a function while holding the world's update lock
// Use for any operation that mutates world state outside of standard Update cycles
func (w *World) RunSafe(fn func()) {
	w.updateMutex.Lock()
	defer w.updateMutex.Unlock()
	fn()
}

// Lock acquires a lock on the world's update mutex (sync.Mutex)
// Must be paired with Unlock()
func (w *World) Lock() {
	w.updateMutex.Lock()
}

// Unlock releases the update mutex
func (w *World) Unlock() {
	w.updateMutex.Unlock()
}

// Update runs all systems sequentially. Only one update cycle can run at a time
func (w *World) Update(dt time.Duration) {
	w.RunSafe(func() {
		w.UpdateLocked(dt)
	})
}

// UpdateLocked runs all systems assuming the caller already holds updateMutex
// Internal use only: call Update() for standard usage, or wrap in RunSafe()
func (w *World) UpdateLocked(dt time.Duration) {
	w.mu.RLock()
	systems := make([]System, len(w.systems))
	copy(systems, w.systems)
	w.mu.RUnlock()

	for _, system := range systems {
		system.Update(w, dt)
	}
}

// Clear removes all entities and components from the world
func (w *World) Clear() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.nextEntityID = 1
	for _, store := range w.allStores {
		store.Clear()
	}
}