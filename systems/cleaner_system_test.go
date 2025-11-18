package systems

import (
	"reflect"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// TestCleanersTriggerConditions verifies cleaners only activate when heat is at max during gold completion
func TestCleanersTriggerConditions(t *testing.T) {
	tests := []struct {
		name          string
		currentHeat   int
		maxHeat       int
		shouldTrigger bool
	}{
		{
			name:          "Heat below max - no trigger",
			currentHeat:   50,
			maxHeat:       100,
			shouldTrigger: false,
		},
		{
			name:          "Heat at max - should trigger",
			currentHeat:   100,
			maxHeat:       100,
			shouldTrigger: true,
		},
		{
			name:          "Heat above max - should trigger",
			currentHeat:   110,
			maxHeat:       100,
			shouldTrigger: true,
		},
		{
			name:          "Heat zero - no trigger",
			currentHeat:   0,
			maxHeat:       100,
			shouldTrigger: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			world := engine.NewWorld()
			ctx := createCleanerTestContext()

			// Create cleaner and gold systems
			cleanerSystem := NewCleanerSystem(ctx, 80, 24)
			goldSystem := NewGoldSequenceSystem(ctx, nil, 80, 24, 0, 0)
			goldSystem.SetCleanerTrigger(cleanerSystem.TriggerCleaners)

			// Create some Red characters to clean
			createRedCharacterAt(world, 10, 5)

			// Trigger cleaners through gold system
			goldSystem.TriggerCleanersIfHeatFull(world, tt.currentHeat, tt.maxHeat)

			// Verify activation state
			if cleanerSystem.IsActive() != tt.shouldTrigger {
				t.Errorf("Expected IsActive=%v, got %v", tt.shouldTrigger, cleanerSystem.IsActive())
			}

			// Verify cleaner entities were created if triggered
			cleanerType := reflect.TypeOf(components.CleanerComponent{})
			cleaners := world.GetEntitiesWith(cleanerType)

			if tt.shouldTrigger && len(cleaners) == 0 {
				t.Error("Expected cleaners to be created when triggered, but none found")
			}
			if !tt.shouldTrigger && len(cleaners) > 0 {
				t.Errorf("Expected no cleaners when not triggered, but found %d", len(cleaners))
			}
		})
	}
}

// TestCleanersDirectionAlternation verifies odd rows go L→R and even rows go R→L
func TestCleanersDirectionAlternation(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24)

	// Create Red characters on multiple rows
	createRedCharacterAt(world, 10, 0) // Row 0 (even)
	createRedCharacterAt(world, 10, 1) // Row 1 (odd)
	createRedCharacterAt(world, 10, 2) // Row 2 (even)
	createRedCharacterAt(world, 10, 3) // Row 3 (odd)

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)

	// Get cleaner entities
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	if len(cleaners) != 4 {
		t.Fatalf("Expected 4 cleaners, got %d", len(cleaners))
	}

	// Verify each cleaner's direction and starting position
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

// TestCleanersRemoveOnlyRedCharacters verifies cleaners remove Red but not Blue/Green
func TestCleanersRemoveOnlyRedCharacters(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24)

	// Create mixed character types on row 5
	redEntity := createRedCharacterAt(world, 40, 5)
	blueEntity := createBlueCharacterAt(world, 41, 5)
	greenEntity := createGreenCharacterAt(world, 42, 5)

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)

	// Simulate cleaner moving across row 5
	// We need to manually position the cleaner at each X position
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	if len(cleaners) != 1 {
		t.Fatalf("Expected 1 cleaner for row 5, got %d", len(cleaners))
	}

	cleanerEntity := cleaners[0]

	// Move cleaner to each character position and check collision
	for x := 40; x <= 42; x++ {
		// Update cleaner position
		cleanerComp, _ := world.GetComponent(cleanerEntity, cleanerType)
		cleaner := cleanerComp.(components.CleanerComponent)
		cleaner.XPosition = float64(x)
		world.AddComponent(cleanerEntity, cleaner)

		// Detect and destroy
		cleanerSystem.detectAndDestroyRedCharacters(world)
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

// TestCleanersAnimationCompletion verifies cleaners deactivate after animation duration
func TestCleanersAnimationCompletion(t *testing.T) {
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

	cleanerSystem := NewCleanerSystem(ctx, 80, 24)

	// Create Red character
	createRedCharacterAt(world, 10, 5)

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)

	if !cleanerSystem.IsActive() {
		t.Fatal("Cleaners should be active after trigger")
	}

	// Advance time (just before duration)
	mockTime.Advance(900 * time.Millisecond)
	cleanerSystem.Update(world, 16*time.Millisecond) // dt is ignored, uses TimeProvider

	if !cleanerSystem.IsActive() {
		t.Error("Cleaners should still be active before duration expires")
	}

	// Advance time (after duration)
	mockTime.Advance(200 * time.Millisecond) // Total: 1.1 seconds
	cleanerSystem.Update(world, 16*time.Millisecond)

	if cleanerSystem.IsActive() {
		t.Error("Cleaners should be inactive after duration expires")
	}

	// Verify cleaners were cleaned up
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	if len(cleaners) != 0 {
		t.Errorf("Expected 0 cleaners after cleanup, got %d", len(cleaners))
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
	cleanerSystem := NewCleanerSystem(ctx, gameWidth, 24)

	// Create Red character
	createRedCharacterAt(world, 10, 1) // Row 1 (odd, L→R)

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)

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

	// Advance time by 0.5 seconds
	mockTime.Advance(500 * time.Millisecond)

	// Update cleaner system
	cleanerSystem.Update(world, 16*time.Millisecond) // dt is ignored

	cleanerComp, _ = world.GetComponent(entity, cleanerType)
	cleaner = cleanerComp.(components.CleanerComponent)

	expectedDistance := expectedSpeed * 0.5 // 0.5 seconds
	actualDistance := cleaner.XPosition - initialPos

	// Allow small floating point error
	if abs(actualDistance-expectedDistance) > 0.1 {
		t.Errorf("Expected movement distance %f, got %f", expectedDistance, actualDistance)
	}
}

