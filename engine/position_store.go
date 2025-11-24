package engine

import (
	"fmt"
	"sync"

	"github.com/lixenwraith/vi-fighter/components"
)

// PositionStore maintains a spatial index for fast position-based queries.
// A single mutex protects both component data and spatial index consistency.
type PositionStore struct {
	mu           sync.RWMutex // Single mutex for all operations
	components   map[Entity]components.PositionComponent
	entities     []Entity               // Dense array for cache-friendly iteration
	spatialIndex map[int]map[int]Entity // [y][x] -> Entity
}

// NewPositionStore creates a new position store with spatial indexing.
func NewPositionStore() *PositionStore {
	return &PositionStore{
		components:   make(map[Entity]components.PositionComponent),
		entities:     make([]Entity, 0, 64),
		spatialIndex: make(map[int]map[int]Entity),
	}
}

// Add atomically updates component and spatial index.
func (ps *PositionStore) Add(e Entity, pos components.PositionComponent) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Remove old position from spatial index if entity already has a position
	if oldPos, exists := ps.components[e]; exists {
		if row, ok := ps.spatialIndex[oldPos.Y]; ok {
			delete(row, oldPos.X)
			// Clean up empty rows
			if len(row) == 0 {
				delete(ps.spatialIndex, oldPos.Y)
			}
		}
	} else {
		// New entity, add to dense array
		ps.entities = append(ps.entities, e)
	}

	// Update component
	ps.components[e] = pos

	// Update spatial index with new position
	if ps.spatialIndex[pos.Y] == nil {
		ps.spatialIndex[pos.Y] = make(map[int]Entity)
	}
	ps.spatialIndex[pos.Y][pos.X] = e
}

// Remove deletes a position component from an entity and updates the spatial index.
func (ps *PositionStore) Remove(e Entity) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Get position before removing
	if pos, exists := ps.components[e]; exists {
		// Remove from spatial index
		if row, ok := ps.spatialIndex[pos.Y]; ok {
			delete(row, pos.X)
			// Clean up empty rows
			if len(row) == 0 {
				delete(ps.spatialIndex, pos.Y)
			}
		}

		// Remove from components map
		delete(ps.components, e)

		// Remove from dense entities array
		for i, entity := range ps.entities {
			if entity == e {
				ps.entities[i] = ps.entities[len(ps.entities)-1]
				ps.entities = ps.entities[:len(ps.entities)-1]
				break
			}
		}
	}
}

// GetEntityAt returns the entity at the specified grid position.
// Returns 0 if no entity exists at that position.
func (ps *PositionStore) GetEntityAt(x, y int) Entity {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	if row, ok := ps.spatialIndex[y]; ok {
		if entity, ok := row[x]; ok {
			return entity
		}
	}
	return 0
}

// Move atomically updates position with collision detection.
// Returns error if target position is occupied.
func (ps *PositionStore) Move(e Entity, newPos components.PositionComponent) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Get old position
	oldPos, exists := ps.components[e]
	if !exists {
		return fmt.Errorf("entity %d does not have a position component", e)
	}

	// Check if target position is occupied by a different entity
	if row, ok := ps.spatialIndex[newPos.Y]; ok {
		if existingEntity, ok := row[newPos.X]; ok && existingEntity != e {
			return fmt.Errorf("position (%d,%d) is occupied by entity %d", newPos.X, newPos.Y, existingEntity)
		}
	}

	// Remove from old position in spatial index
	if row, ok := ps.spatialIndex[oldPos.Y]; ok {
		delete(row, oldPos.X)
		if len(row) == 0 {
			delete(ps.spatialIndex, oldPos.Y)
		}
	}

	// Update component
	ps.components[e] = newPos

	// Add to new position in spatial index
	if ps.spatialIndex[newPos.Y] == nil {
		ps.spatialIndex[newPos.Y] = make(map[int]Entity)
	}
	ps.spatialIndex[newPos.Y][newPos.X] = e

	return nil
}

// PositionBatch provides transactional batch operations with collision detection.
type PositionBatch struct {
	store     *PositionStore
	additions []positionAddition
	committed bool
}

type positionAddition struct {
	entity Entity
	pos    components.PositionComponent
}

// BeginBatch starts a new batch transaction for position updates.
func (ps *PositionStore) BeginBatch() *PositionBatch {
	return &PositionBatch{
		store:     ps,
		additions: make([]positionAddition, 0),
	}
}

// Add queues an entity position for addition in this batch.
func (pb *PositionBatch) Add(e Entity, pos components.PositionComponent) {
	pb.additions = append(pb.additions, positionAddition{entity: e, pos: pos})
}

// Commit applies all batched position additions atomically.
// Returns an error if any position collides with existing entities or other batch additions.
func (pb *PositionBatch) Commit() error {
	if pb.committed {
		return fmt.Errorf("batch already committed")
	}
	pb.committed = true

	pb.store.mu.Lock()
	defer pb.store.mu.Unlock()

	// First pass: check for collisions with existing entities and within batch
	occupied := make(map[int]map[int]bool)
	for _, add := range pb.additions {
		// Check collision with existing entities
		if row, ok := pb.store.spatialIndex[add.pos.Y]; ok {
			if existingEntity, ok := row[add.pos.X]; ok && existingEntity != add.entity {
				return fmt.Errorf("position (%d,%d) is occupied by entity %d", add.pos.X, add.pos.Y, existingEntity)
			}
		}

		// Check collision within batch
		if occupied[add.pos.Y] == nil {
			occupied[add.pos.Y] = make(map[int]bool)
		}
		if occupied[add.pos.Y][add.pos.X] {
			return fmt.Errorf("batch contains multiple additions at position (%d,%d)", add.pos.X, add.pos.Y)
		}
		occupied[add.pos.Y][add.pos.X] = true
	}

	// Second pass: apply all additions (no more checks needed)
	for _, add := range pb.additions {
		// Remove old position if entity already exists
		if oldPos, exists := pb.store.components[add.entity]; exists {
			if row, ok := pb.store.spatialIndex[oldPos.Y]; ok {
				delete(row, oldPos.X)
				if len(row) == 0 {
					delete(pb.store.spatialIndex, oldPos.Y)
				}
			}
		} else {
			// New entity, add to dense array
			pb.store.entities = append(pb.store.entities, add.entity)
		}

		// Update component
		pb.store.components[add.entity] = add.pos

		// Update spatial index
		if pb.store.spatialIndex[add.pos.Y] == nil {
			pb.store.spatialIndex[add.pos.Y] = make(map[int]Entity)
		}
		pb.store.spatialIndex[add.pos.Y][add.pos.X] = add.entity
	}

	return nil
}

// Get retrieves a position component for an entity.
func (ps *PositionStore) Get(e Entity) (components.PositionComponent, bool) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	val, ok := ps.components[e]
	return val, ok
}

// Has checks if an entity has a position component.
func (ps *PositionStore) Has(e Entity) bool {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	_, ok := ps.components[e]
	return ok
}

// All returns all entities with position components.
func (ps *PositionStore) All() []Entity {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	result := make([]Entity, len(ps.entities))
	copy(result, ps.entities)
	return result
}

// Count returns the number of entities with position components.
func (ps *PositionStore) Count() int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return len(ps.entities)
}

// Clear removes all position components and clears the spatial index.
func (ps *PositionStore) Clear() {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ps.components = make(map[Entity]components.PositionComponent)
	ps.entities = make([]Entity, 0, 64)
	ps.spatialIndex = make(map[int]map[int]Entity)
}
