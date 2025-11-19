package systems

import (
	"reflect"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestCleanersRemoveOnlyRedCharacters verifies cleaners remove Red but not Blue/Green
func TestCleanersRemoveOnlyRedCharacters(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Create mixed character types on row 5
	redEntity := createRedCharacterAt(world, 40, 5)
	blueEntity := createBlueCharacterAt(world, 41, 5)
	greenEntity := createGreenCharacterAt(world, 42, 5)

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)

	// Process spawn request
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Wait for spawn to complete
	time.Sleep(50 * time.Millisecond)

	// Simulate cleaner moving across row 5
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	if len(cleaners) != 1 {
		t.Fatalf("Expected 1 cleaner for row 5, got %d", len(cleaners))
	}

	cleanerEntity := cleaners[0]

	// Move cleaner to each character position and check collision
	for x := 40; x <= 42; x++ {
		// Update cleaner position and trail
		cleanerComp, _ := world.GetComponent(cleanerEntity, cleanerType)
		cleaner := cleanerComp.(components.CleanerComponent)
		cleaner.XPosition = float64(x)
		// Update trail with current position
		cleaner.TrailPositions = []float64{float64(x)}
		world.AddComponent(cleanerEntity, cleaner)

		// Check collisions via trail
		cleanerSystem.checkTrailCollisions(world, cleaner.Row, cleaner.TrailPositions)
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

// TestCleanersNoRedCharacters verifies phantom cleaner activation when no Red characters exist
func TestCleanersNoRedCharacters(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Create only Blue and Green characters
	createBlueCharacterAt(world, 10, 5)
	createGreenCharacterAt(world, 20, 10)

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)

	// Process spawn request
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Wait for processing
	time.Sleep(50 * time.Millisecond)

	// NEW BEHAVIOR: Phantom cleaners activate even without Red characters
	// This ensures proper phase transitions
	if !cleanerSystem.IsActive() {
		t.Error("Cleaners should activate (phantom mode) even when no Red characters exist for proper phase transitions")
	}

	// Verify no visual cleaner entities were spawned (phantom mode)
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)
	if len(cleaners) != 0 {
		t.Errorf("Expected 0 visual cleaners (phantom mode), got %d", len(cleaners))
	}
}

// TestCleanerCollisionCoverage verifies no position gaps in collision detection
// when cleaner moves more than 1 character per frame
//
// Mathematical context:
// - Cleaner speed: ~80 chars/sec (gameWidth=80, duration=1s)
// - Frame time: 16ms
// - Movement per frame: 80 × 0.016 = 1.28 characters
// - Example: Moving from 8.84 to 10.12 should check positions 8, 9, and 10
func TestCleanerCollisionCoverage(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Create Red characters at positions 8, 9, and 10 on row 5
	red8 := createRedCharacterAt(world, 8, 5)
	red9 := createRedCharacterAt(world, 9, 5)
	red10 := createRedCharacterAt(world, 10, 5)

	// Trigger cleaners to create a cleaner on row 5
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	// Get the cleaner entity
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)
	if len(cleaners) != 1 {
		t.Fatalf("Expected 1 cleaner, got %d", len(cleaners))
	}

	cleanerEntity := cleaners[0]

	// Simulate cleaner moving from 8.84 to 10.12
	// This simulates the gap scenario: trail positions that skip integer position 9
	cleanerComp, _ := world.GetComponent(cleanerEntity, cleanerType)
	cleaner := cleanerComp.(components.CleanerComponent)
	cleaner.XPosition = 10.12
	// Trail: [current=10.12, previous=8.84]
	cleaner.TrailPositions = []float64{10.12, 8.84}
	world.AddComponent(cleanerEntity, cleaner)

	// Verify all Red characters exist before collision check
	if !entityExists(world, red8) {
		t.Fatal("Red at position 8 should exist before collision")
	}
	if !entityExists(world, red9) {
		t.Fatal("Red at position 9 should exist before collision")
	}
	if !entityExists(world, red10) {
		t.Fatal("Red at position 10 should exist before collision")
	}

	// Check trail collisions - should detect and destroy ALL Red characters
	// at positions 8, 9, and 10 (comprehensive range checking)
	cleanerSystem.checkTrailCollisions(world, cleaner.Row, cleaner.TrailPositions)

	// CRITICAL: Verify Red at position 9 was destroyed
	// This is the position that would be skipped without range checking
	if entityExists(world, red9) {
		t.Error("Red character at position 9 should be destroyed (was skipped in gap)")
	}

	// Also verify positions 8 and 10 were destroyed
	if entityExists(world, red8) {
		t.Error("Red character at position 8 should be destroyed")
	}
	if entityExists(world, red10) {
		t.Error("Red character at position 10 should be destroyed")
	}
}

