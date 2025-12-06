package engine

import (
	"fmt"
	"sync"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
)

// PositionStore maintains a spatial index using a fixed-capacity dense grid
// It supports multiple entities per cell (up to 15)
type PositionStore struct {
	mu         sync.RWMutex
	components map[Entity]components.PositionComponent
	entities   []Entity // Dense array for cache-friendly iteration
	grid       *SpatialGrid
	world      *World // Reference for z-index lookups
}

// NewPositionStore creates a new position store with spatial indexing
func NewPositionStore() *PositionStore {
	// Default grid size, will be resized by GameContext if needed
	return &PositionStore{
		components: make(map[Entity]components.PositionComponent),
		entities:   make([]Entity, 0, 64),
		grid:       NewSpatialGrid(constants.DefaultGridWidth, constants.DefaultGridHeight), // Default safe size
	}
}

// Add inserts or updates an entity's position
// This handles the "cursor ghosting" bug by allowing multiple entities at the same location
func (ps *PositionStore) Add(e Entity, pos components.PositionComponent) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// If entity already has a position, remove it from old grid location
	if oldPos, exists := ps.components[e]; exists {
		ps.grid.Remove(e, oldPos.X, oldPos.Y)
	} else {
		// New entity, add to dense array
		ps.entities = append(ps.entities, e)
	}

	// Update component
	ps.components[e] = pos

	// Add to new grid location
	// NOTE: Grid overflow is silently ignored
	// In real gameplay, 15 entities per cell is sufficient
	// If this becomes an issue, expand MaxEntitiesPerCell or implement cell chaining
	// Current design prioritizes cache locality over edge case handling
	_ = ps.grid.Add(e, pos.X, pos.Y)
}

// Remove deletes an entity from the store and grid
func (ps *PositionStore) Remove(e Entity) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if pos, exists := ps.components[e]; exists {
		// Remove from spatial grid
		ps.grid.Remove(e, pos.X, pos.Y)

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

// Move updates position atomically
// Note: This version ignores collisions at the Store level
// Systems should use HasAny() or GetAllAt() for collision logic before moving if needed
func (ps *PositionStore) Move(e Entity, newPos components.PositionComponent) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	oldPos, exists := ps.components[e]
	if !exists {
		return fmt.Errorf("entity %d does not have a position component", e)
	}

	// Remove from old grid pos
	ps.grid.Remove(e, oldPos.X, oldPos.Y)

	// Update component
	ps.components[e] = newPos

	// Add to new grid pos
	// Note: explicit ignore for OOB and Cell full
	_ = ps.grid.Add(e, newPos.X, newPos.Y)

	return nil
}

// GetAllAt returns a COPY of entities at the given position (concurrent safe but uses memory)
// Returns nil if position is out of bounds or empty
func (ps *PositionStore) GetAllAt(x, y int) []Entity {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	view := ps.grid.GetAllAt(x, y)
	if len(view) == 0 {
		return nil
	}

	// Allocate new slice to detach from grid memory
	result := make([]Entity, len(view))
	copy(result, view)
	return result
}

// GetAllAtInto copies entities into a caller-provided buffer and returns number of entities copied
// SAFE and ZERO-ALLOCATION if buf is on stack
// Use for Render/Physics
func (ps *PositionStore) GetAllAtInto(x, y int, buf []Entity) int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	view := ps.grid.GetAllAt(x, y)
	// Copy min(len(buf), len(view))
	return copy(buf, view)
}

// HasAny O(1) returns true if any entity exists at the given coordinates
func (ps *PositionStore) HasAny(x, y int) bool {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.grid.HasAny(x, y)
}

// ResizeGrid resizes the internal spatial grid
func (ps *PositionStore) ResizeGrid(width, height int) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Create new grid
	ps.grid.Resize(width, height)

	// Re-populate grid from components map
	// This ensures consistency even if grid size changes
	for e, pos := range ps.components {
		// Note: explicit ignore for OOB and Cell full
		_ = ps.grid.Add(e, pos.X, pos.Y)
	}
}

// Get retrieves a position component
func (ps *PositionStore) Get(e Entity) (components.PositionComponent, bool) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	val, ok := ps.components[e]
	return val, ok
}

// Has checks if an entity has a position component
func (ps *PositionStore) Has(e Entity) bool {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	_, ok := ps.components[e]
	return ok
}

// All returns all entities with position components
func (ps *PositionStore) All() []Entity {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	result := make([]Entity, len(ps.entities))
	copy(result, ps.entities)
	return result
}

// Count returns the number of entities
func (ps *PositionStore) Count() int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return len(ps.entities)
}

// Clear removes all data
func (ps *PositionStore) Clear() {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ps.components = make(map[Entity]components.PositionComponent)
	ps.entities = make([]Entity, 0, 64)
	ps.grid.Clear()
}

// SetWorld sets the world reference for z-index lookups
func (ps *PositionStore) SetWorld(w *World) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.world = w
}

// GetTopEntityFiltered returns the highest z-index entity at position that passes filter
// Returns 0 if no matching entity found
func (ps *PositionStore) GetTopEntityFiltered(x, y int, world *World, filter func(Entity) bool) Entity {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	view := ps.grid.GetAllAt(x, y)
	if len(view) == 0 {
		return 0
	}

	return SelectTopEntityFiltered(view, world, filter)
}

// --- Batch Implementation ---

type PositionBatch struct {
	store     *PositionStore
	additions []positionAddition
	committed bool
}

type positionAddition struct {
	entity Entity
	pos    components.PositionComponent
}

func (ps *PositionStore) BeginBatch() *PositionBatch {
	return &PositionBatch{
		store:     ps,
		additions: make([]positionAddition, 0),
	}
}

func (pb *PositionBatch) Add(e Entity, pos components.PositionComponent) {
	pb.additions = append(pb.additions, positionAddition{entity: e, pos: pos})
}

// Commit applies all batched additions
// Checks with HasAny only to prevent unintended spawns on existing entities
func (pb *PositionBatch) Commit() error {
	if pb.committed {
		return fmt.Errorf("batch already committed")
	}
	pb.committed = true

	pb.store.mu.Lock()
	defer pb.store.mu.Unlock()

	// 1. Validation phase (Gameplay logic: don't spawn on top of things)
	// Check both the current grid AND the pending batch for conflicts
	batchOccupied := make(map[int]map[int]bool)

	for _, add := range pb.additions {
		// Check against existing entities
		if pb.store.grid.HasAny(add.pos.X, add.pos.Y) {
			// Collision found in world
			return fmt.Errorf("position (%d,%d) is occupied", add.pos.X, add.pos.Y)
		}

		// Check against other items in this batch
		if batchOccupied[add.pos.Y] == nil {
			batchOccupied[add.pos.Y] = make(map[int]bool)
		}
		if batchOccupied[add.pos.Y][add.pos.X] {
			return fmt.Errorf("batch conflict at position (%d,%d)", add.pos.X, add.pos.Y)
		}
		batchOccupied[add.pos.Y][add.pos.X] = true
	}

	// 2. Application phase
	for _, add := range pb.additions {
		// Remove old position if exists
		if oldPos, exists := pb.store.components[add.entity]; exists {
			pb.store.grid.Remove(add.entity, oldPos.X, oldPos.Y)
		} else {
			pb.store.entities = append(pb.store.entities, add.entity)
		}

		pb.store.components[add.entity] = add.pos
		// Note: explicit ignore for OOB and Cell full
		_ = pb.store.grid.Add(add.entity, add.pos.X, add.pos.Y)
	}

	return nil
}