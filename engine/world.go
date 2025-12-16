package engine

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/events"
)

// World contains all entities and their components using compile-time typed stores
type World struct {
	mu           sync.RWMutex
	nextEntityID core.Entity

	// Global Resources
	Resources *ResourceStore

	// Position Store (Special - spatial index, kept as named field)
	Positions *PositionStore

	// Dynamic Component Stores (registered at startup)
	componentStores map[reflect.Type]AnyStore
	allStores       []AnyStore // For lifecycle operations (Clear, DestroyEntity)

	systems     []System
	updateMutex sync.Mutex
}

// NewWorld creates a new ECS world with dynamic component store support
func NewWorld() *World {
	w := &World{
		nextEntityID:    1,
		Resources:       NewResourceStore(),
		systems:         make([]System, 0),
		Positions:       NewPositionStore(),
		componentStores: make(map[reflect.Type]AnyStore),
		allStores:       make([]AnyStore, 0, 32),
	}

	// Register Positions in allStores for lifecycle management
	w.allStores = append(w.allStores, w.Positions)

	// Set world reference for z-index lookups
	w.Positions.SetWorld(w)

	return w
}

// RegisterComponent creates and registers a typed component store
// Must be called during initialization before systems access stores
func RegisterComponent[T any](w *World) {
	w.mu.Lock()
	defer w.mu.Unlock()

	t := reflect.TypeOf((*T)(nil)).Elem()
	if _, exists := w.componentStores[t]; exists {
		return // Already registered, idempotent
	}

	store := NewStore[T]()
	w.componentStores[t] = store
	w.allStores = append(w.allStores, store)
}

// GetStore retrieves a typed component store
// Panics if store not registered - indicates bootstrap error
// Call only during system initialization, cache the result
func GetStore[T any](w *World) *Store[T] {
	t := reflect.TypeOf((*T)(nil)).Elem()

	w.mu.RLock()
	defer w.mu.RUnlock()

	if store, ok := w.componentStores[t]; ok {
		return store.(*Store[T])
	}
	panic(fmt.Sprintf("component store not registered: %v", t))
}

// HasStore checks if a component store is registered
func HasStore[T any](w *World) bool {
	t := reflect.TypeOf((*T)(nil)).Elem()

	w.mu.RLock()
	defer w.mu.RUnlock()

	_, ok := w.componentStores[t]
	return ok
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
	if HasStore[components.ProtectionComponent](w) {
		protStore := GetStore[components.ProtectionComponent](w)
		if prot, ok := protStore.Get(e); ok {
			if prot.Mask == components.ProtectAll {
				return // Entity is immortal
			}
		}
	}

	// Remove from all stores
	for _, store := range w.allStores {
		store.Remove(e)
	}
}

// AddSystem adds a system to the world and sorts by priority
func (w *World) AddSystem(system System) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.systems = append(w.systems, system)

	// Sort by priority (bubble sort, small N)
	for i := 0; i < len(w.systems)-1; i++ {
		for j := 0; j < len(w.systems)-i-1; j++ {
			if w.systems[j].Priority() > w.systems[j+1].Priority() {
				w.systems[j], w.systems[j+1] = w.systems[j+1], w.systems[j]
			}
		}
	}
}

// Systems returns a copy of all registered systems
// Used by ClockScheduler for event handler auto-registration
func (w *World) Systems() []System {
	w.mu.RLock()
	defer w.mu.RUnlock()
	result := make([]System, len(w.systems))
	copy(result, w.systems)
	return result
}

// RunSafe executes a function while holding the world's update lock
func (w *World) RunSafe(fn func()) {
	w.updateMutex.Lock()
	defer w.updateMutex.Unlock()
	fn()
}

// Lock acquires a lock on the world's update mutex
func (w *World) Lock() {
	w.updateMutex.Lock()
}

// Unlock releases the update mutex
func (w *World) Unlock() {
	w.updateMutex.Unlock()
}

// Update runs all systems sequentially
func (w *World) Update(dt time.Duration) {
	w.RunSafe(func() {
		w.UpdateLocked(dt)
	})
}

// UpdateLocked runs all systems assuming the caller already holds updateMutex
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

// PushEvent emits a game event using world resources
func (w *World) PushEvent(eventType events.EventType, payload any) {
	eqRes, eqOk := GetResource[*EventQueueResource](w.Resources)
	stateRes, stateOk := GetResource[*GameStateResource](w.Resources)
	if !eqOk || !stateOk {
		return
	}
	event := events.GameEvent{
		Type:    eventType,
		Payload: payload,
		Frame:   stateRes.State.GetFrameNumber(),
	}
	eqRes.Queue.Push(event)
}