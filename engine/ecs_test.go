package engine

import (
	"reflect"
	"testing"
)

// TestComponent is a test component for testing
type TestComponent struct {
	X, Y int
}

// TestSpatialIndexCleanup tests that entities are properly removed from spatial index
func TestSpatialIndexCleanup(t *testing.T) {
	world := NewWorld()

	// Create an entity at position (5, 10)
	entity := world.CreateEntity()
	world.AddComponent(entity, TestComponent{X: 5, Y: 10})
	world.UpdateSpatialIndex(entity, 5, 10)

	// Verify entity is in spatial index
	foundEntity := world.GetEntityAtPosition(5, 10)
	if foundEntity != entity {
		t.Errorf("Expected entity %d at position (5, 10), got %d", entity, foundEntity)
	}

	// Remove from spatial index
	world.RemoveFromSpatialIndex(5, 10)

	// Verify entity is no longer in spatial index
	foundEntity = world.GetEntityAtPosition(5, 10)
	if foundEntity != 0 {
		t.Errorf("Expected no entity at position (5, 10) after removal, got %d", foundEntity)
	}

	// Destroy entity
	world.DestroyEntity(entity)

	// Verify entity is destroyed
	if world.EntityCount() != 0 {
		t.Errorf("Expected 0 entities after destruction, got %d", world.EntityCount())
	}
}

// TestSpatialIndexCleanupOnDestroy tests that DestroyEntity cleans up spatial index
func TestSpatialIndexCleanupOnDestroy(t *testing.T) {
	world := NewWorld()

	// Create multiple entities at different positions
	entities := make([]Entity, 5)
	positions := [][2]int{{0, 0}, {1, 1}, {2, 2}, {3, 3}, {4, 4}}

	for i := 0; i < 5; i++ {
		entities[i] = world.CreateEntity()
		world.AddComponent(entities[i], TestComponent{X: positions[i][0], Y: positions[i][1]})
		world.UpdateSpatialIndex(entities[i], positions[i][0], positions[i][1])
	}

	// Verify all entities are in spatial index
	for i := 0; i < 5; i++ {
		foundEntity := world.GetEntityAtPosition(positions[i][0], positions[i][1])
		if foundEntity != entities[i] {
			t.Errorf("Expected entity %d at position (%d, %d), got %d",
				entities[i], positions[i][0], positions[i][1], foundEntity)
		}
	}

	// Destroy middle entity
	world.DestroyEntity(entities[2])

	// Verify middle entity is no longer in spatial index
	foundEntity := world.GetEntityAtPosition(positions[2][0], positions[2][1])
	if foundEntity != 0 {
		t.Errorf("Expected no entity at position (%d, %d) after destruction, got %d",
			positions[2][0], positions[2][1], foundEntity)
	}

	// Verify other entities are still in spatial index
	for i := 0; i < 5; i++ {
		if i == 2 {
			continue
		}
		foundEntity := world.GetEntityAtPosition(positions[i][0], positions[i][1])
		if foundEntity != entities[i] {
			t.Errorf("Expected entity %d at position (%d, %d), got %d",
				entities[i], positions[i][0], positions[i][1], foundEntity)
		}
	}
}

// TestRemoveFromSpatialIndexBeforeDestroy verifies correct order of operations
func TestRemoveFromSpatialIndexBeforeDestroy(t *testing.T) {
	world := NewWorld()

	// Create entity at position (10, 20)
	entity := world.CreateEntity()
	world.AddComponent(entity, TestComponent{X: 10, Y: 20})
	world.UpdateSpatialIndex(entity, 10, 20)

	// Manual cleanup: remove from spatial index first
	world.RemoveFromSpatialIndex(10, 20)

	// Verify removed
	foundEntity := world.GetEntityAtPosition(10, 20)
	if foundEntity != 0 {
		t.Errorf("Expected no entity at (10, 20) after spatial index removal, got %d", foundEntity)
	}

	// Then destroy
	world.DestroyEntity(entity)

	// Verify entity count is zero
	if world.EntityCount() != 0 {
		t.Errorf("Expected 0 entities, got %d", world.EntityCount())
	}
}

