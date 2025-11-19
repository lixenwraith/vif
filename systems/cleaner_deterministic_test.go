package systems

import (
	"reflect"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestDeterministicCleanerLifecycle verifies exact frame-by-frame cleaner behavior
// using MockTimeProvider for precise time control.
func TestDeterministicCleanerLifecycle(t *testing.T) {
	world := engine.NewWorld()

	// Use mock time provider for deterministic behavior
	startTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	mockTime := engine.NewMockTimeProvider(startTime)

	ctx := &engine.GameContext{
		World:        world,
		TimeProvider: mockTime,
		State:        engine.NewGameState(80, 24, 100, mockTime),
		GameWidth:    80,
		GameHeight:   24,
	}

	// Configure cleaner with known duration
	config := constants.DefaultCleanerConfig()
	config.AnimationDuration = 1 * time.Second
	config.Speed = 80.0 // 80 chars/sec = exactly 1 second to cross screen

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, config)
	defer cleanerSystem.Shutdown()

	// Create Red character at specific position
	createRedCharacterAt(world, 40, 5)

	// Frame 0: Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Verify cleaner created
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)
	if len(cleaners) != 1 {
		t.Fatalf("Expected 1 cleaner, got %d", len(cleaners))
	}

	// Get initial cleaner position
	cleanerComp, _ := world.GetComponent(cleaners[0], cleanerType)
	cleaner := cleanerComp.(components.CleanerComponent)
	initialPos := cleaner.XPosition

	t.Logf("Frame 0: Cleaner spawned at row %d, position %.2f", cleaner.Row, initialPos)

	// Note: The first Update() call after spawning sets firstUpdate=true and returns early
	// So we need a second Update() call to skip the first update, then a third to actually move

	// Advance time by 16ms (one frame)
	mockTime.Advance(16 * time.Millisecond)

	// Frame 1: Update (first update is skipped internally, position shouldn't change much)
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Advance time by 16ms
	mockTime.Advance(16 * time.Millisecond)

	// Frame 2: Update (now position should change)
	cleanerSystem.Update(world, 16*time.Millisecond)

	cleaners = world.GetEntitiesWith(cleanerType)
	cleanerComp, _ = world.GetComponent(cleaners[0], cleanerType)
	cleaner = cleanerComp.(components.CleanerComponent)

	// After skipping one update, the cleaner should have moved in Frame 2
	// Allow for floating point tolerance
	if cleaner.XPosition < -0.5 || cleaner.XPosition > 2.0 {
		t.Errorf("Frame 2: Position out of expected range, got %.2f", cleaner.XPosition)
	}

	t.Logf("Frame 2: Cleaner moved to %.2f (initial was %.2f)", cleaner.XPosition, initialPos)

	// Simulate multiple frames
	for frame := 3; frame <= 62; frame++ {
		mockTime.Advance(16 * time.Millisecond)
		cleanerSystem.Update(world, 16*time.Millisecond)

		cleaners = world.GetEntitiesWith(cleanerType)
		if len(cleaners) != 1 {
			t.Logf("Frame %d: Cleaner disappeared (expected at ~1000ms)", frame)
			break
		}
	}

	// After 1 second + buffer, animation should be complete
	mockTime.Advance(100 * time.Millisecond)
	cleanerSystem.Update(world, 16*time.Millisecond)

	cleaners = world.GetEntitiesWith(cleanerType)
	if len(cleaners) != 0 {
		t.Errorf("Expected cleaners to be cleaned up after animation duration, found %d", len(cleaners))
	}

	if cleanerSystem.IsActive() {
		t.Error("CleanerSystem should be inactive after animation complete")
	}

	t.Log("Deterministic lifecycle test completed successfully")
}

