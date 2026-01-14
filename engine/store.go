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
}

// NewStore creates a new component store for type T
func NewStore[T any]() *Store[T] {
	return &Store[T]{
		components: make(map[core.Entity]T),
		entities:   make([]core.Entity, 0, 64),
	}
}

// SetComponent inserts or updates a component for an entity
func (s *Store[T]) SetComponent(e core.Entity, val T) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.components[e]; !exists {
		s.entities = append(s.entities, e)
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

	if _, exists := s.components[e]; exists {
		delete(s.components, e)
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

// AllEntities returns all entities with this component type
func (s *Store[T]) AllEntities() []core.Entity {
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

// ClearAllComponents removes all components from this store
func (s *Store[T]) ClearAllComponents() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.components = make(map[core.Entity]T)
	s.entities = make([]core.Entity, 0, 64)
}