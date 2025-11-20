package engine

import (
	"sync"
	"testing"
	"time"
)

// Note: TestComponent is defined in ecs_test.go as TestComponent
// We'll use TestComponent for these tests to avoid redeclaration

// TestSpatialTransaction_MoveSuccess tests successful move operation
func TestSpatialTransaction_MoveSuccess(t *testing.T) {
	world := NewWorld()

	// Create entity at position (0, 0)
	entity := world.CreateEntity()
	world.AddComponent(entity, TestComponent{X: 0, Y: 0})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(entity, 0, 0)
		tx.Commit()
	}

	// Verify entity is at (0, 0)
	if world.GetEntityAtPosition(0, 0) != entity {
		t.Errorf("Expected entity at (0, 0)")
	}

	// Move to (5, 5) using transaction
	tx := world.BeginSpatialTransaction()
	result := tx.Move(entity, 0, 0, 5, 5)

	// Verify no collision
	if result.HasCollision {
		t.Errorf("Expected no collision, got collision with entity %d", result.CollidingEntity)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Verify entity is now at (5, 5)
	if world.GetEntityAtPosition(5, 5) != entity {
		t.Errorf("Expected entity at (5, 5) after move")
	}

	// Verify old position is empty
	if world.GetEntityAtPosition(0, 0) != 0 {
		t.Errorf("Expected no entity at old position (0, 0)")
	}
}

// TestSpatialTransaction_MoveCollision tests move operation with collision
func TestSpatialTransaction_MoveCollision(t *testing.T) {
	world := NewWorld()

	// Create two entities
	entity1 := world.CreateEntity()
	world.AddComponent(entity1, TestComponent{X: 0, Y: 0})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(entity1, 0, 0)
		tx.Commit()
	}

	entity2 := world.CreateEntity()
	world.AddComponent(entity2, TestComponent{X: 5, Y: 5})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(entity2, 5, 5)
		tx.Commit()
	}

	// Try to move entity1 to entity2's position
	tx := world.BeginSpatialTransaction()
	result := tx.Move(entity1, 0, 0, 5, 5)

	// Verify collision detected
	if !result.HasCollision {
		t.Errorf("Expected collision, got none")
	}

	if result.CollidingEntity != entity2 {
		t.Errorf("Expected collision with entity %d, got %d", entity2, result.CollidingEntity)
	}

	// Transaction should have collisions
	if !tx.HasCollisions() {
		t.Errorf("Transaction should report collisions")
	}

	// Commit should still work (even though operation wasn't added)
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Verify entity1 is still at (0, 0) - move was not added to transaction
	if world.GetEntityAtPosition(0, 0) != entity1 {
		t.Errorf("Expected entity1 still at (0, 0) after collision")
	}

	// Verify entity2 is still at (5, 5)
	if world.GetEntityAtPosition(5, 5) != entity2 {
		t.Errorf("Expected entity2 still at (5, 5) after collision")
	}
}

// TestSpatialTransaction_SpawnSuccess tests successful spawn operation
func TestSpatialTransaction_SpawnSuccess(t *testing.T) {
	world := NewWorld()

	// Create entity (not in spatial index yet)
	entity := world.CreateEntity()
	world.AddComponent(entity, TestComponent{X: 10, Y: 10})

	// Spawn at (10, 10) using transaction
	tx := world.BeginSpatialTransaction()
	result := tx.Spawn(entity, 10, 10)

	// Verify no collision
	if result.HasCollision {
		t.Errorf("Expected no collision, got collision with entity %d", result.CollidingEntity)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Verify entity is now in spatial index at (10, 10)
	if world.GetEntityAtPosition(10, 10) != entity {
		t.Errorf("Expected entity at (10, 10) after spawn")
	}
}

// TestSpatialTransaction_SpawnCollision tests spawn operation on occupied position
func TestSpatialTransaction_SpawnCollision(t *testing.T) {
	world := NewWorld()

	// Create and place first entity
	entity1 := world.CreateEntity()
	world.AddComponent(entity1, TestComponent{X: 7, Y: 7})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(entity1, 7, 7)
		tx.Commit()
	}

	// Create second entity
	entity2 := world.CreateEntity()
	world.AddComponent(entity2, TestComponent{X: 7, Y: 7})

	// Try to spawn entity2 at same position as entity1
	tx := world.BeginSpatialTransaction()
	result := tx.Spawn(entity2, 7, 7)

	// Verify collision detected
	if !result.HasCollision {
		t.Errorf("Expected collision, got none")
	}

	if result.CollidingEntity != entity1 {
		t.Errorf("Expected collision with entity %d, got %d", entity1, result.CollidingEntity)
	}

	// Commit should work (no operations to commit)
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Verify entity1 is still at (7, 7)
	if world.GetEntityAtPosition(7, 7) != entity1 {
		t.Errorf("Expected entity1 at (7, 7)")
	}
}