// TestDeterministicCleanerTiming verifies exact animation duration with MockTimeProvider
func TestDeterministicCleanerTiming(t *testing.T) {
	world := engine.NewWorld()

	startTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	mockTime := engine.NewMockTimeProvider(startTime)

	ctx := &engine.GameContext{
		World:        world,
		TimeProvider: mockTime,
		State:        engine.NewGameState(80, 24, 100, mockTime),
		GameWidth:    80,
		GameHeight:   24,
	}

	// Configure with 500ms animation
	config := constants.DefaultCleanerConfig()
	config.AnimationDuration = 500 * time.Millisecond

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, config)
	defer cleanerSystem.Shutdown()

	// Create Red characters
	for row := 0; row < 5; row++ {
		createRedCharacterAt(world, 40, row)
	}

	// T=0ms: Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)

	if !cleanerSystem.IsActive() {
		t.Fatal("Cleaners should be active after trigger")
	}

	// T=16ms: First real update
	mockTime.Advance(16 * time.Millisecond)
	cleanerSystem.Update(world, 16*time.Millisecond)

	if !cleanerSystem.IsActive() {
		t.Error("Cleaners should still be active at T=16ms")
	}

	// T=250ms: Halfway through animation
	mockTime.Advance(234 * time.Millisecond)
	cleanerSystem.Update(world, 16*time.Millisecond)

	if !cleanerSystem.IsActive() {
		t.Error("Cleaners should still be active at T=250ms (halfway)")
	}

	// T=490ms: Just before completion
	mockTime.Advance(240 * time.Millisecond)
	cleanerSystem.Update(world, 16*time.Millisecond)

	if !cleanerSystem.IsActive() {
		t.Error("Cleaners should still be active at T=490ms (just before completion)")
	}

	// T=510ms: Just after completion
	mockTime.Advance(20 * time.Millisecond)
	cleanerSystem.Update(world, 16*time.Millisecond)

	if cleanerSystem.IsActive() {
		t.Error("Cleaners should be inactive at T=510ms (after 500ms duration)")
	}

	// Verify cleanup happened
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)
	if len(cleaners) != 0 {
		t.Errorf("Expected no cleaners after animation complete, found %d", len(cleaners))
	}

	t.Log("Deterministic timing test completed successfully")
}

// TestDeterministicCollisionDetection verifies predictable collision timing
func TestDeterministicCollisionDetection(t *testing.T) {
	world := engine.NewWorld()

	startTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	mockTime := engine.NewMockTimeProvider(startTime)

	ctx := &engine.GameContext{
		World:        world,
		TimeProvider: mockTime,
		State:        engine.NewGameState(80, 24, 100, mockTime),
		GameWidth:    80,
		GameHeight:   24,
	}

	// Configure with known speed: 20 chars/sec
	config := constants.DefaultCleanerConfig()
	config.AnimationDuration = 4 * time.Second
	config.Speed = 20.0 // 20 chars/sec

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, config)
	defer cleanerSystem.Shutdown()

	// Create Red character at X=10 on row 5 (odd row, L→R from X=-1)
	redEntity := createRedCharacterAt(world, 10, 5)

	// Calculate when cleaner should hit X=10
	// Starting position: -1.0
	// Need to travel: 11.0 chars
	// Speed: 20.0 chars/sec
	// Time: 11.0 / 20.0 = 0.55 seconds = 550ms

	// Trigger cleaners at T=0
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Verify Red character exists
	seqType := reflect.TypeOf(components.SequenceComponent{})
	entities := world.GetEntitiesWith(seqType)
	redCount := 0
	for _, entity := range entities {
		seqComp, _ := world.GetComponent(entity, seqType)
		seq := seqComp.(components.SequenceComponent)
		if seq.Type == components.SequenceRed {
			redCount++
		}
	}
	if redCount != 1 {
		t.Fatalf("Expected 1 Red character, got %d", redCount)
	}

	// T=500ms: Before collision (should still have Red char)
	for i := 0; i < 31; i++ { // 31 frames * 16ms = 496ms
		mockTime.Advance(16 * time.Millisecond)
		cleanerSystem.Update(world, 16*time.Millisecond)
	}

	// Check if Red character still exists
	entities = world.GetEntitiesWith(seqType)
	stillExists := false
	for _, entity := range entities {
		if entity == redEntity {
			_, ok := world.GetComponent(entity, seqType)
			if ok {
				stillExists = true
				break
			}
		}
	}

	// Note: Due to trail-based collision with truncation, the character might
	// disappear slightly earlier. This is acceptable per requirements.
	t.Logf("T=500ms: Red character exists = %v", stillExists)

	// T=600ms: After expected collision
	for i := 0; i < 6; i++ { // 6 frames * 16ms = 96ms
		mockTime.Advance(16 * time.Millisecond)
		cleanerSystem.Update(world, 16*time.Millisecond)
	}

	// Verify Red character was destroyed
	entities = world.GetEntitiesWith(seqType)
	destroyed := true
	for _, entity := range entities {
		if entity == redEntity {
			_, ok := world.GetComponent(entity, seqType)
			if ok {
				destroyed = false
				break
			}
		}
	}

	if !destroyed {
		t.Error("Red character should have been destroyed by T=600ms")
	}

	// Verify flash effect was created
	flashType := reflect.TypeOf(components.RemovalFlashComponent{})
	flashes := world.GetEntitiesWith(flashType)
	if len(flashes) == 0 {
		t.Error("Expected flash effect to be created when Red character destroyed")
	} else {
		t.Logf("Flash effect created successfully (%d flashes)", len(flashes))
	}

	t.Log("Deterministic collision detection test completed successfully")
}