// TestCleanerCollisionReverseDirection verifies gap coverage for R→L cleaners
func TestCleanerCollisionReverseDirection(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Create Red characters at positions 70, 71, and 72 on row 6 (even row, R→L)
	red70 := createRedCharacterAt(world, 70, 6)
	red71 := createRedCharacterAt(world, 71, 6)
	red72 := createRedCharacterAt(world, 72, 6)

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	// Get cleaner
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)
	if len(cleaners) != 1 {
		t.Fatalf("Expected 1 cleaner, got %d", len(cleaners))
	}

	cleanerEntity := cleaners[0]

	// Simulate cleaner moving R→L from 72.16 to 69.88
	// This should cover positions 72, 71, 70, and 69
	cleanerComp, _ := world.GetComponent(cleanerEntity, cleanerType)
	cleaner := cleanerComp.(components.CleanerComponent)
	cleaner.XPosition = 69.88
	// Trail: [current=69.88, previous=72.16]
	cleaner.TrailPositions = []float64{69.88, 72.16}
	world.AddComponent(cleanerEntity, cleaner)

	// Check collisions
	cleanerSystem.checkTrailCollisions(world, cleaner.Row, cleaner.TrailPositions)

	// Verify all Red characters were destroyed
	if entityExists(world, red70) {
		t.Error("Red character at position 70 should be destroyed")
	}
	if entityExists(world, red71) {
		t.Error("Red character at position 71 should be destroyed (was in gap)")
	}
	if entityExists(world, red72) {
		t.Error("Red character at position 72 should be destroyed")
	}
}

// TestCleanerCollisionLongTrail verifies collision coverage across entire trail
func TestCleanerCollisionLongTrail(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Create Red characters at multiple positions
	positions := []int{15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25}
	redEntities := make(map[int]engine.Entity)
	for _, x := range positions {
		redEntities[x] = createRedCharacterAt(world, x, 3)
	}

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	// Get cleaner
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)
	if len(cleaners) != 1 {
		t.Fatalf("Expected 1 cleaner, got %d", len(cleaners))
	}

	cleanerEntity := cleaners[0]

	// Simulate cleaner with long trail covering positions 15-25
	// Trail positions with gaps: 25.1, 23.2, 21.3, 19.4, 17.5, 15.6
	cleanerComp, _ := world.GetComponent(cleanerEntity, cleanerType)
	cleaner := cleanerComp.(components.CleanerComponent)
	cleaner.XPosition = 25.1
	cleaner.TrailPositions = []float64{25.1, 23.2, 21.3, 19.4, 17.5, 15.6}
	world.AddComponent(cleanerEntity, cleaner)

	// Check collisions
	cleanerSystem.checkTrailCollisions(world, cleaner.Row, cleaner.TrailPositions)

	// Verify ALL Red characters were destroyed (including those in gaps)
	for x, entity := range redEntities {
		if entityExists(world, entity) {
			t.Errorf("Red character at position %d should be destroyed", x)
		}
	}
}