// TestSpatialTransaction_Destroy tests destroy operation
func TestSpatialTransaction_Destroy(t *testing.T) {
	world := NewWorld()

	// Create entity at position (3, 3)
	entity := world.CreateEntity()
	world.AddComponent(entity, TestComponent{X: 3, Y: 3})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(entity, 3, 3)
		tx.Commit()
	}

	// Verify entity exists in spatial index
	if world.GetEntityAtPosition(3, 3) != entity {
		t.Errorf("Expected entity at (3, 3)")
	}

	// Destroy using transaction
	tx := world.BeginSpatialTransaction()
	tx.Destroy(entity, 3, 3)

	// Commit transaction
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Verify entity is removed from spatial index
	if world.GetEntityAtPosition(3, 3) != 0 {
		t.Errorf("Expected no entity at (3, 3) after destroy")
	}
}

// TestSpatialTransaction_Rollback tests transaction rollback
func TestSpatialTransaction_Rollback(t *testing.T) {
	world := NewWorld()

	// Create entity at (0, 0)
	entity := world.CreateEntity()
	world.AddComponent(entity, TestComponent{X: 0, Y: 0})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(entity, 0, 0)
		tx.Commit()
	}

	// Create transaction with move
	tx := world.BeginSpatialTransaction()
	tx.Move(entity, 0, 0, 5, 5)

	// Rollback instead of commit
	tx.Rollback()

	// Verify entity is still at (0, 0) - move was not applied
	if world.GetEntityAtPosition(0, 0) != entity {
		t.Errorf("Expected entity still at (0, 0) after rollback")
	}

	if world.GetEntityAtPosition(5, 5) != 0 {
		t.Errorf("Expected no entity at (5, 5) after rollback")
	}
}

// TestSpatialTransaction_MultipleOperations tests multiple operations in one transaction
func TestSpatialTransaction_MultipleOperations(t *testing.T) {
	world := NewWorld()

	// Create entities
	entity1 := world.CreateEntity()
	world.AddComponent(entity1, TestComponent{X: 0, Y: 0})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(entity1, 0, 0)
		tx.Commit()
	}

	entity2 := world.CreateEntity()
	world.AddComponent(entity2, TestComponent{X: 1, Y: 1})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(entity2, 1, 1)
		tx.Commit()
	}

	entity3 := world.CreateEntity()
	world.AddComponent(entity3, TestComponent{X: 10, Y: 10})

	// Create transaction with multiple operations
	tx := world.BeginSpatialTransaction()
	tx.Move(entity1, 0, 0, 5, 5)    // Move entity1
	tx.Move(entity2, 1, 1, 6, 6)    // Move entity2
	tx.Spawn(entity3, 10, 10)        // Spawn entity3
	tx.Destroy(entity1, 5, 5)        // Remove entity1 after moving it

	// Commit all operations
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Verify results
	if world.GetEntityAtPosition(0, 0) != 0 {
		t.Errorf("Expected no entity at (0, 0)")
	}
	if world.GetEntityAtPosition(5, 5) != 0 {
		t.Errorf("Expected no entity at (5, 5) after destroy")
	}
	if world.GetEntityAtPosition(6, 6) != entity2 {
		t.Errorf("Expected entity2 at (6, 6)")
	}
	if world.GetEntityAtPosition(10, 10) != entity3 {
		t.Errorf("Expected entity3 at (10, 10)")
	}
}

// TestMoveEntitySafe tests the MoveEntitySafe convenience method
func TestMoveEntitySafe(t *testing.T) {
	world := NewWorld()

	// Create entity
	entity := world.CreateEntity()
	world.AddComponent(entity, TestComponent{X: 2, Y: 2})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(entity, 2, 2)
		tx.Commit()
	}

	// Move using MoveEntitySafe
	result := world.MoveEntitySafe(entity, 2, 2, 8, 8)

	// Verify no collision
	if result.HasCollision {
		t.Errorf("Expected no collision")
	}

	// Verify move was applied
	if world.GetEntityAtPosition(8, 8) != entity {
		t.Errorf("Expected entity at (8, 8)")
	}
	if world.GetEntityAtPosition(2, 2) != 0 {
		t.Errorf("Expected no entity at old position (2, 2)")
	}
}

