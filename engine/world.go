package engine

// @lixen: #dev{base(core),feature[drain(render,system)],feature[dust(render,system)],feature[quasar(render,system)]}

import (
	"sync"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/event"
)

// World contains all entities and their components using typed stores
type World struct {
	mu           sync.RWMutex
	nextEntityID core.Entity

	// Global ResourceStore
	ResourceStore *ResourceStore

	// Position Store (Special - spatial index, kept as named field)
	Components ComponentStore
	Positions  *PositionStore

	// Direct pointers for high-frequency path optimization
	eventQueue  *event.EventQueue
	frameSource *atomic.Int64

	systems     []System
	updateMutex sync.Mutex
}

// NewWorld creates a new ECS world with dynamic component store support
func NewWorld() *World {
	w := &World{
		nextEntityID:  1,
		ResourceStore: NewResourceStore(),
		systems:       make([]System, 0),
	}

	initComponentStores(w)

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
	if prot, ok := w.Components.Protection.Get(e); ok {
		if prot.Mask == component.ProtectAll {
			return
		}
	}
	w.removeFromAllStores(e)
}

// Clear removes all entities and components from the world
func (w *World) Clear() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.nextEntityID = 1
	w.clearAllStores()
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

// TryLock attempts to acquire the update mutex without blocking
// Returns true if lock acquired, false if already held
func (w *World) TryLock() bool {
	return w.updateMutex.TryLock()
}

// Unlock releases the update mutex
func (w *World) Unlock() {
	w.updateMutex.Unlock()
}

// Update runs all systems sequentially
func (w *World) Update() {
	w.RunSafe(func() {
		w.UpdateLocked()
	})
}

// UpdateLocked runs all systems assuming the caller already holds updateMutex
func (w *World) UpdateLocked() {
	w.mu.RLock()
	systems := make([]System, len(w.systems))
	copy(systems, w.systems)
	w.mu.RUnlock()

	for _, system := range systems {
		system.Update()
	}
}

// TODO: more clutches for clockscheduler
// FrameNumber returns the current authoritative frame index from GameContext
// Optimized for hot-path access by simulation and event loops
func (w *World) FrameNumber() int64 {
	if w.frameSource == nil {
		return 0
	}
	return w.frameSource.Load()
}

// SetEventMetadata wires the direct pointers for PushEvent optimization
// Called once during GameContext initialization
func (w *World) SetEventMetadata(q *event.EventQueue, f *atomic.Int64) {
	w.eventQueue = q
	w.frameSource = f
}

// PushEvent emits a game event using direct cached pointers
// This is the hot-path for all system communication
func (w *World) PushEvent(eventType event.EventType, payload any) {
	if w.eventQueue == nil || w.frameSource == nil {
		return // Not yet initialized
	}

	w.eventQueue.Push(event.GameEvent{
		Type:    eventType,
		Payload: payload,
		Frame:   w.frameSource.Load(),
	})
}