// TestComponentTypeIndex tests that component type index is properly maintained
func TestComponentTypeIndex(t *testing.T) {
	world := NewWorld()

	// Create entities with test components
	entity1 := world.CreateEntity()
	world.AddComponent(entity1, TestComponent{X: 1, Y: 1})

	entity2 := world.CreateEntity()
	world.AddComponent(entity2, TestComponent{X: 2, Y: 2})

	// Get entities with TestComponent
	compType := reflect.TypeOf(TestComponent{})
	entities := world.GetEntitiesWith(compType)

	if len(entities) != 2 {
		t.Errorf("Expected 2 entities with TestComponent, got %d", len(entities))
	}

	// Destroy one entity
	world.DestroyEntity(entity1)

	// Verify type index is updated
	entities = world.GetEntitiesWith(compType)
	if len(entities) != 1 {
		t.Errorf("Expected 1 entity with TestComponent after destruction, got %d", len(entities))
	}

	if entities[0] != entity2 {
		t.Errorf("Expected entity2 (%d) in type index, got %d", entity2, entities[0])
	}
}

// PositionComponent for testing (matches game component structure)
type PositionComponent struct {
	X, Y int
}

// TestSafeDestroyEntityWithPosition tests SafeDestroyEntity with PositionComponent
func TestSafeDestroyEntityWithPosition(t *testing.T) {
	world := NewWorld()

	// Create entity with PositionComponent
	entity := world.CreateEntity()
	world.AddComponent(entity, PositionComponent{X: 10, Y: 20})
	world.UpdateSpatialIndex(entity, 10, 20)

	// Verify entity is in spatial index
	foundEntity := world.GetEntityAtPosition(10, 20)
	if foundEntity != entity {
		t.Errorf("Expected entity %d at position (10, 20), got %d", entity, foundEntity)
	}

	// SafeDestroyEntity should handle spatial index removal
	world.SafeDestroyEntity(entity)

	// Verify entity is removed from spatial index
	foundEntity = world.GetEntityAtPosition(10, 20)
	if foundEntity != 0 {
		t.Errorf("Expected no entity at position (10, 20) after SafeDestroyEntity, got %d", foundEntity)
	}

	// Verify entity is destroyed
	if world.EntityCount() != 0 {
		t.Errorf("Expected 0 entities after SafeDestroyEntity, got %d", world.EntityCount())
	}
}

// TestSafeDestroyEntityWithoutPosition tests SafeDestroyEntity without PositionComponent
func TestSafeDestroyEntityWithoutPosition(t *testing.T) {
	world := NewWorld()

	// Create entity without PositionComponent (just TestComponent)
	entity := world.CreateEntity()
	world.AddComponent(entity, TestComponent{X: 5, Y: 5})

	// Verify entity exists
	if world.EntityCount() != 1 {
		t.Errorf("Expected 1 entity, got %d", world.EntityCount())
	}

	// SafeDestroyEntity should still work without PositionComponent
	world.SafeDestroyEntity(entity)

	// Verify entity is destroyed
	if world.EntityCount() != 0 {
		t.Errorf("Expected 0 entities after SafeDestroyEntity, got %d", world.EntityCount())
	}
}

