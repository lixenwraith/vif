package engine

import (
	"sync"

	"github.com/lixenwraith/vi-fighter/core"
)

// Store is a generic container for a specific component type T
// Uses sparse set pattern for cache-friendly iteration
type Store[T any] struct {
	mu         sync.RWMutex
	components map[core.Entity]T
	entities   []core.Entity // Array of entities that have this component

	world *World // Reference for bitmask signature updates
	bit   uint64 // Component's unique bit index
}

// NewStore creates a new component store for type T
func NewStore[T any](w *World, bit uint64) *Store[T] {
	return &Store[T]{
		components: make(map[core.Entity]T),
		entities:   make([]core.Entity, 0, 64),
		world:      w,
		bit:        bit,
	}
}

// SetUnsafe updates a component without locking (Fast path for tick-bound systems)
// Caller MUST hold UpdateMutex (RunSafe context)
func (s *Store[T]) SetUnsafe(e core.Entity, val T) {
	if _, exists := s.components[e]; !exists {
		s.entities = append(s.entities, e)
		s.world.AddSignature(e, s.bit)
	}
	s.components[e] = val
}

// GetUnsafe retrieves a component without locking
// Caller MUST hold UpdateMutex (RunSafe context)
func (s *Store[T]) GetUnsafe(e core.Entity) (T, bool) {
	val, ok := s.components[e]
	return val, ok
}

// HasUnsafe checks if an entity has this component without locking
// Caller MUST hold UpdateMutex (RunSafe context)
func (s *Store[T]) HasUnsafe(e core.Entity) bool {
	_, ok := s.components[e]
	return ok
}

// SetComponent inserts or updates a component for an entity
func (s *Store[T]) SetComponent(e core.Entity, val T) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.components[e]; !exists {
		s.entities = append(s.entities, e)
		s.world.AddSignature(e, s.bit)
	}
	s.components[e] = val
}

// GetComponent retrieves a component for an entity
func (s *Store[T]) GetComponent(e core.Entity) (T, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.components[e]
	return val, ok
}

// RemoveEntity deletes a component from an entity
func (s *Store[T]) RemoveEntity(e core.Entity) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.components) == 0 {
		return
	}

	if _, exists := s.components[e]; exists {
		delete(s.components, e)
		s.world.RemoveSignature(e, s.bit)
		// RemoveEntity from entities slice
		for i, entity := range s.entities {
			if entity == e {
				s.entities[i] = s.entities[len(s.entities)-1]
				s.entities = s.entities[:len(s.entities)-1]
				break
			}
		}
	}
}

// HasEntity checks if entity has this component
func (s *Store[T]) HasEntity(e core.Entity) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.components[e]
	return ok
}

// GetAllEntities returns all entities with this component type
func (s *Store[T]) GetAllEntities() []core.Entity {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]core.Entity, len(s.entities))
	copy(result, s.entities)
	return result
}

// CountEntities returns number of entities with this component
func (s *Store[T]) CountEntities() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entities)
}

// RemoveBatch deletes multiple entities in a single pass - O(n+m) vs O(n*m) for individual removes
func (s *Store[T]) RemoveBatch(entities []core.Entity) {
	if len(entities) == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Early exit if store is empty
	if len(s.components) == 0 {
		return
	}

	// Build removal set and delete from map
	toRemove := make(map[core.Entity]struct{}, len(entities))
	for _, e := range entities {
		if _, exists := s.components[e]; exists {
			toRemove[e] = struct{}{}
			delete(s.components, e)
			s.world.RemoveSignature(e, s.bit)
		}
	}

	if len(toRemove) == 0 {
		return
	}

	// Single pass compaction of entities slice
	writeIdx := 0
	for _, e := range s.entities {
		if _, remove := toRemove[e]; !remove {
			s.entities[writeIdx] = e
			writeIdx++
		}
	}
	s.entities = s.entities[:writeIdx]
}

// RemoveEntityUnsafe deletes a component from an entity without locking
// Caller MUST hold updateMutex
func (s *Store[T]) RemoveEntityUnsafe(e core.Entity) {
	if len(s.components) == 0 {
		return
	}

	if _, exists := s.components[e]; exists {
		delete(s.components, e)
		s.world.RemoveSignature(e, s.bit)
		for i, entity := range s.entities {
			if entity == e {
				s.entities[i] = s.entities[len(s.entities)-1]
				s.entities = s.entities[:len(s.entities)-1]
				break
			}
		}
	}
}

// RemoveBatchUnsafe deletes multiple entities in a single pass without locking
// Caller MUST hold updateMutex
func (s *Store[T]) RemoveBatchUnsafe(entities []core.Entity) {
	if len(entities) == 0 || len(s.components) == 0 {
		return
	}

	toRemove := make(map[core.Entity]struct{}, len(entities))
	for _, e := range entities {
		if _, exists := s.components[e]; exists {
			toRemove[e] = struct{}{}
			delete(s.components, e)
			s.world.RemoveSignature(e, s.bit)
		}
	}

	if len(toRemove) == 0 {
		return
	}

	writeIdx := 0
	for _, e := range s.entities {
		if _, remove := toRemove[e]; !remove {
			s.entities[writeIdx] = e
			writeIdx++
		}
	}
	s.entities = s.entities[:writeIdx]
}

// ClearAllComponents removes all components from this store
func (s *Store[T]) ClearAllComponents() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for e := range s.components {
		s.world.RemoveSignature(e, s.bit)
	}

	s.components = make(map[core.Entity]T)
	s.entities = make([]core.Entity, 0, 64)
}

// ClearAllComponentsUnsafe removes all components without locking
// Caller MUST hold updateMutex
func (s *Store[T]) ClearAllComponentsUnsafe() {
	for e := range s.components {
		s.world.RemoveSignature(e, s.bit)
	}

	s.components = make(map[core.Entity]T)
	s.entities = make([]core.Entity, 0, 64)
}
