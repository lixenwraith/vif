package systems

import (
	"reflect"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestTrailSystemCleanup tests that trail entities properly clean up spatial index
func TestTrailSystemCleanup(t *testing.T) {
	world := engine.NewWorld()
	trailSystem := NewTrailSystem()

	// Create a trail entity that will expire
	entity := world.CreateEntity()
	pos := components.PositionComponent{X: 5, Y: 10}
	trail := components.TrailComponent{
		Intensity: 1.0,
		Timestamp: time.Now().Add(-1 * time.Second), // Already expired
	}

	world.AddComponent(entity, pos)
	world.AddComponent(entity, trail)
	world.UpdateSpatialIndex(pos.X, pos.Y)

	// Verify entity is in spatial index
	foundEntity := world.GetEntityAtPosition(pos.X, pos.Y)
	if foundEntity == 0 {
		// Trail entities might not be in spatial index, which is fine
		t.Log("Trail entity not in spatial index (expected behavior)")
	}

	// Run trail system update - should destroy expired trail
	trailSystem.Update(world, 16*time.Millisecond)

	// Verify entity is destroyed
	posType := reflect.TypeOf(components.PositionComponent{})
	_, exists := world.GetComponent(entity, posType)
	if exists {
		t.Error("Expected trail entity to be destroyed after expiration")
	}

	// Verify spatial index is clean
	foundEntity = world.GetEntityAtPosition(pos.X, pos.Y)
	if foundEntity != 0 {
		t.Errorf("Expected no entity at position (%d, %d) after trail cleanup, got %d",
			pos.X, pos.Y, foundEntity)
	}
}

// TestTrailSystemLowIntensity tests that low intensity trails are cleaned up
func TestTrailSystemLowIntensity(t *testing.T) {
	world := engine.NewWorld()
	trailSystem := NewTrailSystem()

	// Create a trail entity with low intensity
	entity := world.CreateEntity()
	pos := components.PositionComponent{X: 3, Y: 7}
	trail := components.TrailComponent{
		Intensity: 0.01, // Very low intensity
		Timestamp: time.Now().Add(-400 * time.Millisecond),
	}

	world.AddComponent(entity, pos)
	world.AddComponent(entity, trail)

	// Run trail system update - should destroy low intensity trail
	trailSystem.Update(world, 16*time.Millisecond)

	// Verify entity is destroyed
	posType := reflect.TypeOf(components.PositionComponent{})
	_, exists := world.GetComponent(entity, posType)
	if exists {
		t.Error("Expected trail entity to be destroyed due to low intensity")
	}

	// Verify spatial index is clean
	foundEntity := world.GetEntityAtPosition(pos.X, pos.Y)
	if foundEntity != 0 {
		t.Errorf("Expected no entity at position (%d, %d) after low intensity cleanup, got %d",
			pos.X, pos.Y, foundEntity)
	}
}

// TestTrailSystemFutureTimestamp tests that future trail points are not destroyed
func TestTrailSystemFutureTimestamp(t *testing.T) {
	world := engine.NewWorld()
	trailSystem := NewTrailSystem()

	// Create a trail entity with future timestamp
	entity := world.CreateEntity()
	pos := components.PositionComponent{X: 2, Y: 4}
	trail := components.TrailComponent{
		Intensity: 1.0,
		Timestamp: time.Now().Add(1 * time.Second), // Future timestamp
	}

	world.AddComponent(entity, pos)
	world.AddComponent(entity, trail)

	// Run trail system update - should NOT destroy future trail
	trailSystem.Update(world, 16*time.Millisecond)

	// Verify entity still exists
	posType := reflect.TypeOf(components.PositionComponent{})
	_, exists := world.GetComponent(entity, posType)
	if !exists {
		t.Error("Expected trail entity with future timestamp to remain")
	}
}

// TestAddTrailFunction tests the AddTrail function
func TestAddTrailFunction(t *testing.T) {
	world := engine.NewWorld()

	// Add trail from (0, 0) to (8, 0)
	AddTrail(world, 0, 0, 8, 0)

	// Verify trail entities were created
	trailType := reflect.TypeOf(components.TrailComponent{})
	posType := reflect.TypeOf(components.PositionComponent{})
	entities := world.GetEntitiesWith(trailType, posType)

	if len(entities) != trailLength {
		t.Errorf("Expected %d trail entities, got %d", trailLength, len(entities))
	}

	// Verify each entity has proper components
	for _, entity := range entities {
		_, hasPos := world.GetComponent(entity, posType)
		if !hasPos {
			t.Error("Trail entity missing PositionComponent")
		}

		trailComp, hasTrail := world.GetComponent(entity, trailType)
		if !hasTrail {
			t.Error("Trail entity missing TrailComponent")
		}

		trail := trailComp.(components.TrailComponent)
		if trail.Intensity <= 0 || trail.Intensity > 1.0 {
			t.Errorf("Trail intensity out of range: %f", trail.Intensity)
		}
	}
}

// TestTrailSystemPriority verifies the trail system has correct priority
func TestTrailSystemPriority(t *testing.T) {
	trailSystem := NewTrailSystem()
	expectedPriority := 20

	if trailSystem.Priority() != expectedPriority {
		t.Errorf("Expected trail system priority %d, got %d", expectedPriority, trailSystem.Priority())
	}
}

// TestMultipleTrailCleanup tests cleanup of multiple trail entities
func TestMultipleTrailCleanup(t *testing.T) {
	world := engine.NewWorld()
	trailSystem := NewTrailSystem()

	// Create multiple expired trail entities
	positions := []components.PositionComponent{
		{X: 1, Y: 1},
		{X: 2, Y: 2},
		{X: 3, Y: 3},
		{X: 4, Y: 4},
		{X: 5, Y: 5},
	}

	entities := make([]engine.Entity, len(positions))
	for i, pos := range positions {
		entities[i] = world.CreateEntity()
		trail := components.TrailComponent{
			Intensity: 1.0,
			Timestamp: time.Now().Add(-1 * time.Second), // Expired
		}
		world.AddComponent(entities[i], pos)
		world.AddComponent(entities[i], trail)
	}

	initialCount := world.EntityCount()
	if initialCount != len(positions) {
		t.Errorf("Expected %d entities, got %d", len(positions), initialCount)
	}

	// Run trail system - should clean up all expired trails
	trailSystem.Update(world, 16*time.Millisecond)

	// Verify all entities are destroyed
	finalCount := world.EntityCount()
	if finalCount != 0 {
		t.Errorf("Expected 0 entities after cleanup, got %d", finalCount)
	}

	// Verify spatial index is clean for all positions
	for _, pos := range positions {
		foundEntity := world.GetEntityAtPosition(pos.X, pos.Y)
		if foundEntity != 0 {
			t.Errorf("Expected no entity at position (%d, %d), got %d", pos.X, pos.Y, foundEntity)
		}
	}
}
