package systems

import (
	"reflect"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestCleanersDirectionAlternation verifies odd rows go L→R and even rows go R→L
func TestCleanersDirectionAlternation(t *testing.T) {
	// Use mock time provider to prevent cleaners from moving before we check positions
	startTime := time.Now()
	mockTime := engine.NewMockTimeProvider(startTime)

	world := engine.NewWorld()
	ctx := &engine.GameContext{
		World:        world,
		TimeProvider: mockTime,
		GameWidth:    80,
		GameHeight:   24,
	}

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Create Red characters on multiple rows
	createRedCharacterAt(world, 10, 0) // Row 0 (even)
	createRedCharacterAt(world, 10, 1) // Row 1 (odd)
	createRedCharacterAt(world, 10, 2) // Row 2 (even)
	createRedCharacterAt(world, 10, 3) // Row 3 (odd)

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)

	// Process spawn request
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Wait for async spawn processing (real time) but don't advance mock time
	time.Sleep(50 * time.Millisecond)

	// Get cleaner entities
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	if len(cleaners) != 4 {
		t.Fatalf("Expected 4 cleaners, got %d", len(cleaners))
	}

	// Verify each cleaner's direction and starting position
	// Since we haven't advanced mock time, cleaners should be at their initial positions
	for _, entity := range cleaners {
		cleanerComp, ok := world.GetComponent(entity, cleanerType)
		if !ok {
			t.Fatal("Failed to get cleaner component")
		}
		cleaner := cleanerComp.(components.CleanerComponent)

		if cleaner.Row%2 == 0 {
			// Even row: R→L (direction = -1, start at right)
			if cleaner.Direction != -1 {
				t.Errorf("Row %d (even): expected direction -1, got %d", cleaner.Row, cleaner.Direction)
			}
			if cleaner.XPosition != 80.0 {
				t.Errorf("Row %d (even): expected start position 80.0, got %f", cleaner.Row, cleaner.XPosition)
			}
		} else {
			// Odd row: L→R (direction = 1, start at left)
			if cleaner.Direction != 1 {
				t.Errorf("Row %d (odd): expected direction 1, got %d", cleaner.Row, cleaner.Direction)
			}
			if cleaner.XPosition != -1.0 {
				t.Errorf("Row %d (odd): expected start position -1.0, got %f", cleaner.Row, cleaner.XPosition)
			}
		}
	}
}

// TestCleanersMovementSpeed verifies cleaners move at correct speed
func TestCleanersMovementSpeed(t *testing.T) {
	// Use mock time provider for controlled time advancement
	startTime := time.Now()
	mockTime := engine.NewMockTimeProvider(startTime)

	world := engine.NewWorld()
	ctx := &engine.GameContext{
		World:        world,
		TimeProvider: mockTime,
		GameWidth:    80,
		GameHeight:   24,
	}

	gameWidth := 80
	cleanerSystem := NewCleanerSystem(ctx, gameWidth, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Create Red character
	createRedCharacterAt(world, 10, 1) // Row 1 (odd, L→R)

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)

	// Process spawn request
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Wait for spawn
	time.Sleep(50 * time.Millisecond)

	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	if len(cleaners) != 1 {
		t.Fatalf("Expected 1 cleaner, got %d", len(cleaners))
	}

	entity := cleaners[0]
	cleanerComp, _ := world.GetComponent(entity, cleanerType)
	cleaner := cleanerComp.(components.CleanerComponent)

	// Expected speed: gameWidth / animationDuration = 80 / 1.0 = 80 pixels/second
	expectedSpeed := float64(gameWidth) / constants.CleanerAnimationDuration.Seconds()

	if cleaner.Speed != expectedSpeed {
		t.Errorf("Expected speed %f, got %f", expectedSpeed, cleaner.Speed)
	}

	// Record initial position
	initialPos := cleaner.XPosition

	// Simulate 0.5 seconds of game loop updates at 60fps (16ms per frame)
	frameDuration := 16 * time.Millisecond
	totalTime := 500 * time.Millisecond
	numFrames := int(totalTime / frameDuration)

	for i := 0; i < numFrames; i++ {
		mockTime.Advance(frameDuration)
		cleanerSystem.Update(world, frameDuration)
	}

	cleanerComp, _ = world.GetComponent(entity, cleanerType)
	cleaner = cleanerComp.(components.CleanerComponent)

	expectedDistance := expectedSpeed * 0.5 // 0.5 seconds
	actualDistance := cleaner.XPosition - initialPos

	// Allow for some variance due to frame timing
	tolerance := expectedSpeed * 0.1 // 10% tolerance
	if abs(actualDistance-expectedDistance) > tolerance {
		t.Errorf("Expected movement distance ~%f, got %f (tolerance: %f)",
			expectedDistance, actualDistance, tolerance)
	}
}

// TestCleanersMultipleRows verifies cleaners work correctly across multiple rows
func TestCleanersMultipleRows(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Create Red characters on rows 1, 5, 10
	createRedCharacterAt(world, 10, 1)
	createRedCharacterAt(world, 20, 5)
	createRedCharacterAt(world, 30, 10)

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)

	// Process spawn request
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Wait for processing
	time.Sleep(50 * time.Millisecond)

	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	if len(cleaners) != 3 {
		t.Fatalf("Expected 3 cleaners, got %d", len(cleaners))
	}

	// Verify each row has a cleaner
	rows := make(map[int]bool)
	for _, entity := range cleaners {
		cleanerComp, _ := world.GetComponent(entity, cleanerType)
		cleaner := cleanerComp.(components.CleanerComponent)
		rows[cleaner.Row] = true
	}

	expectedRows := []int{1, 5, 10}
	for _, row := range expectedRows {
		if !rows[row] {
			t.Errorf("Expected cleaner on row %d, but not found", row)
		}
	}
}
