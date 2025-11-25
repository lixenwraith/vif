package engine

import (
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
)

// TestComponent is a test component for testing
type TestComponent struct {
	X, Y int
}

// TestComponentTypeIndex tests that component type index is properly maintained
func TestComponentTypeIndex(t *testing.T) {
	world := NewWorld()
	testStore := NewStore[TestComponent]()

	// Create entities with test components
	entity1 := world.CreateEntity()
	testStore.Add(entity1, TestComponent{X: 1, Y: 1})

	entity2 := world.CreateEntity()
	testStore.Add(entity2, TestComponent{X: 2, Y: 2})

	// Get entities with TestComponent
	entities := testStore.All()

	if len(entities) != 2 {
		t.Errorf("Expected 2 entities with TestComponent, got %d", len(entities))
	}

	// Destroy one entity
	world.DestroyEntity(entity1)
	testStore.Remove(entity1) // Manual cleanup since testStore not registered in world

	// Verify type index is updated
	entities = testStore.All()
	if len(entities) != 1 {
		t.Errorf("Expected 1 entity with TestComponent after destruction, got %d", len(entities))
	}

	if entities[0] != entity2 {
		t.Errorf("Expected entity2 (%d) in type index, got %d", entity2, entities[0])
	}
}

// TestDestroyEntityWithPosition tests DestroyEntity with PositionComponent
func TestDestroyEntityWithPosition(t *testing.T) {
	world := NewWorld()

	// Create entity with PositionComponent using spatial transaction
	entity := world.CreateEntity()
	tx := world.BeginSpatialTransaction()
	tx.Spawn(entity, 10, 20)
	tx.Commit()

	// Verify entity is in spatial index
	foundEntity := world.GetEntityAtPosition(10, 20)
	if foundEntity != entity {
		t.Errorf("Expected entity %d at position (10, 20), got %d", entity, foundEntity)
	}

	// Verify entity has position component
	if pos, ok := world.Positions.Get(entity); !ok {
		t.Error("Expected entity to have position component")
	} else if pos.X != 10 || pos.Y != 20 {
		t.Errorf("Expected position (10, 20), got (%d, %d)", pos.X, pos.Y)
	}

	// DestroyEntity should handle spatial index removal
	world.DestroyEntity(entity)

	// Verify entity is removed from spatial index
	foundEntity = world.GetEntityAtPosition(10, 20)
	if foundEntity != 0 {
		t.Errorf("Expected no entity at position (10, 20) after DestroyEntity, got %d", foundEntity)
	}

	// Verify entity has no components
	if world.HasAnyComponent(entity) {
		t.Error("Expected entity to have no components after DestroyEntity")
	}
}

// TestDestroyEntityWithoutPosition tests DestroyEntity without PositionComponent
func TestDestroyEntityWithoutPosition(t *testing.T) {
	world := NewWorld()
	testStore := NewStore[TestComponent]()

	// Create entity without PositionComponent (just TestComponent)
	entity := world.CreateEntity()
	testStore.Add(entity, TestComponent{X: 5, Y: 5})

	// Verify entity exists in test store
	if !testStore.Has(entity) {
		t.Error("Expected entity to have TestComponent")
	}

	// DestroyEntity should still work without PositionComponent
	world.DestroyEntity(entity)
	testStore.Remove(entity) // Manual cleanup since testStore not registered in world

	// Verify entity is destroyed in test store
	if testStore.Has(entity) {
		t.Error("Expected entity to not have TestComponent after DestroyEntity")
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

	// Verify middle entity has no components
	if world.HasAnyComponent(entities[2]) {
		t.Error("Expected destroyed entity to have no components")
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

	// Verify no panic and entity has no components
	if world.HasAnyComponent(entity) {
		t.Error("Expected destroyed entity to have no components")
	}
}

// TestDestroyEntityComponentTypeIndex tests that component type index is cleaned up
func TestDestroyEntityComponentTypeIndex(t *testing.T) {
	world := NewWorld()

	// Create entities with PositionComponent
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

	// Get entities with PositionComponent
	entities := world.Positions.All()

	if len(entities) != 2 {
		t.Errorf("Expected 2 entities with PositionComponent, got %d", len(entities))
	}

	// DestroyEntity on first entity
	world.DestroyEntity(entity1)

	// Verify type index is updated
	entities = world.Positions.All()
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

// TestQuery tests the new query builder pattern
func TestQuery(t *testing.T) {
	world := NewWorld()

	// Create entities with different component combinations
	e1 := WithPosition(world.NewEntity(), world.Positions, components.PositionComponent{X: 1, Y: 1}).Build()

	_ = With(world.NewEntity(), world.Characters, components.CharacterComponent{Rune: 'A'}).Build()

	e3 := With(
		WithPosition(world.NewEntity(), world.Positions, components.PositionComponent{X: 2, Y: 2}),
		world.Characters,
		components.CharacterComponent{Rune: 'B'},
	).Build()

	// Query for entities with both Position and Character
	entities := world.Query().
		With(world.Positions).
		With(world.Characters).
		Execute()

	if len(entities) != 1 {
		t.Errorf("Expected 1 entity with both components, got %d", len(entities))
	}
	if len(entities) > 0 && entities[0] != e3 {
		t.Errorf("Expected entity %d, got %d", e3, entities[0])
	}

	// Query for entities with only Position
	entities = world.Query().
		With(world.Positions).
		Execute()

	if len(entities) != 2 {
		t.Errorf("Expected 2 entities with Position, got %d", len(entities))
	}

	// Verify e1 and e3 are in results (order may vary)
	found := make(map[Entity]bool)
	for _, e := range entities {
		found[e] = true
	}
	if !found[e1] || !found[e3] {
		t.Errorf("Expected to find entities %d and %d", e1, e3)
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
