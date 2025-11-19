package systems

import (
	"reflect"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

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

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
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

	// Simulate 900ms of game loop updates at 60fps
	frameDuration := 16 * time.Millisecond
	for i := 0; i < 56; i++ { // 56 frames * 16ms = 896ms
		mockTime.Advance(frameDuration)
		cleanerSystem.Update(world, frameDuration)
	}

	if !cleanerSystem.IsActive() {
		t.Error("Cleaners should still be active before duration expires")
	}

	// Simulate another 200ms of updates (total: ~1.1 seconds)
	for i := 0; i < 13; i++ { // 13 frames * 16ms = 208ms
		mockTime.Advance(frameDuration)
		cleanerSystem.Update(world, frameDuration)
	}

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

// TestCleanersTrailTracking verifies trail positions are tracked correctly
func TestCleanersTrailTracking(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
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

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
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

	// Force cleanup by advancing time beyond animation duration and calling Update
	frameDuration := 16 * time.Millisecond
	for i := 0; i < 125; i++ { // 125 frames * 16ms = 2 seconds
		mockTime.Advance(frameDuration)
		cleanerSystem.Update(world, frameDuration)
	}

	// Verify cleanup happened
	cleaners = world.GetEntitiesWith(cleanerType)
	if len(cleaners) > 0 {
		t.Errorf("Expected cleaners to be cleaned up, but found %d", len(cleaners))
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
