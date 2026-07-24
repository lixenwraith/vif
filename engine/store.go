package engine

import (
	"github.com/lixenwraith/vi-fighter/core"
)

// Store is a sparse-set container for component type T.
//
// SYNC INVARIANT: all access occurs under World.updateMutex. The tick
// scheduler, event loop, render pass, and input path each acquire it;
// there is no internal locking.
//
// Layout: dense[i] pairs with entities[i]; index maps entity -> dense slot.
// Removal swap-removes and rewrites the moved entity's index, so indices
// are valid by construction — no tombstones, no stale slots.
type Store[T any] struct {
	world    *World
	index    map[core.Entity]int32
	dense    []T
	entities []core.Entity // parallel to dense

	bit uint64
}

// NewStore creates a new component store for type T
func NewStore[T any](w *World, bit uint64) *Store[T] {
	return &Store[T]{
		index:    make(map[core.Entity]int32, 256),
		dense:    make([]T, 0, 256),
		entities: make([]core.Entity, 0, 256),
		world:    w,
		bit:      bit,
	}
}

// SetComponent inserts or overwrites a component for an entity
func (s *Store[T]) SetComponent(e core.Entity, val T) {
	if i, ok := s.index[e]; ok {
		s.dense[i] = val
		return
	}
	s.index[e] = int32(len(s.dense))
	s.dense = append(s.dense, val)
	s.entities = append(s.entities, e)
	s.world.AddComponentMask(e, s.bit)
}

// GetComponent retrieves a component by value (compatibility API)
func (s *Store[T]) GetComponent(e core.Entity) (T, bool) {
	if i, ok := s.index[e]; ok {
		return s.dense[i], true
	}
	var zero T
	return zero, false
}

// GetPtr returns a direct pointer to the stored component.
// The pointer is valid until the next structural change on THIS store:
// SetComponent of a new entity (append may reallocate) or any removal
// (swap-remove moves elements). Mutations through the pointer persist
// without a SetComponent write-back.
func (s *Store[T]) GetPtr(e core.Entity) (*T, bool) {
	if i, ok := s.index[e]; ok {
		return &s.dense[i], true
	}
	return nil, false
}

// HasEntity checks if entity has this component
func (s *Store[T]) HasEntity(e core.Entity) bool {
	_, ok := s.index[e]
	return ok
}

// removeAt swap-removes dense slot i, fixing the moved entity's index
func (s *Store[T]) removeAt(i int32) {
	e := s.entities[i]
	last := int32(len(s.dense) - 1)

	if i != last {
		moved := s.entities[last]
		s.dense[i] = s.dense[last]
		s.entities[i] = moved
		s.index[moved] = i
	}

	// Zero the vacated tail slot so pointer-bearing components
	// (Genes []float64, MemberEntries, etc.) release their references
	var zero T
	s.dense[last] = zero
	s.dense = s.dense[:last]
	s.entities = s.entities[:last]
	delete(s.index, e)
}

// RemoveEntity deletes a component from an entity. O(1)
func (s *Store[T]) RemoveEntity(e core.Entity, skipMask ...bool) {
	i, ok := s.index[e]
	if !ok {
		return
	}
	s.removeAt(i)
	if len(skipMask) == 0 || !skipMask[0] {
		s.world.RemoveComponentMask(e, s.bit)
	}
}

// RemoveBatch deletes multiple entities. O(m), no allocation
func (s *Store[T]) RemoveBatch(entities []core.Entity, skipMask ...bool) {
	if len(s.index) == 0 {
		return
	}
	clearMask := len(skipMask) == 0 || !skipMask[0]
	for _, e := range entities {
		i, ok := s.index[e]
		if !ok {
			continue
		}
		s.removeAt(i)
		if clearMask {
			s.world.RemoveComponentMask(e, s.bit)
		}
	}
}

// GetAllEntities returns a detached copy (compatibility API).
// Safe to iterate while destroying. Prefer Entities()/Each for hot paths.
func (s *Store[T]) GetAllEntities() []core.Entity {
	result := make([]core.Entity, len(s.entities))
	copy(result, s.entities)
	return result
}

// Entities returns the live dense entity slice — zero allocation.
// CONTRACT: no removals from this store while ranging it; collect
// candidates and remove after the loop (the existing system pattern).
// Additions during the range are visible only via re-checking len.
func (s *Store[T]) Entities() []core.Entity {
	return s.entities
}

// Each iterates entity/component pairs in dense order with direct pointers.
// Return false to stop. Same removal contract as Entities().
// Additions during iteration are not visited (length snapshotted).
func (s *Store[T]) Each(fn func(e core.Entity, c *T) bool) {
	n := len(s.entities)
	for i := range n {
		if !fn(s.entities[i], &s.dense[i]) {
			return
		}
	}
}

// CountEntities returns number of entities with this component
func (s *Store[T]) CountEntities() int {
	return len(s.entities)
}

// ClearAllComponents removes all components, retaining capacity
func (s *Store[T]) ClearAllComponents() {
	clear(s.index)
	clear(s.dense) // zero elements: release inner pointers
	s.dense = s.dense[:0]
	s.entities = s.entities[:0]
}