// TestSafeDestroyEntityMultipleEntities tests SafeDestroyEntity with multiple entities
func TestSafeDestroyEntityMultipleEntities(t *testing.T) {
	world := NewWorld()

	// Create multiple entities at different positions
	entities := make([]Entity, 5)
	positions := [][2]int{{0, 0}, {1, 1}, {2, 2}, {3, 3}, {4, 4}}

	for i := 0; i < 5; i++ {
		entities[i] = world.CreateEntity()
		world.AddComponent(entities[i], PositionComponent{X: positions[i][0], Y: positions[i][1]})
		world.UpdateSpatialIndex(entities[i], positions[i][0], positions[i][1])
	}

	// Verify all entities are in spatial index
	for i := 0; i < 5; i++ {
		foundEntity := world.GetEntityAtPosition(positions[i][0], positions[i][1])
		if foundEntity != entities[i] {
			t.Errorf("Expected entity %d at position (%d, %d), got %d",
				entities[i], positions[i][0], positions[i][1], foundEntity)
		}
	}

	// SafeDestroyEntity on middle entity
	world.SafeDestroyEntity(entities[2])

	// Verify middle entity is removed from spatial index
	foundEntity := world.GetEntityAtPosition(positions[2][0], positions[2][1])
	if foundEntity != 0 {
		t.Errorf("Expected no entity at position (%d, %d) after SafeDestroyEntity, got %d",
			positions[2][0], positions[2][1], foundEntity)
	}

	// Verify middle entity is destroyed
	if world.EntityCount() != 4 {
		t.Errorf("Expected 4 entities after destroying one, got %d", world.EntityCount())
	}

	// Verify other entities are still in spatial index
	for i := 0; i < 5; i++ {
		if i == 2 {
			continue
		}
		foundEntity := world.GetEntityAtPosition(positions[i][0], positions[i][1])
		if foundEntity != entities[i] {
			t.Errorf("Expected entity %d at position (%d, %d), got %d",
				entities[i], positions[i][0], positions[i][1], foundEntity)
		}
	}
}

// TestSafeDestroyEntityNonExistent tests SafeDestroyEntity on non-existent entity
func TestSafeDestroyEntityNonExistent(t *testing.T) {
	world := NewWorld()

	// Create and immediately destroy an entity to get a valid ID
	entity := world.CreateEntity()
	world.DestroyEntity(entity)

	// Try to SafeDestroyEntity on already-destroyed entity (should be no-op)
	world.SafeDestroyEntity(entity)

	// Verify no panic and entity count is still 0
	if world.EntityCount() != 0 {
		t.Errorf("Expected 0 entities, got %d", world.EntityCount())
	}
}

// TestSafeDestroyEntityComponentTypeIndex tests that component type index is cleaned up
func TestSafeDestroyEntityComponentTypeIndex(t *testing.T) {
	world := NewWorld()

	// Create entities with PositionComponent
	entity1 := world.CreateEntity()
	world.AddComponent(entity1, PositionComponent{X: 1, Y: 1})
	world.UpdateSpatialIndex(entity1, 1, 1)

	entity2 := world.CreateEntity()
	world.AddComponent(entity2, PositionComponent{X: 2, Y: 2})
	world.UpdateSpatialIndex(entity2, 2, 2)

	// Get entities with PositionComponent
	compType := reflect.TypeOf(PositionComponent{})
	entities := world.GetEntitiesWith(compType)

	if len(entities) != 2 {
		t.Errorf("Expected 2 entities with PositionComponent, got %d", len(entities))
	}

	// SafeDestroyEntity on first entity
	world.SafeDestroyEntity(entity1)

	// Verify type index is updated
	entities = world.GetEntitiesWith(compType)
	if len(entities) != 1 {
		t.Errorf("Expected 1 entity with PositionComponent after SafeDestroyEntity, got %d", len(entities))
	}

	if entities[0] != entity2 {
		t.Errorf("Expected entity2 (%d) in type index, got %d", entity2, entities[0])
	}

	// Verify entity1 is removed from spatial index
	foundEntity := world.GetEntityAtPosition(1, 1)
	if foundEntity != 0 {
		t.Errorf("Expected no entity at position (1, 1) after SafeDestroyEntity, got %d", foundEntity)
	}

	// Verify entity2 is still in spatial index
	foundEntity = world.GetEntityAtPosition(2, 2)
	if foundEntity != entity2 {
		t.Errorf("Expected entity2 (%d) at position (2, 2), got %d", entity2, foundEntity)
	}
}
