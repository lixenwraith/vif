package systems

import (
	"reflect"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestDeterministicCleanerBehavior verifies consistent cleaner behavior across runs
func TestDeterministicCleanerBehavior(t *testing.T) {
	// Run the same scenario multiple times and verify identical behavior
	for run := 0; run < 3; run++ {
		world := engine.NewWorld()
		ctx := createCleanerTestContext()
		cleanerSystem := NewCleanerSystem(ctx)

		// Create identical Red characters
		createRedCharacterAt(world, 40, 5)
		createRedCharacterAt(world, 40, 10)

		// Activate cleaners
		cleanerSystem.ActivateCleaners(world)
		cleanerSystem.Update(world, 16*time.Millisecond)

		// Verify cleaner count is consistent
		cleanerType := reflect.TypeOf(components.CleanerComponent{})
		cleaners := world.GetEntitiesWith(cleanerType)

		if len(cleaners) != 2 {
			t.Errorf("Run %d: Expected 2 cleaners, got %d", run, len(cleaners))
		}

		// Verify cleaners are on expected rows
		rows := make(map[int]bool)
		for _, entity := range cleaners {
			comp, ok := world.GetComponent(entity, cleanerType)
			if !ok {
				continue
			}
			c := comp.(components.CleanerComponent)
			rows[c.GridY] = true
		}

		if !rows[5] {
			t.Errorf("Run %d: Missing cleaner on row 5", run)
		}
		if !rows[10] {
			t.Errorf("Run %d: Missing cleaner on row 10", run)
		}
	}
}

// TestDeterministicDirection verifies row direction is always consistent
func TestDeterministicDirection(t *testing.T) {
	// Test multiple runs to verify direction is always deterministic
	for run := 0; run < 5; run++ {
		world := engine.NewWorld()
		ctx := createCleanerTestContext()
		cleanerSystem := NewCleanerSystem(ctx)

		// Create Red characters on rows 3 (odd) and 4 (even)
		createRedCharacterAt(world, 40, 3)
		createRedCharacterAt(world, 40, 4)

		// Activate cleaners
		cleanerSystem.ActivateCleaners(world)
		cleanerSystem.Update(world, 16*time.Millisecond)

		// Verify direction is consistent
		cleanerType := reflect.TypeOf(components.CleanerComponent{})
		cleaners := world.GetEntitiesWith(cleanerType)

		for _, entity := range cleaners {
			comp, ok := world.GetComponent(entity, cleanerType)
			if !ok {
				continue
			}
			c := comp.(components.CleanerComponent)

			if c.GridY == 3 {
				// Odd row: always L->R (positive velocity)
				if c.VelocityX <= 0 {
					t.Errorf("Run %d: Row 3 should have positive velocity, got %v", run, c.VelocityX)
				}
			} else if c.GridY == 4 {
				// Even row: always R->L (negative velocity)
				if c.VelocityX >= 0 {
					t.Errorf("Run %d: Row 4 should have negative velocity, got %v", run, c.VelocityX)
				}
			}
		}
	}
}

// TestDeterministicCollision verifies collision detection is consistent
func TestDeterministicCollision(t *testing.T) {
	// Run multiple times and verify Red characters are always destroyed
	for run := 0; run < 3; run++ {
		world := engine.NewWorld()
		ctx := createCleanerTestContext()
		cleanerSystem := NewCleanerSystem(ctx)

		// Create Red characters at known positions
		red1 := createRedCharacterAt(world, 20, 5)
		red2 := createRedCharacterAt(world, 40, 5)
		red3 := createRedCharacterAt(world, 60, 5)

		// Activate cleaners and run animation
		cleanerSystem.ActivateCleaners(world)

		// Run until complete
		maxIterations := 1000
		for i := 0; i < maxIterations && !cleanerSystem.IsAnimationComplete(); i++ {
			cleanerSystem.Update(world, 16*time.Millisecond)
			time.Sleep(1 * time.Millisecond)
		}

		// Verify all Red characters were destroyed (deterministic behavior)
		if entityExists(world, red1) {
			t.Errorf("Run %d: Red character 1 should be destroyed", run)
		}
		if entityExists(world, red2) {
			t.Errorf("Run %d: Red character 2 should be destroyed", run)
		}
		if entityExists(world, red3) {
			t.Errorf("Run %d: Red character 3 should be destroyed", run)
		}
	}
}
