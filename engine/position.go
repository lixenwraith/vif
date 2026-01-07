package engine

import (
	"fmt"
	"sync"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
)

// Position maintains a spatial index using a fixed-capacity dense grid
// It supports multiple entities per cell (up to MaxEntitiesPerCell)
type Position struct {
	mu         sync.RWMutex
	components map[core.Entity]component.PositionComponent
	entities   []core.Entity // Dense array for cache-friendly iteration
	grid       *SpatialGrid
	world      *World // Reference for z-index lookups
}

// NewPosition creates a new position store with spatial indexing
func NewPosition() *Position {
	// Default grid size, will be resized by GameContext if needed
	return &Position{
		components: make(map[core.Entity]component.PositionComponent),
		entities:   make([]core.Entity, 0, 64),
		grid:       NewSpatialGrid(constant.DefaultGridWidth, constant.DefaultGridHeight), // Default safe size
	}
}

// Set inserts or updates an entity's position
// Multiple entities at the same location are allowed, overflow silently ignored
func (p *Position) Set(e core.Entity, pos component.PositionComponent) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// If entity already has a position, remove it from old grid location
	if oldPos, exists := p.components[e]; exists {
		p.grid.Remove(e, oldPos.X, oldPos.Y)
	} else {
		// New entity, add to dense array
		p.entities = append(p.entities, e)
	}

	// Update component
	p.components[e] = pos

	// Set to new grid location
	_ = p.grid.Add(e, pos.X, pos.Y)
}

// Remove deletes an entity from the store and grid
func (p *Position) Remove(e core.Entity) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if pos, exists := p.components[e]; exists {
		// Remove from spatial grid
		p.grid.Remove(e, pos.X, pos.Y)

		// Remove from components map
		delete(p.components, e)

		// Remove from dense entities array
		for i, entity := range p.entities {
			if entity == e {
				p.entities[i] = p.entities[len(p.entities)-1]
				p.entities = p.entities[:len(p.entities)-1]
				break
			}
		}
	}
}

// Move updates position atomically
// Systems should use HasAny() or GetAllAt() for collision logic before moving if needed
func (p *Position) Move(e core.Entity, newPos component.PositionComponent) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	oldPos, exists := p.components[e]
	if !exists {
		return fmt.Errorf("entity %d does not have a position component", e)
	}

	// Remove from old grid pos
	p.grid.Remove(e, oldPos.X, oldPos.Y)

	// Update component
	p.components[e] = newPos

	// Set to new grid pos
	// Explicit ignore for OOB and Cell full
	_ = p.grid.Add(e, newPos.X, newPos.Y)

	return nil
}

// GetAllAt returns a COPY of entities at the given position (concurrent safe but uses memory),
// Returns nil if position is out of bounds or empty
func (p *Position) GetAllAt(x, y int) []core.Entity {
	p.mu.RLock()
	defer p.mu.RUnlock()

	view := p.grid.GetAllAt(x, y)
	if len(view) == 0 {
		return nil
	}

	// Allocate new slice to detach from grid memory
	result := make([]core.Entity, len(view))
	copy(result, view)
	return result
}

// GetAllAtInto copies entities into a caller-provided buffer and returns number of entities copied
// SAFE and ZERO-ALLOCATION if buf is on stack
// Use for Render/Physics
func (p *Position) GetAllAtInto(x, y int, buf []core.Entity) int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	view := p.grid.GetAllAt(x, y)
	// Copy min(len(buf), len(view))
	return copy(buf, view)
}

// HasAny O(1) returns true if any entity exists at the given coordinates
func (p *Position) HasAny(x, y int) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.grid.HasAny(x, y)
}

// ResizeGrid resizes the internal spatial grid
func (p *Position) ResizeGrid(width, height int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Create new grid
	p.grid.Resize(width, height)

	// Re-populate grid from components map
	// This ensures consistency even if grid size changes
	for e, pos := range p.components {
		// Explicit ignore for OOB and Cell full
		_ = p.grid.Add(e, pos.X, pos.Y)
	}
}