// TestMoveEntitySafe_Collision tests MoveEntitySafe with collision
func TestMoveEntitySafe_Collision(t *testing.T) {
	world := NewWorld()

	// Create two entities
	entity1 := world.CreateEntity()
	world.AddComponent(entity1, TestComponent{X: 0, Y: 0})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(entity1, 0, 0)
		tx.Commit()
	}

	entity2 := world.CreateEntity()
	world.AddComponent(entity2, TestComponent{X: 5, Y: 5})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(entity2, 5, 5)
		tx.Commit()
	}

	// Try to move entity1 to entity2's position
	result := world.MoveEntitySafe(entity1, 0, 0, 5, 5)

	// Verify collision was detected
	if !result.HasCollision {
		t.Errorf("Expected collision")
	}

	if result.CollidingEntity != entity2 {
		t.Errorf("Expected collision with entity %d, got %d", entity2, result.CollidingEntity)
	}

	// Verify entity1 is still at original position
	if world.GetEntityAtPosition(0, 0) != entity1 {
		t.Errorf("Expected entity1 still at (0, 0)")
	}
}

// TestValidateSpatialIndex tests the spatial index validation
func TestValidateSpatialIndex(t *testing.T) {
	world := NewWorld()

	// Create valid entity
	entity := world.CreateEntity()
	world.AddComponent(entity, TestComponent{X: 5, Y: 5})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(entity, 5, 5)
		tx.Commit()
	}

	// Validate - should have no inconsistencies
	inconsistencies := world.ValidateSpatialIndex()
	if len(inconsistencies) != 0 {
		t.Errorf("Expected no inconsistencies, got: %v", inconsistencies)
	}
}

// TestValidateSpatialIndex_MissingEntity tests validation catches missing entities
func TestValidateSpatialIndex_MissingEntity(t *testing.T) {
	world := NewWorld()

	// Manually add entry to spatial index without creating entity
	world.mu.Lock()
	if world.spatialIndex[10] == nil {
		world.spatialIndex[10] = make(map[int]Entity)
	}
	world.spatialIndex[10][10] = Entity(999) // Non-existent entity
	world.mu.Unlock()

	// Validate - should catch the inconsistency
	inconsistencies := world.ValidateSpatialIndex()
	if len(inconsistencies) == 0 {
		t.Errorf("Expected inconsistencies for non-existent entity")
	}
}

// TestSpatialTransaction_ConcurrentTransactions tests concurrent transaction safety
func TestSpatialTransaction_ConcurrentTransactions(t *testing.T) {
	world := NewWorld()

	// Create multiple entities
	numEntities := 10
	entities := make([]Entity, numEntities)
	for i := 0; i < numEntities; i++ {
		entities[i] = world.CreateEntity()
		world.AddComponent(entities[i], TestComponent{X: i, Y: 0})
		{
			tx := world.BeginSpatialTransaction()
			tx.Spawn(entities[i], i, 0)
			tx.Commit()
		}
	}

	// Concurrently move all entities
	var wg sync.WaitGroup
	for i := 0; i < numEntities; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Small delay to increase chance of concurrent execution
			time.Sleep(time.Microsecond * time.Duration(idx*10))

			// Move entity down by 1
			result := world.MoveEntitySafe(entities[idx], idx, 0, idx, 1)
			if result.HasCollision {
				t.Errorf("Unexpected collision for entity %d", idx)
			}
		}(i)
	}

	wg.Wait()

	// Verify all entities moved successfully
	for i := 0; i < numEntities; i++ {
		found := world.GetEntityAtPosition(i, 1)
		if found != entities[i] {
			t.Errorf("Expected entity %d at position (%d, 1), got %d", entities[i], i, found)
		}

		// Verify old position is empty
		if world.GetEntityAtPosition(i, 0) != 0 {
			t.Errorf("Expected no entity at old position (%d, 0)", i)
		}
	}
}

