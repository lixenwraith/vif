package engine

import (
	"fmt"

	"github.com/lixenwraith/vi-fighter/components"
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
	Type   OperationType
	Entity Entity
	OldX   int // Used for Move operations
	OldY   int // Used for Move operations
	NewX   int // Used for Move and Spawn operations
	NewY   int // Used for Move and Spawn operations
}

// CollisionResult contains information about a collision
type CollisionResult struct {
	HasCollision    bool
	CollidingEntity Entity
	X, Y            int
}

// SpatialTransaction manages a set of pending spatial index operations
// All operations are validated for collisions before being added to the transaction
// Commit applies all operations atomically under a single lock
type SpatialTransaction struct {
	world      *World
	operations []SpatialOperation
	collisions []CollisionResult
}

// BeginSpatialTransaction creates a new spatial transaction
func (w *World) BeginSpatialTransaction() *SpatialTransaction {
	return &SpatialTransaction{
		world:      w,
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
		HasCollision:    false,
		CollidingEntity: 0,
		X:               newX,
		Y:               newY,
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
		Type:   OpMove,
		Entity: entity,
		OldX:   oldX,
		OldY:   oldY,
		NewX:   newX,
		NewY:   newY,
	})

	return result
}

// Spawn adds a spawn operation to the transaction
// Returns collision information if the target position is occupied
func (tx *SpatialTransaction) Spawn(entity Entity, x, y int) CollisionResult {
	// Check for collision at spawn position
	existingEntity := tx.world.GetEntityAtPosition(x, y)

	result := CollisionResult{
		HasCollision:    false,
		CollidingEntity: 0,
		X:               x,
		Y:               y,
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
		Type:   OpSpawn,
		Entity: entity,
		NewX:   x,
		NewY:   y,
	})

	return result
}

// Destroy adds a destroy operation to the transaction
// This removes the entity from the spatial index at the specified position
func (tx *SpatialTransaction) Destroy(entity Entity, x, y int) {
	tx.operations = append(tx.operations, SpatialOperation{
		Type:   OpDestroy,
		Entity: entity,
		OldX:   x,
		OldY:   y,
	})
}

// EntityPosition represents an entity and its spawn position
type EntityPosition struct {
	Entity Entity
	X      int
	Y      int
}

// SpawnBatch validates all positions first, then adds all spawn operations atomically
// This ensures entire batch either succeeds or fails - no partial spawns
// Returns collision result with information about which position had a collision
func (tx *SpatialTransaction) SpawnBatch(entities []EntityPosition) CollisionResult {
	// Phase 1: Validate ALL positions first (before adding any operations)
	for _, ep := range entities {
		existingEntity := tx.world.GetEntityAtPosition(ep.X, ep.Y)
		if existingEntity != 0 {
			// Collision detected - return immediately without adding any operations
			result := CollisionResult{
				HasCollision:    true,
				CollidingEntity: existingEntity,
				X:               ep.X,
				Y:               ep.Y,
			}
			tx.collisions = append(tx.collisions, result)
			return result
		}
	}

	// Phase 2: All positions are clear - add all spawn operations to transaction
	for _, ep := range entities {
		tx.operations = append(tx.operations, SpatialOperation{
			Type:   OpSpawn,
			Entity: ep.Entity,
			NewX:   ep.X,
			NewY:   ep.Y,
		})
	}

	// Return success result
	return CollisionResult{
		HasCollision:    false,
		CollidingEntity: 0,
		X:               0,
		Y:               0,
	}
}

// HasCollisions returns true if any operations in the transaction had collisions
func (tx *SpatialTransaction) HasCollisions() bool {
	return len(tx.collisions) > 0
}

// GetCollisions returns all collision results from this transaction
func (tx *SpatialTransaction) GetCollisions() []CollisionResult {
	return tx.collisions
}

// Commit applies all pending operations using the PositionStore's thread-safe API.
func (tx *SpatialTransaction) Commit() error {
	if len(tx.operations) == 0 {
		return nil // Nothing to commit
	}

	// NOTE: We do NOT lock tx.world.mu here.
	// PositionStore has its own internal RWMutex for thread-safe operations.
	// Operations are applied sequentially; for atomic batching of multiple operations,
	// use PositionStore's batch API instead.

	posStore := tx.world.Positions

	for _, op := range tx.operations {
		switch op.Type {
		case OpMove:
			// Construct the component for the new position
			newPos := components.PositionComponent{X: op.NewX, Y: op.NewY}

			// Use PositionStore.Move which handles:
			// 1. Locking
			// 2. Spatial Index updates
			// 3. Component map updates
			// 4. Collision checks (again, for safety)
			if err := posStore.Move(op.Entity, newPos); err != nil {
				return fmt.Errorf("failed to commit move for entity %d: %w", op.Entity, err)
			}

		case OpSpawn:
			newPos := components.PositionComponent{X: op.NewX, Y: op.NewY}
			posStore.Add(op.Entity, newPos)

		case OpDestroy:
			posStore.Remove(op.Entity)
		}
	}

	return nil
}