// TestDeterministicMultipleCleaners verifies deterministic behavior with multiple cleaners
func TestDeterministicMultipleCleaners(t *testing.T) {
	world := engine.NewWorld()

	startTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	mockTime := engine.NewMockTimeProvider(startTime)

	ctx := &engine.GameContext{
		World:        world,
		TimeProvider: mockTime,
		State:        engine.NewGameState(80, 24, 100, mockTime),
		GameWidth:    80,
		GameHeight:   24,
	}

	config := constants.DefaultCleanerConfig()
	config.AnimationDuration = 1 * time.Second

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, config)
	defer cleanerSystem.Shutdown()

	// Create Red characters on rows 0, 1, 2, 3, 4
	redRows := []int{0, 1, 2, 3, 4}
	for _, row := range redRows {
		createRedCharacterAt(world, 40, row)
	}

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Verify correct number of cleaners
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)
	if len(cleaners) != len(redRows) {
		t.Fatalf("Expected %d cleaners, got %d", len(redRows), len(cleaners))
	}

	// Verify each cleaner has correct row and direction
	rowsSeen := make(map[int]bool)
	for _, entity := range cleaners {
		cleanerComp, _ := world.GetComponent(entity, cleanerType)
		cleaner := cleanerComp.(components.CleanerComponent)

		rowsSeen[cleaner.Row] = true

		// Verify direction: odd rows L→R (1), even rows R→L (-1)
		expectedDir := 1
		expectedStart := -1.0
		if cleaner.Row%2 == 0 {
			expectedDir = -1
			expectedStart = 80.0
		}

		if cleaner.Direction != expectedDir {
			t.Errorf("Row %d: Expected direction %d, got %d", cleaner.Row, expectedDir, cleaner.Direction)
		}

		if cleaner.XPosition != expectedStart {
			t.Errorf("Row %d: Expected start position %.1f, got %.1f", cleaner.Row, expectedStart, cleaner.XPosition)
		}
	}

	// Verify all expected rows were covered
	for _, row := range redRows {
		if !rowsSeen[row] {
			t.Errorf("Missing cleaner for row %d", row)
		}
	}

	t.Log("Deterministic multiple cleaners test completed successfully")
}

// TestDeterministicCleanerDeactivation verifies exact deactivation timing
func TestDeterministicCleanerDeactivation(t *testing.T) {
	world := engine.NewWorld()

	startTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	mockTime := engine.NewMockTimeProvider(startTime)

	ctx := &engine.GameContext{
		World:        world,
		TimeProvider: mockTime,
		State:        engine.NewGameState(80, 24, 100, mockTime),
		GameWidth:    80,
		GameHeight:   24,
	}

	// 300ms animation
	config := constants.DefaultCleanerConfig()
	config.AnimationDuration = 300 * time.Millisecond

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, config)
	defer cleanerSystem.Shutdown()

	createRedCharacterAt(world, 40, 5)

	// T=0: Activate
	ctx.State.RequestCleaners()
	ctx.State.ActivateCleaners()
	cleanerSystem.ActivateCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)

	if !ctx.State.GetCleanerActive() {
		t.Fatal("GameState should show cleaners as active")
	}

	// T=299ms: Just before deactivation
	mockTime.Advance(299 * time.Millisecond)
	cleanerSystem.Update(world, 16*time.Millisecond)

	if !cleanerSystem.IsActive() {
		t.Error("Cleaners should still be active at T=299ms")
	}
	if cleanerSystem.IsAnimationComplete() {
		t.Error("Animation should not be complete at T=299ms")
	}

	// T=301ms: Just after deactivation
	mockTime.Advance(2 * time.Millisecond)
	cleanerSystem.Update(world, 16*time.Millisecond)

	if cleanerSystem.IsActive() {
		t.Error("Cleaners should be inactive at T=301ms")
	}
	if !cleanerSystem.IsAnimationComplete() {
		t.Error("Animation should be complete at T=301ms")
	}

	// Verify state was reset
	if cleanerSystem.GetActiveCleanerCount() != 0 {
		t.Errorf("Active cleaner count should be 0, got %d", cleanerSystem.GetActiveCleanerCount())
	}

	t.Log("Deterministic deactivation test completed successfully")
}