// TestSpatialTransaction_MoveSelfToSamePosition tests moving entity to its own position
func TestSpatialTransaction_MoveSelfToSamePosition(t *testing.T) {
	world := NewWorld()

	// Create entity
	entity := world.CreateEntity()
	world.AddComponent(entity, TestComponent{X: 5, Y: 5})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(entity, 5, 5)
		tx.Commit()
	}

	// Move to same position (should not be a collision)
	tx := world.BeginSpatialTransaction()
	result := tx.Move(entity, 5, 5, 5, 5)

	// Should not report collision with itself
	if result.HasCollision {
		t.Errorf("Expected no collision when moving to same position")
	}

	// Commit
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Verify entity is still at (5, 5)
	if world.GetEntityAtPosition(5, 5) != entity {
		t.Errorf("Expected entity at (5, 5)")
	}
}

// TestSpatialTransaction_EmptyCommit tests committing empty transaction
func TestSpatialTransaction_EmptyCommit(t *testing.T) {
	world := NewWorld()

	// Create empty transaction
	tx := world.BeginSpatialTransaction()

	// Commit should succeed with no operations
	if err := tx.Commit(); err != nil {
		t.Fatalf("Empty commit failed: %v", err)
	}
}

// TestSpatialTransaction_GetCollisions tests retrieving collision information
func TestSpatialTransaction_GetCollisions(t *testing.T) {
	world := NewWorld()

	// Create entities at (1,1) and (2,2)
	entity1 := world.CreateEntity()
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(entity1, 1, 1)
		tx.Commit()
	}

	entity2 := world.CreateEntity()
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(entity2, 2, 2)
		tx.Commit()
	}

	// Create new entities to spawn at occupied positions
	entity3 := world.CreateEntity()
	entity4 := world.CreateEntity()

	// Create transaction with multiple collisions
	tx := world.BeginSpatialTransaction()
	tx.Spawn(entity3, 1, 1) // Collision with entity1
	tx.Spawn(entity4, 2, 2) // Collision with entity2

	// Verify collision count
	if !tx.HasCollisions() {
		t.Errorf("Expected collisions")
	}

	collisions := tx.GetCollisions()
	if len(collisions) != 2 {
		t.Errorf("Expected 2 collisions, got %d", len(collisions))
	}

	// Verify collision details
	expectedCollisions := map[Entity]bool{entity1: false, entity2: false}
	for _, col := range collisions {
		if !col.HasCollision {
			t.Errorf("Collision record should have HasCollision=true")
		}
		expectedCollisions[col.CollidingEntity] = true
	}

	if !expectedCollisions[entity1] || !expectedCollisions[entity2] {
		t.Errorf("Expected collisions with entities %d and %d", entity1, entity2)
	}
}

// TestSpatialTransaction_SpawnBatchSuccess tests successful batch spawn
func TestSpatialTransaction_SpawnBatchSuccess(t *testing.T) {
	world := NewWorld()

	// Create 5 entities for a sequence
	entities := make([]Entity, 5)
	batch := make([]EntityPosition, 5)

	for i := 0; i < 5; i++ {
		entities[i] = world.CreateEntity()
		batch[i] = EntityPosition{
			Entity: entities[i],
			X:      i,
			Y:      0,
		}
	}

	// Spawn batch atomically
	tx := world.BeginSpatialTransaction()
	result := tx.SpawnBatch(batch)

	// Verify no collision
	if result.HasCollision {
		t.Errorf("Expected no collision, got collision at (%d, %d)", result.X, result.Y)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Verify all entities are in spatial index
	for i := 0; i < 5; i++ {
		found := world.GetEntityAtPosition(i, 0)
		if found != entities[i] {
			t.Errorf("Expected entity %d at position (%d, 0), got %d", entities[i], i, found)
		}
	}
}

// TestSpatialTransaction_SpawnBatchCollision tests batch spawn with collision
func TestSpatialTransaction_SpawnBatchCollision(t *testing.T) {
	world := NewWorld()

	// Place an entity at position (2, 0)
	existingEntity := world.CreateEntity()
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(existingEntity, 2, 0)
		tx.Commit()
	}

	// Try to spawn a batch that includes position (2, 0)
	entities := make([]Entity, 5)
	batch := make([]EntityPosition, 5)

	for i := 0; i < 5; i++ {
		entities[i] = world.CreateEntity()
		batch[i] = EntityPosition{
			Entity: entities[i],
			X:      i,
			Y:      0,
		}
	}

	// Spawn batch - should detect collision at (2, 0)
	tx := world.BeginSpatialTransaction()
	result := tx.SpawnBatch(batch)

	// Verify collision detected
	if !result.HasCollision {
		t.Errorf("Expected collision at (2, 0)")
	}

	if result.X != 2 || result.Y != 0 {
		t.Errorf("Expected collision at (2, 0), got (%d, %d)", result.X, result.Y)
	}

	if result.CollidingEntity != existingEntity {
		t.Errorf("Expected collision with entity %d, got %d", existingEntity, result.CollidingEntity)
	}

	// Verify transaction has collisions
	if !tx.HasCollisions() {
		t.Errorf("Transaction should report collisions")
	}

	// Commit should succeed (no operations were added)
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Verify NO entities from the batch were spawned (atomic failure)
	for i := 0; i < 5; i++ {
		if i == 2 {
			// Position 2 should still have the existing entity
			if world.GetEntityAtPosition(i, 0) != existingEntity {
				t.Errorf("Expected existing entity at (2, 0)")
			}
		} else {
			// All other positions should be empty
			if world.GetEntityAtPosition(i, 0) != 0 {
				t.Errorf("Expected no entity at (%d, 0) after batch collision", i)
			}
		}
	}
}