// Get retrieves a position component
func (p *Position) Get(e core.Entity) (component.PositionComponent, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	val, ok := p.components[e]
	return val, ok
}

// Has checks if an entity has a position component
func (p *Position) Has(e core.Entity) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, ok := p.components[e]
	return ok
}

// All returns all entities with position components
func (p *Position) All() []core.Entity {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]core.Entity, len(p.entities))
	copy(result, p.entities)
	return result
}

// Count returns the number of entities
func (p *Position) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.entities)
}

// Clear removes all data
func (p *Position) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.components = make(map[core.Entity]component.PositionComponent)
	p.entities = make([]core.Entity, 0, 64)
	p.grid.Clear()
}

// SetWorld sets the world reference for z-index lookups
func (p *Position) SetWorld(w *World) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.world = w
}

// --- Unsafe operation ---

// Lock manually acquires the write lock for bulk operations.
// CRITICAL: Blocks all other readers/writers. Must be paired with Unlock().
// Use only in high-performance system updates (e.g., DustSystem).
func (p *Position) Lock() {
	p.mu.Lock()
}

// Unlock releases the write lock manually.
func (p *Position) Unlock() {
	p.mu.Unlock()
}

// GetUnsafe retrieves position without locking.
// CRITICAL: Caller MUST hold Lock/RLock.
func (p *Position) GetUnsafe(e core.Entity) (component.PositionComponent, bool) {
	val, ok := p.components[e]
	return val, ok
}

// MoveUnsafe updates position without locking.
// CRITICAL: Caller MUST hold Lock().
func (p *Position) MoveUnsafe(e core.Entity, newPos component.PositionComponent) {
	oldPos, exists := p.components[e]
	if !exists {
		return
	}
	p.grid.Remove(e, oldPos.X, oldPos.Y)
	p.components[e] = newPos
	// Explicit ignore for OOB and Cell full
	_ = p.grid.Add(e, newPos.X, newPos.Y)
}

// GetAllAtIntoUnsafe copies entities at (x,y) into buf without locking.
// CRITICAL: Caller MUST hold Lock/RLock.
// Returns number of entities copied.
func (p *Position) GetAllAtIntoUnsafe(x, y int, buf []core.Entity) int {
	if x < 0 || x >= p.grid.Width || y < 0 || y >= p.grid.Height {
		return 0
	}

	// Direct grid access is safe because we hold the lock
	idx := y*p.grid.Width + x
	cell := &p.grid.Cells[idx]
	count := int(cell.Count)

	if count == 0 {
		return 0
	}

	if count > len(buf) {
		count = len(buf)
	}

	copy(buf, cell.Entities[:count])
	return count
}

// --- Batch Implementation ---

type PositionBatch struct {
	store     *Position
	additions []positionAddition
	committed bool
}

type positionAddition struct {
	entity core.Entity
	pos    component.PositionComponent
}

func (p *Position) BeginBatch() *PositionBatch {
	return &PositionBatch{
		store:     p,
		additions: make([]positionAddition, 0),
	}
}

func (pb *PositionBatch) Add(e core.Entity, pos component.PositionComponent) {
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
		// Explicit ignore for OOB and Cell full
		_ = pb.store.grid.Add(add.entity, add.pos.X, add.pos.Y)
	}

	return nil
}

// CommitForce applies batched additions without checking for existing entity collisions
// Used for effects like Dust that overlay existing entities or replace them before death processing
func (pb *PositionBatch) CommitForce() {
	if pb.committed {
		return
	}
	pb.committed = true

	pb.store.mu.Lock()
	defer pb.store.mu.Unlock()

	for _, add := range pb.additions {
		// Remove old position if exists
		if oldPos, exists := pb.store.components[add.entity]; exists {
			pb.store.grid.Remove(add.entity, oldPos.X, oldPos.Y)
		} else {
			pb.store.entities = append(pb.store.entities, add.entity)
		}

		pb.store.components[add.entity] = add.pos
		// Explicit ignore for OOB and Cell full
		_ = pb.store.grid.Add(add.entity, add.pos.X, add.pos.Y)
	}
}