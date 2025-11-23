package engine

import (
	"fmt"
	"sync"

	"github.com/lixenwraith/vi-fighter/components"
)

// PositionStore is a specialized store for PositionComponent that maintains
// a spatial index for fast position-based queries. This store ensures that
// the spatial index is always consistent with the component data.
type PositionStore struct {
	*Store[components.PositionComponent]
	spatialIndex map[int]map[int]Entity // [y][x] -> Entity
	spatialMutex sync.RWMutex
}

// NewPositionStore creates a new position store with spatial indexing.
func NewPositionStore() *PositionStore {
	return &PositionStore{
		Store:        NewStore[components.PositionComponent](),
		spatialIndex: make(map[int]map[int]Entity),
	}
}

// Add overrides the base Store.Add to maintain spatial index consistency.
// This method ensures atomic updates of both the component data and spatial index.
func (ps *PositionStore) Add(e Entity, pos components.PositionComponent) {
	ps.spatialMutex.Lock()
	defer ps.spatialMutex.Unlock()

	// Remove old position from spatial index if entity already has a position
	if oldPos, exists := ps.Store.Get(e); exists {
		if row, ok := ps.spatialIndex[oldPos.Y]; ok {
			delete(row, oldPos.X)
			// Clean up empty rows
			if len(row) == 0 {
				delete(ps.spatialIndex, oldPos.Y)
			}
		}
	}

	// Add to base store
	ps.Store.Add(e, pos)

	// Update spatial index with new position
	if ps.spatialIndex[pos.Y] == nil {
		ps.spatialIndex[pos.Y] = make(map[int]Entity)
	}
	ps.spatialIndex[pos.Y][pos.X] = e
}

// Remove overrides the base Store.Remove to maintain spatial index consistency.
func (ps *PositionStore) Remove(e Entity) {
	ps.spatialMutex.Lock()
	defer ps.spatialMutex.Unlock()

	// Get position before removing
	if pos, exists := ps.Store.Get(e); exists {
		// Remove from spatial index
		if row, ok := ps.spatialIndex[pos.Y]; ok {
			delete(row, pos.X)
			// Clean up empty rows
			if len(row) == 0 {
				delete(ps.spatialIndex, pos.Y)
			}
		}
	}

	// Remove from base store
	ps.Store.Remove(e)
}

// GetEntityAt returns the entity at the specified grid position.
// Returns 0 if no entity exists at that position.
func (ps *PositionStore) GetEntityAt(x, y int) Entity {
	ps.spatialMutex.RLock()
	defer ps.spatialMutex.RUnlock()

	if row, ok := ps.spatialIndex[y]; ok {
		if entity, ok := row[x]; ok {
			return entity
		}
	}
	return 0
}

// Move atomically moves an entity from one position to another.
// This is more efficient than separate Get/Add calls and ensures atomicity.
// Returns an error if the target position is already occupied.
func (ps *PositionStore) Move(e Entity, newPos components.PositionComponent) error {
	ps.spatialMutex.Lock()
	defer ps.spatialMutex.Unlock()

	// Check if target position is occupied by a different entity
	if row, ok := ps.spatialIndex[newPos.Y]; ok {
		if existingEntity, ok := row[newPos.X]; ok && existingEntity != e {
			return fmt.Errorf("position (%d,%d) is occupied by entity %d", newPos.X, newPos.Y, existingEntity)
		}
	}

	// Get old position
	oldPos, exists := ps.Store.Get(e)
	if !exists {
		return fmt.Errorf("entity %d does not have a position component", e)
	}

	// Remove from old position in spatial index
	if row, ok := ps.spatialIndex[oldPos.Y]; ok {
		delete(row, oldPos.X)
		if len(row) == 0 {
			delete(ps.spatialIndex, oldPos.Y)
		}
	}

	// Update component
	ps.Store.Add(e, newPos)

	// Add to new position in spatial index
	if ps.spatialIndex[newPos.Y] == nil {
		ps.spatialIndex[newPos.Y] = make(map[int]Entity)
	}
	ps.spatialIndex[newPos.Y][newPos.X] = e

	return nil
}

// PositionBatch provides transactional batch operations for position updates.
// This is useful for spawning multiple entities atomically with collision detection.
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

	pb.store.spatialMutex.Lock()
	defer pb.store.spatialMutex.Unlock()

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
		if oldPos, exists := pb.store.Store.Get(add.entity); exists {
			if row, ok := pb.store.spatialIndex[oldPos.Y]; ok {
				delete(row, oldPos.X)
				if len(row) == 0 {
					delete(pb.store.spatialIndex, oldPos.Y)
				}
			}
		}

		// Add to base store
		pb.store.Store.Add(add.entity, add.pos)

		// Update spatial index
		if pb.store.spatialIndex[add.pos.Y] == nil {
			pb.store.spatialIndex[add.pos.Y] = make(map[int]Entity)
		}
		pb.store.spatialIndex[add.pos.Y][add.pos.X] = add.entity
	}

	return nil
}

// Clear overrides base Clear to also clear spatial index.
func (ps *PositionStore) Clear() {
	ps.spatialMutex.Lock()
	defer ps.spatialMutex.Unlock()

	ps.Store.Clear()
	ps.spatialIndex = make(map[int]map[int]Entity)
}
