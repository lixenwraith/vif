package engine

import (
	"reflect"
	"testing"
	"time"
)

// TestComponent is a test component for testing
type TestComponent struct {
	X, Y int
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

// TestDestroyEntityWithPosition tests DestroyEntity with PositionComponent
func TestDestroyEntityWithPosition(t *testing.T) {
	world := NewWorld()

	// Create entity with PositionComponent
	entity := world.CreateEntity()
	world.AddComponent(entity, PositionComponent{X: 10, Y: 20})

	tx := world.BeginSpatialTransaction()
	tx.Spawn(entity, 10, 20)
	tx.Commit()

	// Verify entity is in spatial index
	foundEntity := world.GetEntityAtPosition(10, 20)
	if foundEntity != entity {
		t.Errorf("Expected entity %d at position (10, 20), got %d", entity, foundEntity)
	}

	// DestroyEntity should handle spatial index removal
	world.DestroyEntity(entity)

	// Verify entity is removed from spatial index
	foundEntity = world.GetEntityAtPosition(10, 20)
	if foundEntity != 0 {
		t.Errorf("Expected no entity at position (10, 20) after DestroyEntity, got %d", foundEntity)
	}

	// Verify entity is destroyed
	if world.EntityCount() != 0 {
		t.Errorf("Expected 0 entities after DestroyEntity, got %d", world.EntityCount())
	}
}

// TestDestroyEntityWithoutPosition tests DestroyEntity without PositionComponent
func TestDestroyEntityWithoutPosition(t *testing.T) {
	world := NewWorld()

	// Create entity without PositionComponent (just TestComponent)
	entity := world.CreateEntity()
	world.AddComponent(entity, TestComponent{X: 5, Y: 5})

	// Verify entity exists
	if world.EntityCount() != 1 {
		t.Errorf("Expected 1 entity, got %d", world.EntityCount())
	}

	// DestroyEntity should still work without PositionComponent
	world.DestroyEntity(entity)

	// Verify entity is destroyed
	if world.EntityCount() != 0 {
		t.Errorf("Expected 0 entities after DestroyEntity, got %d", world.EntityCount())
	}
}

// TestDestroyEntityMultipleEntities tests DestroyEntity with multiple entities
func TestDestroyEntityMultipleEntities(t *testing.T) {
	world := NewWorld()

	// Create multiple entities at different positions
	entities := make([]Entity, 5)
	positions := [][2]int{{0, 0}, {1, 1}, {2, 2}, {3, 3}, {4, 4}}

	for i := 0; i < 5; i++ {
		entities[i] = world.CreateEntity()
		world.AddComponent(entities[i], PositionComponent{X: positions[i][0], Y: positions[i][1]})

		tx := world.BeginSpatialTransaction()
		tx.Spawn(entities[i], positions[i][0], positions[i][1])
		tx.Commit()
	}

	// Verify all entities are in spatial index
	for i := 0; i < 5; i++ {
		foundEntity := world.GetEntityAtPosition(positions[i][0], positions[i][1])
		if foundEntity != entities[i] {
			t.Errorf("Expected entity %d at position (%d, %d), got %d",
				entities[i], positions[i][0], positions[i][1], foundEntity)
		}
	}

	// DestroyEntity on middle entity
	world.DestroyEntity(entities[2])

	// Verify middle entity is removed from spatial index
	foundEntity := world.GetEntityAtPosition(positions[2][0], positions[2][1])
	if foundEntity != 0 {
		t.Errorf("Expected no entity at position (%d, %d) after DestroyEntity, got %d",
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

// TestDestroyEntityNonExistent tests DestroyEntity on non-existent entity
func TestDestroyEntityNonExistent(t *testing.T) {
	world := NewWorld()

	// Create and immediately destroy an entity to get a valid ID
	entity := world.CreateEntity()
	world.DestroyEntity(entity)

	// Try to DestroyEntity on already-destroyed entity (should be no-op)
	world.DestroyEntity(entity)

	// Verify no panic and entity count is still 0
	if world.EntityCount() != 0 {
		t.Errorf("Expected 0 entities, got %d", world.EntityCount())
	}
}

// TestDestroyEntityComponentTypeIndex tests that component type index is cleaned up
func TestDestroyEntityComponentTypeIndex(t *testing.T) {
	world := NewWorld()

	// Create entities with PositionComponent
	entity1 := world.CreateEntity()
	world.AddComponent(entity1, PositionComponent{X: 1, Y: 1})

	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(entity1, 1, 1)
		tx.Commit()
	}

	entity2 := world.CreateEntity()
	world.AddComponent(entity2, PositionComponent{X: 2, Y: 2})

	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(entity2, 2, 2)
		tx.Commit()
	}

	// Get entities with PositionComponent
	compType := reflect.TypeOf(PositionComponent{})
	entities := world.GetEntitiesWith(compType)

	if len(entities) != 2 {
		t.Errorf("Expected 2 entities with PositionComponent, got %d", len(entities))
	}

	// DestroyEntity on first entity
	world.DestroyEntity(entity1)

	// Verify type index is updated
	entities = world.GetEntitiesWith(compType)
	if len(entities) != 1 {
		t.Errorf("Expected 1 entity with PositionComponent after DestroyEntity, got %d", len(entities))
	}

	if entities[0] != entity2 {
		t.Errorf("Expected entity2 (%d) in type index, got %d", entity2, entities[0])
	}

	// Verify entity1 is removed from spatial index
	foundEntity := world.GetEntityAtPosition(1, 1)
	if foundEntity != 0 {
		t.Errorf("Expected no entity at position (1, 1) after DestroyEntity, got %d", foundEntity)
	}

	// Verify entity2 is still in spatial index
	foundEntity = world.GetEntityAtPosition(2, 2)
	if foundEntity != entity2 {
		t.Errorf("Expected entity2 (%d) at position (2, 2), got %d", entity2, foundEntity)
	}
}

// TestSystemExecutionOrder verifies that systems execute in priority order
func TestSystemExecutionOrder(t *testing.T) {
	world := NewWorld()

	// Track execution order
	executionOrder := make([]int, 0)

	// Create test systems with different priorities
	system10 := &testSystem{priority: 10, executionOrder: &executionOrder}
	system15 := &testSystem{priority: 15, executionOrder: &executionOrder}
	system20 := &testSystem{priority: 20, executionOrder: &executionOrder}
	system25 := &testSystem{priority: 25, executionOrder: &executionOrder}
	system30 := &testSystem{priority: 30, executionOrder: &executionOrder}

	// Add systems in random order to verify sorting works
	world.AddSystem(system25)
	world.AddSystem(system10)
	world.AddSystem(system30)
	world.AddSystem(system15)
	world.AddSystem(system20)

	// Run update
	world.Update(0)

	// Verify execution order matches priority order (lower priority = runs first)
	expectedOrder := []int{10, 15, 20, 25, 30}
	if len(executionOrder) != len(expectedOrder) {
		t.Fatalf("Expected %d systems to execute, got %d", len(expectedOrder), len(executionOrder))
	}

	for i, expectedPriority := range expectedOrder {
		if executionOrder[i] != expectedPriority {
			t.Errorf("System execution order mismatch at position %d: expected priority %d, got %d",
				i, expectedPriority, executionOrder[i])
		}
	}
}

// testSystem is a mock system for testing execution order
type testSystem struct {
	priority       int
	executionOrder *[]int
}

func (s *testSystem) Update(world *World, dt time.Duration) {
	*s.executionOrder = append(*s.executionOrder, s.priority)
}

func (s *testSystem) Priority() int {
	return s.priority
}