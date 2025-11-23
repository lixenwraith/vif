package systems

import (
	"reflect"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestCleanerDirectionAlternation verifies odd rows go L->R and even rows go R->L
func TestCleanerDirectionAlternation(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	// Create Red characters on odd row (3) and even row (4)
	createRedCharacterAt(world, 40, 3)
	createRedCharacterAt(world, 40, 4)

	// Activate and spawn
	cleanerSystem.ActivateCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Check cleaner directions
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	if len(cleaners) != 2 {
		t.Fatalf("Expected 2 cleaners, got %d", len(cleaners))
	}

	for _, entity := range cleaners {
		comp, ok := world.GetComponent(entity, cleanerType)
		if !ok {
			continue
		}
		c := comp.(components.CleanerComponent)

		if c.GridY == 3 {
			// Odd row: should go left to right (positive velocity)
			if c.VelocityX <= 0 {
				t.Errorf("Row 3 (odd) should have positive VelocityX, got %v", c.VelocityX)
			}
		} else if c.GridY == 4 {
			// Even row: should go right to left (negative velocity)
			if c.VelocityX >= 0 {
				t.Errorf("Row 4 (even) should have negative VelocityX, got %v", c.VelocityX)
			}
		}
	}
}

// TestCleanerMovement verifies cleaners move correctly over time
func TestCleanerMovement(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	// Create Red character on row 5
	createRedCharacterAt(world, 40, 5)

	// Activate and spawn
	cleanerSystem.ActivateCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Get initial position
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	if len(cleaners) != 1 {
		t.Fatalf("Expected 1 cleaner, got %d", len(cleaners))
	}

	entity := cleaners[0]
	comp, _ := world.GetComponent(entity, cleanerType)
	initialX := comp.(components.CleanerComponent).PreciseX

	// Run several updates
	for i := 0; i < 10; i++ {
		cleanerSystem.Update(world, 16*time.Millisecond)
	}

	// Verify position changed
	if entityExists(world, entity) {
		comp, ok := world.GetComponent(entity, cleanerType)
		if ok {
			finalX := comp.(components.CleanerComponent).PreciseX
			if finalX == initialX {
				t.Error("Cleaner should have moved from initial position")
			}
		}
	}
}

// TestCleanerAnimationDuration verifies animation completes within reasonable time
func TestCleanerAnimationDuration(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	// Create Red character
	createRedCharacterAt(world, 40, 5)

	// Activate cleaners
	cleanerSystem.ActivateCleaners(world)

	// Track animation start time
	startTime := time.Now()

	// Run animation until complete
	maxIterations := 1000
	for i := 0; i < maxIterations && !cleanerSystem.IsAnimationComplete(); i++ {
		cleanerSystem.Update(world, 16*time.Millisecond)
		time.Sleep(1 * time.Millisecond)
	}

	duration := time.Since(startTime)

	if !cleanerSystem.IsAnimationComplete() {
		t.Fatal("Animation did not complete in reasonable time")
	}

	// Animation should complete in approximately CleanerAnimationDuration + some overhead
	// Allow generous margin for test timing variability
	maxExpected := constants.CleanerAnimationDuration * 3
	if duration > maxExpected {
		t.Errorf("Animation took %v, expected less than %v", duration, maxExpected)
	}
}

// TestCleanerTrailTracking verifies trail positions are tracked correctly
func TestCleanerTrailTracking(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	// Create Red character
	createRedCharacterAt(world, 40, 5)

	// Activate and spawn
	cleanerSystem.ActivateCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Run several updates to build trail
	for i := 0; i < 20; i++ {
		cleanerSystem.Update(world, 16*time.Millisecond)
		time.Sleep(1 * time.Millisecond)
	}

	// Verify trail exists and is limited to max length
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	if len(cleaners) == 0 {
		t.Skip("Cleaner already completed (test timing issue)")
	}

	for _, entity := range cleaners {
		comp, ok := world.GetComponent(entity, cleanerType)
		if !ok {
			continue
		}
		c := comp.(components.CleanerComponent)

		if len(c.Trail) == 0 {
			t.Error("Trail should not be empty")
		}

		if len(c.Trail) > constants.CleanerTrailLength {
			t.Errorf("Trail length %d exceeds max %d", len(c.Trail), constants.CleanerTrailLength)
		}
	}
}
