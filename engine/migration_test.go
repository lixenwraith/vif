package engine

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/components"
)

// TestParallelWorldCreation verifies that old and new ECS worlds can coexist
// during the migration phase. Entities created in one world should not affect the other.
func TestParallelWorldCreation(t *testing.T) {
	world := NewWorld()

	// Old API - create entity using reflection-based approach
	e1 := world.CreateEntity()
	world.AddComponent(e1, components.PositionComponent{X: 5, Y: 10})

	// New API via bridge - create entity using generic approach
	e2 := world.CreateEntityGeneric()
	world.generic.Positions.Add(e2, components.PositionComponent{X: 15, Y: 20})

	// Verify isolation - old world position lookup
	entity := world.GetEntityAtPosition(5, 10)
	if entity != e1 {
		t.Errorf("Old world position lookup failed: expected entity %d, got %d", e1, entity)
	}

	// Verify isolation - new world position lookup
	entity = world.generic.Positions.GetEntityAt(15, 20)
	if entity != e2 {
		t.Errorf("New world position lookup failed: expected entity %d, got %d", e2, entity)
	}

	// Verify that old world doesn't see new world's entity
	entity = world.GetEntityAtPosition(15, 20)
	if entity != 0 {
		t.Errorf("Old world should not see new world's entity at (15,20), got entity %d", entity)
	}

	// Verify that new world doesn't see old world's entity
	entity = world.generic.Positions.GetEntityAt(5, 10)
	if entity != 0 {
		t.Errorf("New world should not see old world's entity at (5,10), got entity %d", entity)
	}
}

// TestMigrateFrom verifies that data can be migrated from old to new world
func TestMigrateFrom(t *testing.T) {
	oldWorld := NewWorld()

	// Create entities in old world
	e1 := oldWorld.CreateEntity()
	oldWorld.AddComponent(e1, components.PositionComponent{X: 10, Y: 20})
	oldWorld.AddComponent(e1, components.CharacterComponent{Character: 'A'})

	e2 := oldWorld.CreateEntity()
	oldWorld.AddComponent(e2, components.PositionComponent{X: 30, Y: 40})

	// Create new generic world and migrate
	newWorld := NewWorldGeneric()
	newWorld.MigrateFrom(oldWorld)

	// Verify positions were migrated
	pos, ok := newWorld.Positions.Get(e1)
	if !ok {
		t.Error("Entity e1 position was not migrated")
	}
	if pos.X != 10 || pos.Y != 20 {
		t.Errorf("Entity e1 position incorrect: expected (10,20), got (%d,%d)", pos.X, pos.Y)
	}

	pos, ok = newWorld.Positions.Get(e2)
	if !ok {
		t.Error("Entity e2 position was not migrated")
	}
	if pos.X != 30 || pos.Y != 40 {
		t.Errorf("Entity e2 position incorrect: expected (30,40), got (%d,%d)", pos.X, pos.Y)
	}

	// Verify character component was migrated
	char, ok := newWorld.Characters.Get(e1)
	if !ok {
		t.Error("Entity e1 character was not migrated")
	}
	if char.Character != 'A' {
		t.Errorf("Entity e1 character incorrect: expected 'A', got '%c'", char.Character)
	}

	// Verify spatial index works after migration
	entity := newWorld.Positions.GetEntityAt(10, 20)
	if entity != e1 {
		t.Errorf("Spatial index lookup failed: expected entity %d at (10,20), got %d", e1, entity)
	}

	entity = newWorld.Positions.GetEntityAt(30, 40)
	if entity != e2 {
		t.Errorf("Spatial index lookup failed: expected entity %d at (30,40), got %d", e2, entity)
	}
}

// TestGenericWorldDestroyEntity verifies that DestroyEntity properly removes
// entities from all stores including the spatial index
func TestGenericWorldDestroyEntity(t *testing.T) {
	world := NewWorldGeneric()

	// Create entity with position and character
	e := world.CreateEntity()
	world.Positions.Add(e, components.PositionComponent{X: 5, Y: 10})
	world.Characters.Add(e, components.CharacterComponent{Character: 'Z'})

	// Verify entity exists
	if !world.HasAnyComponent(e) {
		t.Error("Entity should exist before destroy")
	}

	// Verify spatial index has the entity
	entity := world.Positions.GetEntityAt(5, 10)
	if entity != e {
		t.Errorf("Spatial index should have entity %d at (5,10), got %d", e, entity)
	}

	// Destroy entity
	world.DestroyEntity(e)

	// Verify entity is completely removed
	if world.HasAnyComponent(e) {
		t.Error("Entity should not exist after destroy")
	}

	// Verify spatial index is cleared
	entity = world.Positions.GetEntityAt(5, 10)
	if entity != 0 {
		t.Errorf("Spatial index should not have entity at (5,10) after destroy, got %d", entity)
	}

	// Verify stores are cleared
	_, ok := world.Positions.Get(e)
	if ok {
		t.Error("Position store should not have entity after destroy")
	}

	_, ok = world.Characters.Get(e)
	if ok {
		t.Error("Character store should not have entity after destroy")
	}
}
