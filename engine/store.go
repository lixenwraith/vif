package engine

import "sync"

// Store is a generic container for a specific component type T.
// Uses sparse set pattern for cache-friendly iteration.
type Store[T any] struct {
	mu         sync.RWMutex
	components map[Entity]T
	entities   []Entity // Dense array of entities that have this component
}

// NewStore creates a new component store for type T.
func NewStore[T any]() *Store[T] {
	return &Store[T]{
		components: make(map[Entity]T),
		entities:   make([]Entity, 0, 64),
	}
}

// Add inserts or updates a component for an entity.
func (s *Store[T]) Add(e Entity, val T) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.components[e]; !exists {
		s.entities = append(s.entities, e)
	}
	s.components[e] = val
}

// Get retrieves a component for an entity.
func (s *Store[T]) Get(e Entity) (T, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.components[e]
	return val, ok
}

// Remove deletes a component from an entity.
func (s *Store[T]) Remove(e Entity) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.components[e]; exists {
		delete(s.components, e)
		// Remove from entities slice
		for i, entity := range s.entities {
			if entity == e {
				s.entities[i] = s.entities[len(s.entities)-1]
				s.entities = s.entities[:len(s.entities)-1]
				break
			}
		}
	}
}

// Has checks if entity has this component.
func (s *Store[T]) Has(e Entity) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.components[e]
	return ok
}

// All returns all entities with this component type.
func (s *Store[T]) All() []Entity {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Entity, len(s.entities))
	copy(result, s.entities)
	return result
}

// Count returns number of entities with this component.
func (s *Store[T]) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entities)
}