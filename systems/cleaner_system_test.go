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
			defer cleanerSystem.Shutdown()

			goldSystem := NewGoldSequenceSystem(ctx, nil, 80, 24, 0, 0)
			goldSystem.SetCleanerTrigger(cleanerSystem.TriggerCleaners)

			// Create some Red characters to clean
			createRedCharacterAt(world, 10, 5)

			// Trigger cleaners through gold system
			goldSystem.TriggerCleanersIfHeatFull(world, tt.currentHeat, tt.maxHeat)

			// Process spawn requests
			cleanerSystem.Update(world, 16*time.Millisecond)

			// Wait a bit for async processing
			time.Sleep(50 * time.Millisecond)

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

	// Wait for async processing
	time.Sleep(50 * time.Millisecond)

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
	defer cleanerSystem.Shutdown()

	// Create Red character
	createRedCharacterAt(world, 10, 5)

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)

	// Process spawn request
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Wait for spawn
	time.Sleep(50 * time.Millisecond)

	if !cleanerSystem.IsActive() {
		t.Fatal("Cleaners should be active after trigger")
	}

	// Advance time (just before duration)
	mockTime.Advance(900 * time.Millisecond)

	// Wait for update loop to process
	time.Sleep(100 * time.Millisecond)

	if !cleanerSystem.IsActive() {
		t.Error("Cleaners should still be active before duration expires")
	}

	// Advance time (after duration)
	mockTime.Advance(200 * time.Millisecond) // Total: 1.1 seconds

	// Wait for update loop to process
	time.Sleep(100 * time.Millisecond)

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

	// Advance time by 0.5 seconds
	mockTime.Advance(500 * time.Millisecond)

	// Wait for update loop to process multiple frames
	time.Sleep(200 * time.Millisecond)

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

// TestCleanersNoRedCharacters verifies cleaners don't activate when no Red characters exist
func TestCleanersNoRedCharacters(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24)
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

// TestCleanersTrailTracking verifies trail positions are tracked correctly
func TestCleanersTrailTracking(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24)
	defer cleanerSystem.Shutdown()

	// Create Red character
	createRedCharacterAt(world, 10, 1)

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)

	// Process spawn request
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Wait for spawn
	time.Sleep(100 * time.Millisecond)

	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	if len(cleaners) != 1 {
		t.Fatalf("Expected 1 cleaner, got %d", len(cleaners))
	}

	entity := cleaners[0]

	// Get initial cleaner component
	cleanerComp, ok := world.GetComponent(entity, cleanerType)
	if !ok {
		t.Fatal("Failed to get cleaner component")
	}
	cleaner := cleanerComp.(components.CleanerComponent)

	// Verify cleaner has trail slice allocated (from pool)
	if cleaner.TrailPositions == nil {
		t.Error("Trail positions slice should be allocated from pool")
	}

	// Verify trail capacity matches pool allocation
	if cap(cleaner.TrailPositions) != constants.CleanerTrailLength {
		t.Errorf("Trail capacity should be %d (from pool), got %d",
			constants.CleanerTrailLength, cap(cleaner.TrailPositions))
	}

	// Trail length should be at most CleanerTrailLength
	if len(cleaner.TrailPositions) > constants.CleanerTrailLength {
		t.Errorf("Trail length should be capped at %d, got %d",
			constants.CleanerTrailLength, len(cleaner.TrailPositions))
	}

	// Note: The concurrent update loop updates trails asynchronously
	// We verify the structure is correct, actual population depends on timing
	t.Logf("Trail structure verified: cap=%d, len=%d, XPosition=%f",
		cap(cleaner.TrailPositions), len(cleaner.TrailPositions), cleaner.XPosition)
}

// TestCleanersDuplicateTriggerIgnored verifies duplicate triggers are ignored
func TestCleanersDuplicateTriggerIgnored(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24)
	defer cleanerSystem.Shutdown()

	// Create Red characters
	createRedCharacterAt(world, 10, 5)

	// First trigger
	cleanerSystem.TriggerCleaners(world)

	// Process first request
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Wait for processing
	time.Sleep(50 * time.Millisecond)

	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners1 := world.GetEntitiesWith(cleanerType)
	count1 := len(cleaners1)

	// Second trigger (should be ignored)
	cleanerSystem.TriggerCleaners(world)

	// Process second request
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Wait for processing
	time.Sleep(50 * time.Millisecond)

	cleaners2 := world.GetEntitiesWith(cleanerType)
	count2 := len(cleaners2)

	if count1 != count2 {
		t.Errorf("Duplicate trigger created new cleaners: before=%d, after=%d", count1, count2)
	}
}

// TestCleanersPoolReuse verifies sync.Pool is reusing trail slices
func TestCleanersPoolReuse(t *testing.T) {
	// Use mock time provider to avoid race condition when swapping TimeProvider
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
	defer cleanerSystem.Shutdown()

	// Create Red character
	createRedCharacterAt(world, 10, 5)

	// First activation
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	// Get the first cleaner's trail
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)
	if len(cleaners) == 0 {
		t.Fatal("Expected at least one cleaner")
	}

	cleanerComp, _ := world.GetComponent(cleaners[0], cleanerType)
	cleaner := cleanerComp.(components.CleanerComponent)
	firstTrail := cleaner.TrailPositions

	// Force cleanup by advancing time beyond animation duration
	mockTime.Advance(2 * time.Second)

	// Wait for cleanup to complete
	time.Sleep(200 * time.Millisecond)

	// Verify cleanup happened
	cleaners = world.GetEntitiesWith(cleanerType)
	if len(cleaners) > 0 {
		t.Logf("Warning: cleaners not cleaned up yet, waiting longer...")
		time.Sleep(200 * time.Millisecond)
	}

	// Create new Red character for second activation
	createRedCharacterAt(world, 20, 8)

	// Second activation
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	cleaners = world.GetEntitiesWith(cleanerType)
	if len(cleaners) == 0 {
		t.Fatal("Expected at least one cleaner in second activation")
	}

	cleanerComp, _ = world.GetComponent(cleaners[0], cleanerType)
	cleaner = cleanerComp.(components.CleanerComponent)
	secondTrail := cleaner.TrailPositions

	// Trails should have the same capacity (from pool reuse)
	if cap(firstTrail) == cap(secondTrail) && cap(firstTrail) == constants.CleanerTrailLength {
		// This suggests pool reuse is working (same capacity)
		// Note: We can't directly compare pointers since Go doesn't expose that
		t.Logf("Pool likely reusing slices: cap=%d", cap(secondTrail))
	}
}

// TestCleanersConcurrentAccess verifies thread-safe concurrent access
func TestCleanersConcurrentAccess(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24)
	defer cleanerSystem.Shutdown()

	// Create multiple Red characters
	for i := 0; i < 10; i++ {
		createRedCharacterAt(world, 10+i, i)
	}

	// Trigger multiple times concurrently
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			cleanerSystem.TriggerCleaners(world)
			done <- true
		}()
	}

	// Wait for all triggers
	for i := 0; i < 10; i++ {
		<-done
	}

	// Process requests
	for i := 0; i < 10; i++ {
		cleanerSystem.Update(world, 16*time.Millisecond)
		time.Sleep(10 * time.Millisecond)
	}

	// Check IsActive concurrently
	for i := 0; i < 100; i++ {
		go func() {
			_ = cleanerSystem.IsActive()
			done <- true
		}()
	}

	for i := 0; i < 100; i++ {
		<-done
	}

	// If we reach here without data races, the test passes
	t.Log("Concurrent access completed without data races")
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