// TestSpatialTransaction_SpawnBatchRollback tests batch spawn with rollback
func TestSpatialTransaction_SpawnBatchRollback(t *testing.T) {
	world := NewWorld()

	// Create batch of entities
	entities := make([]Entity, 3)
	batch := make([]EntityPosition, 3)

	for i := 0; i < 3; i++ {
		entities[i] = world.CreateEntity()
		batch[i] = EntityPosition{
			Entity: entities[i],
			X:      i * 2,
			Y:      5,
		}
	}

	// Spawn batch
	tx := world.BeginSpatialTransaction()
	result := tx.SpawnBatch(batch)

	if result.HasCollision {
		t.Errorf("Expected no collision")
	}

	// Rollback instead of commit
	tx.Rollback()

	// Verify NO entities were spawned
	for i := 0; i < 3; i++ {
		if world.GetEntityAtPosition(i*2, 5) != 0 {
			t.Errorf("Expected no entity at (%d, 5) after rollback", i*2)
		}
	}
}

// TestSpatialTransaction_SpawnBatchConcurrent tests concurrent batch spawns
func TestSpatialTransaction_SpawnBatchConcurrent(t *testing.T) {
	world := NewWorld()

	// Create multiple batches to spawn concurrently
	numBatches := 5
	batchSize := 10
	var wg sync.WaitGroup

	successCount := 0
	var successMutex sync.Mutex

	for b := 0; b < numBatches; b++ {
		wg.Add(1)
		go func(batchNum int) {
			defer wg.Done()

			// Small delay to increase concurrency
			time.Sleep(time.Microsecond * time.Duration(batchNum*10))

			// Create batch - all entities in same row but different columns
			entities := make([]Entity, batchSize)
			batch := make([]EntityPosition, batchSize)

			for i := 0; i < batchSize; i++ {
				entities[i] = world.CreateEntity()
				batch[i] = EntityPosition{
					Entity: entities[i],
					X:      i,
					Y:      batchNum,
				}
			}

			// Spawn batch
			tx := world.BeginSpatialTransaction()
			result := tx.SpawnBatch(batch)

			if !result.HasCollision {
				if err := tx.Commit(); err == nil {
					successMutex.Lock()
					successCount++
					successMutex.Unlock()
				}
			}
		}(b)
	}

	wg.Wait()

	// Verify all batches succeeded (different rows, no conflicts)
	if successCount != numBatches {
		t.Errorf("Expected %d successful batches, got %d", numBatches, successCount)
	}

	// Verify spatial index has all entities
	for b := 0; b < numBatches; b++ {
		for i := 0; i < batchSize; i++ {
			if world.GetEntityAtPosition(i, b) == 0 {
				t.Errorf("Expected entity at (%d, %d)", i, b)
			}
		}
	}
}

// TestSpatialTransaction_SpawnBatchEmpty tests empty batch spawn
func TestSpatialTransaction_SpawnBatchEmpty(t *testing.T) {
	world := NewWorld()

	// Create empty batch
	batch := make([]EntityPosition, 0)

	tx := world.BeginSpatialTransaction()
	result := tx.SpawnBatch(batch)

	// Should succeed with no collision
	if result.HasCollision {
		t.Errorf("Expected no collision for empty batch")
	}

	// Commit should succeed
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
}
