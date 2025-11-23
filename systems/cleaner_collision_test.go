package systems

import (
	"reflect"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestCleanerRemovesRedCharacters verifies cleaners destroy Red characters during sweep
func TestCleanerRemovesRedCharacters(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	// Create Red characters on row 5
	redEntity1 := createRedCharacterAt(world, 10, 5)
	redEntity2 := createRedCharacterAt(world, 40, 5)
	redEntity3 := createRedCharacterAt(world, 70, 5)

	// Activate cleaners and run animation
	cleanerSystem.ActivateCleaners(world)

	// Run animation until complete
	maxIterations := 1000
	for i := 0; i < maxIterations && !cleanerSystem.IsAnimationComplete(); i++ {
		cleanerSystem.Update(world, 16*time.Millisecond)
		time.Sleep(1 * time.Millisecond)
	}

	if !cleanerSystem.IsAnimationComplete() {
		t.Fatal("Animation did not complete in reasonable time")
	}

	// Verify all Red characters were destroyed
	if entityExists(world, redEntity1) {
		t.Error("Red character at x=10 should have been destroyed")
	}
	if entityExists(world, redEntity2) {
		t.Error("Red character at x=40 should have been destroyed")
	}
	if entityExists(world, redEntity3) {
		t.Error("Red character at x=70 should have been destroyed")
	}
}

// TestCleanerSelectivity verifies cleaners only remove Red, not Blue/Green
func TestCleanerSelectivity(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	// Create mixed character types on row 5
	redEntity := createRedCharacterAt(world, 40, 5)
	blueEntity := createBlueCharacterAt(world, 41, 5)
	greenEntity := createGreenCharacterAt(world, 42, 5)

	// Activate cleaners and run animation
	cleanerSystem.ActivateCleaners(world)

	// Run animation until complete
	maxIterations := 1000
	for i := 0; i < maxIterations && !cleanerSystem.IsAnimationComplete(); i++ {
		cleanerSystem.Update(world, 16*time.Millisecond)
		time.Sleep(1 * time.Millisecond)
	}

	if !cleanerSystem.IsAnimationComplete() {
		t.Fatal("Animation did not complete in reasonable time")
	}

	// Verify only Red was destroyed
	if entityExists(world, redEntity) {
		t.Error("Red character should have been destroyed")
	}
	if !entityExists(world, blueEntity) {
		t.Error("Blue character should NOT have been destroyed")
	}
	if !entityExists(world, greenEntity) {
		t.Error("Green character should NOT have been destroyed")
	}
}

// TestCleanerSweptSegmentCollision verifies collision detection across swept segment
func TestCleanerSweptSegmentCollision(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	// Create Red characters evenly spaced across row 5
	redEntities := make([]engine.Entity, 0)
	for x := 5; x < 75; x += 5 {
		redEntities = append(redEntities, createRedCharacterAt(world, x, 5))
	}

	// Activate cleaners
	cleanerSystem.ActivateCleaners(world)

	// Run animation until complete
	maxIterations := 1000
	for i := 0; i < maxIterations && !cleanerSystem.IsAnimationComplete(); i++ {
		cleanerSystem.Update(world, 16*time.Millisecond)
		time.Sleep(1 * time.Millisecond)
	}

	if !cleanerSystem.IsAnimationComplete() {
		t.Fatal("Animation did not complete in reasonable time")
	}

	// Verify ALL Red characters were destroyed (no tunneling/skipping)
	for i, entity := range redEntities {
		if entityExists(world, entity) {
			t.Errorf("Red character at index %d should have been destroyed (swept segment collision)", i)
		}
	}
}

// TestCleanerMultipleRows verifies cleaners handle multiple rows correctly
func TestCleanerMultipleRows(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	// Create Red characters on rows 3, 7, 15
	red1 := createRedCharacterAt(world, 20, 3)
	red2 := createRedCharacterAt(world, 40, 7)
	red3 := createRedCharacterAt(world, 60, 15)

	// Activate cleaners
	cleanerSystem.ActivateCleaners(world)

	// Verify 3 cleaners were spawned (one per row)
	cleanerSystem.Update(world, 16*time.Millisecond)
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	if len(cleaners) != 3 {
		t.Errorf("Expected 3 cleaners (one per row), got %d", len(cleaners))
	}

	// Run animation until complete
	maxIterations := 1000
	for i := 0; i < maxIterations && !cleanerSystem.IsAnimationComplete(); i++ {
		cleanerSystem.Update(world, 16*time.Millisecond)
		time.Sleep(1 * time.Millisecond)
	}

	if !cleanerSystem.IsAnimationComplete() {
		t.Fatal("Animation did not complete in reasonable time")
	}

	// Verify all Red characters were destroyed
	if entityExists(world, red1) {
		t.Error("Red character on row 3 should have been destroyed")
	}
	if entityExists(world, red2) {
		t.Error("Red character on row 7 should have been destroyed")
	}
	if entityExists(world, red3) {
		t.Error("Red character on row 15 should have been destroyed")
	}
}
