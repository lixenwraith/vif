package systems

import (
	"reflect"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestCleanerLifecycle verifies cleaners spawn, move, and destroy correctly
func TestCleanerLifecycle(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	// Create Red character
	createRedCharacterAt(world, 40, 5)

	// Initial state: animation complete (no cleaners)
	if !cleanerSystem.IsAnimationComplete() {
		t.Error("Expected animation to be complete initially")
	}

	// Activate cleaners
	cleanerSystem.ActivateCleaners(world)

	// Process spawn
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Verify cleaner was spawned
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	if len(cleaners) != 1 {
		t.Fatalf("Expected 1 cleaner after activation, got %d", len(cleaners))
	}

	// Verify animation is now active
	if cleanerSystem.IsAnimationComplete() {
		t.Error("Expected animation to be active after spawning")
	}

	// Run animation until complete
	maxIterations := 1000
	for i := 0; i < maxIterations && !cleanerSystem.IsAnimationComplete(); i++ {
		cleanerSystem.Update(world, 16*time.Millisecond)
		time.Sleep(1 * time.Millisecond)
	}

	// Verify animation completed
	if !cleanerSystem.IsAnimationComplete() {
		t.Fatal("Animation did not complete in reasonable time")
	}

	// Verify cleaners were destroyed
	cleaners = world.GetEntitiesWith(cleanerType)
	if len(cleaners) != 0 {
		t.Errorf("Expected no cleaners after completion, got %d", len(cleaners))
	}
}

// TestCleanerEntityCleanup verifies cleaner entities are destroyed when passing target
func TestCleanerEntityCleanup(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	// Create Red character
	createRedCharacterAt(world, 40, 5)

	// Activate and spawn
	cleanerSystem.ActivateCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Track entity count over time
	cleanerType := reflect.TypeOf(components.CleanerComponent{})

	initialCount := len(world.GetEntitiesWith(cleanerType))
	if initialCount != 1 {
		t.Fatalf("Expected 1 cleaner initially, got %d", initialCount)
	}

	// Run animation until completion
	maxIterations := 1000
	for i := 0; i < maxIterations && !cleanerSystem.IsAnimationComplete(); i++ {
		cleanerSystem.Update(world, 16*time.Millisecond)
		time.Sleep(1 * time.Millisecond)
	}

	// Verify entity was destroyed
	finalCount := len(world.GetEntitiesWith(cleanerType))
	if finalCount != 0 {
		t.Errorf("Expected 0 cleaners after completion, got %d", finalCount)
	}
}

// TestCleanerReactivation verifies cleaners can be activated multiple times
func TestCleanerReactivation(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	// First cycle
	createRedCharacterAt(world, 40, 5)
	cleanerSystem.ActivateCleaners(world)

	// Run until complete
	maxIterations := 1000
	for i := 0; i < maxIterations; i++ {
		cleanerSystem.Update(world, 16*time.Millisecond)
		time.Sleep(1 * time.Millisecond)
		if cleanerSystem.IsAnimationComplete() {
			break
		}
	}

	if !cleanerSystem.IsAnimationComplete() {
		t.Fatal("First animation did not complete")
	}

	// Second cycle
	createRedCharacterAt(world, 20, 8)
	cleanerSystem.ActivateCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Verify new cleaner was spawned
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	if len(cleaners) != 1 {
		t.Errorf("Expected 1 cleaner in second cycle, got %d", len(cleaners))
	}

	if cleanerSystem.IsAnimationComplete() {
		t.Error("Expected animation to be active in second cycle")
	}
}
