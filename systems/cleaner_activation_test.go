package systems

import (
	"reflect"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestCleanerActivation verifies cleaner activation creates entities for Red rows
func TestCleanerActivation(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	// Create Red characters on multiple rows
	createRedCharacterAt(world, 10, 5)
	createRedCharacterAt(world, 20, 5)
	createRedCharacterAt(world, 15, 8)

	// Verify not active initially
	if !cleanerSystem.IsAnimationComplete() {
		t.Error("Expected animation to be complete initially")
	}

	// Activate cleaners
	cleanerSystem.ActivateCleaners(world)

	// Process spawn on next update
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Verify cleaners were spawned (one per unique row with Red)
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	if len(cleaners) != 2 {
		t.Errorf("Expected 2 cleaners (rows 5 and 8), got %d", len(cleaners))
	}

	// Verify animation is now active
	if cleanerSystem.IsAnimationComplete() {
		t.Error("Expected animation to be active after spawning")
	}
}

// TestCleanerActivationWithoutRed verifies phantom cleaner behavior (no entities spawned when no Red)
func TestCleanerActivationWithoutRed(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	// Create only Blue and Green characters
	createBlueCharacterAt(world, 10, 5)
	createGreenCharacterAt(world, 20, 10)

	// Activate cleaners
	cleanerSystem.ActivateCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Verify no visual cleaner entities were spawned (phantom mode)
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	if len(cleaners) != 0 {
		t.Errorf("Expected no cleaners in phantom mode, got %d", len(cleaners))
	}

	// Animation should complete immediately (no entities to track)
	if !cleanerSystem.IsAnimationComplete() {
		t.Error("Expected animation to complete immediately in phantom mode")
	}
}

// TestCleanerDuplicateActivationIgnored verifies duplicate activation is handled correctly
func TestCleanerDuplicateActivationIgnored(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	// Create Red characters
	createRedCharacterAt(world, 10, 5)

	// Activate multiple times before Update
	cleanerSystem.ActivateCleaners(world)
	cleanerSystem.ActivateCleaners(world)
	cleanerSystem.ActivateCleaners(world)

	// Process spawn
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Verify only one cleaner was spawned (not duplicated)
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	if len(cleaners) != 1 {
		t.Errorf("Expected 1 cleaner despite multiple activations, got %d", len(cleaners))
	}
}

// TestCleanerActivationAfterCompletion verifies cleaners can be reactivated after completion
func TestCleanerActivationAfterCompletion(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	// First activation cycle
	createRedCharacterAt(world, 10, 5)
	cleanerSystem.ActivateCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Wait for animation to complete (run updates until complete)
	maxIterations := 1000
	for i := 0; i < maxIterations && !cleanerSystem.IsAnimationComplete(); i++ {
		cleanerSystem.Update(world, 16*time.Millisecond)
		time.Sleep(1 * time.Millisecond)
	}

	if !cleanerSystem.IsAnimationComplete() {
		t.Fatal("Animation did not complete in reasonable time")
	}

	// Second activation cycle
	createRedCharacterAt(world, 20, 8)
	cleanerSystem.ActivateCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Verify new cleaners were spawned
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	if len(cleaners) != 1 {
		t.Errorf("Expected 1 cleaner in second activation, got %d", len(cleaners))
	}
}