// TestCleanersNoRedCharacters verifies cleaners don't activate when no Red characters exist
func TestCleanersNoRedCharacters(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24)

	// Create only Blue and Green characters
	createBlueCharacterAt(world, 10, 5)
	createGreenCharacterAt(world, 20, 10)

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)

	// Should not activate since no Red characters
	if cleanerSystem.IsActive() {
		t.Error("Cleaners should not activate when no Red characters exist")
	}
}

// TestCleanersMultipleRows verifies cleaners work correctly across multiple rows
func TestCleanersMultipleRows(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24)

	// Create Red characters on rows 1, 5, 10
	createRedCharacterAt(world, 10, 1)
	createRedCharacterAt(world, 20, 5)
	createRedCharacterAt(world, 30, 10)

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)

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

// TestCleanersTrailTracking verifies trail positions are tracked correctly
func TestCleanersTrailTracking(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24)

	// Create Red character
	createRedCharacterAt(world, 10, 1)

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)

	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	entity := cleaners[0]

	// Update multiple times to build trail
	for i := 0; i < 15; i++ {
		cleanerSystem.Update(world, 16*time.Millisecond) // 60 FPS
	}

	cleanerComp, _ := world.GetComponent(entity, cleanerType)
	cleaner := cleanerComp.(components.CleanerComponent)

	// Trail should be capped at CleanerTrailLength
	if len(cleaner.TrailPositions) > constants.CleanerTrailLength {
		t.Errorf("Trail length should be capped at %d, got %d",
			constants.CleanerTrailLength, len(cleaner.TrailPositions))
	}

	// Trail should contain recent positions
	if len(cleaner.TrailPositions) == 0 {
		t.Error("Trail should contain positions after multiple updates")
	}

	// Current position should be in trail
	found := false
	for _, pos := range cleaner.TrailPositions {
		if abs(pos-cleaner.XPosition) < 0.1 {
			found = true
			break
		}
	}
	if !found {
		t.Error("Current position should be in trail")
	}
}

// TestCleanersDuplicateTriggerIgnored verifies duplicate triggers are ignored
func TestCleanersDuplicateTriggerIgnored(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24)

	// Create Red characters
	createRedCharacterAt(world, 10, 5)

	// First trigger
	cleanerSystem.TriggerCleaners(world)

	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners1 := world.GetEntitiesWith(cleanerType)
	count1 := len(cleaners1)

	// Second trigger (should be ignored)
	cleanerSystem.TriggerCleaners(world)

	cleaners2 := world.GetEntitiesWith(cleanerType)
	count2 := len(cleaners2)

	if count1 != count2 {
		t.Errorf("Duplicate trigger created new cleaners: before=%d, after=%d", count1, count2)
	}
}

// Helper functions

func createCleanerTestContext() *engine.GameContext {
	timeProvider := engine.NewMonotonicTimeProvider()
	world := engine.NewWorld()

	return &engine.GameContext{
		World:        world,
		TimeProvider: timeProvider,
		GameWidth:    80,
		GameHeight:   24,
	}
}

func createRedCharacterAt(world *engine.World, x, y int) engine.Entity {
	entity := world.CreateEntity()

	world.AddComponent(entity, components.PositionComponent{X: x, Y: y})
	world.AddComponent(entity, components.CharacterComponent{
		Rune:  'R',
		Style: render.GetStyleForSequence(components.SequenceRed, components.LevelBright),
	})
	world.AddComponent(entity, components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceRed,
		Level: components.LevelBright,
	})

	world.UpdateSpatialIndex(entity, x, y)
	return entity
}

func createBlueCharacterAt(world *engine.World, x, y int) engine.Entity {
	entity := world.CreateEntity()

	world.AddComponent(entity, components.PositionComponent{X: x, Y: y})
	world.AddComponent(entity, components.CharacterComponent{
		Rune:  'B',
		Style: render.GetStyleForSequence(components.SequenceBlue, components.LevelBright),
	})
	world.AddComponent(entity, components.SequenceComponent{
		ID:    2,
		Index: 0,
		Type:  components.SequenceBlue,
		Level: components.LevelBright,
	})

	world.UpdateSpatialIndex(entity, x, y)
	return entity
}

func createGreenCharacterAt(world *engine.World, x, y int) engine.Entity {
	entity := world.CreateEntity()

	world.AddComponent(entity, components.PositionComponent{X: x, Y: y})
	world.AddComponent(entity, components.CharacterComponent{
		Rune:  'G',
		Style: render.GetStyleForSequence(components.SequenceGreen, components.LevelBright),
	})
	world.AddComponent(entity, components.SequenceComponent{
		ID:    3,
		Index: 0,
		Type:  components.SequenceGreen,
		Level: components.LevelBright,
	})

	world.UpdateSpatialIndex(entity, x, y)
	return entity
}

func entityExists(world *engine.World, entity engine.Entity) bool {
	posType := reflect.TypeOf(components.PositionComponent{})
	_, exists := world.GetComponent(entity, posType)
	return exists
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
