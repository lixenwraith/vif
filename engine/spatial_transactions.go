package engine

import (
	"fmt"
)

// OperationType represents the type of spatial operation
type OperationType int

const (
	// OpMove represents a move operation (remove from old position, add to new position)
	OpMove OperationType = iota
	// OpSpawn represents a spawn operation (add to position)
	OpSpawn
	// OpDestroy represents a destroy operation (remove from position)
	OpDestroy
)

// SpatialOperation represents a pending spatial index operation
type SpatialOperation struct {
	Type     OperationType
	Entity   Entity
	OldX     int // Used for Move operations
	OldY     int // Used for Move operations
	NewX     int // Used for Move and Spawn operations
	NewY     int // Used for Move and Spawn operations
}

// CollisionResult contains information about a collision
type CollisionResult struct {
	HasCollision bool
	CollidingEntity Entity
	X, Y int
}

// SpatialTransaction manages a set of pending spatial index operations
// All operations are validated for collisions before being added to the transaction
// Commit applies all operations atomically under a single lock
type SpatialTransaction struct {
	world *World
	operations []SpatialOperation
	collisions []CollisionResult
}

// BeginSpatialTransaction creates a new spatial transaction
func (w *World) BeginSpatialTransaction() *SpatialTransaction {
	return &SpatialTransaction{
		world: w,
		operations: make([]SpatialOperation, 0),
		collisions: make([]CollisionResult, 0),
	}
}

// Move adds a move operation to the transaction
// Returns collision information if the target position is occupied
func (tx *SpatialTransaction) Move(entity Entity, oldX, oldY, newX, newY int) CollisionResult {
	// Check for collision at new position
	existingEntity := tx.world.GetEntityAtPosition(newX, newY)

	result := CollisionResult{
		HasCollision: false,
		CollidingEntity: 0,
		X: newX,
		Y: newY,
	}

	// Collision if there's an entity at the target position that's not the entity being moved
	if existingEntity != 0 && existingEntity != entity {
		result.HasCollision = true
		result.CollidingEntity = existingEntity
		tx.collisions = append(tx.collisions, result)
		// Do not add operation if there's a collision
		return result
	}

	// No collision - add move operation
	tx.operations = append(tx.operations, SpatialOperation{
		Type: OpMove,
		Entity: entity,
		OldX: oldX,
		OldY: oldY,
		NewX: newX,
		NewY: newY,
	})

	return result
}

// Spawn adds a spawn operation to the transaction
// Returns collision information if the target position is occupied
func (tx *SpatialTransaction) Spawn(entity Entity, x, y int) CollisionResult {
	// Check for collision at spawn position
	existingEntity := tx.world.GetEntityAtPosition(x, y)

	result := CollisionResult{
		HasCollision: false,
		CollidingEntity: 0,
		X: x,
		Y: y,
	}

	if existingEntity != 0 {
		result.HasCollision = true
		result.CollidingEntity = existingEntity
		tx.collisions = append(tx.collisions, result)
		// Do not add operation if there's a collision
		return result
	}

	// No collision - add spawn operation
	tx.operations = append(tx.operations, SpatialOperation{
		Type: OpSpawn,
		Entity: entity,
		NewX: x,
		NewY: y,
	})

	return result
}

// Destroy adds a destroy operation to the transaction
// This removes the entity from the spatial index at the specified position
func (tx *SpatialTransaction) Destroy(entity Entity, x, y int) {
	tx.operations = append(tx.operations, SpatialOperation{
		Type: OpDestroy,
		Entity: entity,
		OldX: x,
		OldY: y,
	})
}

// HasCollisions returns true if any operations in the transaction had collisions
func (tx *SpatialTransaction) HasCollisions() bool {
	return len(tx.collisions) > 0
}

// GetCollisions returns all collision results from this transaction
func (tx *SpatialTransaction) GetCollisions() []CollisionResult {
	return tx.collisions
}

// Commit applies all pending operations to the spatial index atomically
// This is done under a single lock to ensure consistency
// Returns any error that occurred during commit
func (tx *SpatialTransaction) Commit() error {
	if len(tx.operations) == 0 {
		return nil // Nothing to commit
	}

	tx.world.mu.Lock()
	defer tx.world.mu.Unlock()

	// Apply all operations
	for _, op := range tx.operations {
		switch op.Type {
		case OpMove:
			// Remove from old position
			if row, ok := tx.world.spatialIndex[op.OldY]; ok {
				delete(row, op.OldX)
			}
			// Add to new position
			if tx.world.spatialIndex[op.NewY] == nil {
				tx.world.spatialIndex[op.NewY] = make(map[int]Entity)
			}
			tx.world.spatialIndex[op.NewY][op.NewX] = op.Entity

		case OpSpawn:
			// Add to position
			if tx.world.spatialIndex[op.NewY] == nil {
				tx.world.spatialIndex[op.NewY] = make(map[int]Entity)
			}
			tx.world.spatialIndex[op.NewY][op.NewX] = op.Entity

		case OpDestroy:
			// Remove from position
			if row, ok := tx.world.spatialIndex[op.OldY]; ok {
				delete(row, op.OldX)
			}
		}
	}

	return nil
}

// Rollback clears all pending operations without applying them
func (tx *SpatialTransaction) Rollback() {
	tx.operations = nil
	tx.collisions = nil
}

// String returns a string representation of the transaction for debugging
func (tx *SpatialTransaction) String() string {
	return fmt.Sprintf("SpatialTransaction{operations: %d, collisions: %d}",
		len(tx.operations), len(tx.collisions))
}
