package systems

import (
	"reflect"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestCleanersRemovalFlashEffect verifies flash effects are created when red characters are removed
func TestCleanersRemovalFlashEffect(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Create a Red character at position (40, 5)
	redEntity := createRedCharacterAt(world, 40, 5)

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	// Get cleaner entity
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)
	if len(cleaners) != 1 {
		t.Fatalf("Expected 1 cleaner, got %d", len(cleaners))
	}

	cleanerEntity := cleaners[0]

	// Move cleaner to red character position
	cleanerComp, _ := world.GetComponent(cleanerEntity, cleanerType)
	cleaner := cleanerComp.(components.CleanerComponent)
	cleaner.XPosition = 40.0
	cleaner.TrailPositions = []float64{40.0}
	world.AddComponent(cleanerEntity, cleaner)

	// Verify red character exists before removal
	if !entityExists(world, redEntity) {
		t.Fatal("Red character should exist before cleaner contact")
	}

	// Check collisions via trail (should create flash)
	cleanerSystem.checkTrailCollisions(world, cleaner.Row, cleaner.TrailPositions)

	// Verify red character was destroyed
	if entityExists(world, redEntity) {
		t.Error("Red character should be destroyed after cleaner contact")
	}

	// Verify flash effect was created
	flashType := reflect.TypeOf(components.RemovalFlashComponent{})
	flashes := world.GetEntitiesWith(flashType)

	if len(flashes) != 1 {
		t.Fatalf("Expected 1 flash effect, got %d", len(flashes))
	}

	// Verify flash properties
	flashComp, ok := world.GetComponent(flashes[0], flashType)
	if !ok {
		t.Fatal("Failed to get flash component")
	}
	flash := flashComp.(components.RemovalFlashComponent)

	if flash.X != 40 || flash.Y != 5 {
		t.Errorf("Flash position should be (40, 5), got (%d, %d)", flash.X, flash.Y)
	}
	if flash.Char != 'R' {
		t.Errorf("Flash should preserve character 'R', got '%c'", flash.Char)
	}
	if flash.Duration != constants.RemovalFlashDuration {
		t.Errorf("Flash duration should be %d ms, got %d ms", constants.RemovalFlashDuration, flash.Duration)
	}
}

// TestCleanersFlashCleanup verifies flash effects are cleaned up after expiration
func TestCleanersFlashCleanup(t *testing.T) {
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

	// Create a Red character
	createRedCharacterAt(world, 40, 5)

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	// Move cleaner to red character position and trigger removal
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)
	if len(cleaners) == 0 {
		t.Fatal("No cleaners found")
	}

	cleanerEntity := cleaners[0]
	cleanerComp, _ := world.GetComponent(cleanerEntity, cleanerType)
	cleaner := cleanerComp.(components.CleanerComponent)
	cleaner.XPosition = 40.0
	cleaner.TrailPositions = []float64{40.0}
	world.AddComponent(cleanerEntity, cleaner)

	// Check collisions via trail (creates flash)
	cleanerSystem.checkTrailCollisions(world, cleaner.Row, cleaner.TrailPositions)

	// Verify flash was created
	flashType := reflect.TypeOf(components.RemovalFlashComponent{})
	flashes := world.GetEntitiesWith(flashType)
	if len(flashes) != 1 {
		t.Fatalf("Expected 1 flash, got %d", len(flashes))
	}

	// Advance time just before flash expires
	mockTime.Advance(time.Duration(constants.RemovalFlashDuration-10) * time.Millisecond)

	// Run cleanup - flash should still exist
	cleanerSystem.cleanupExpiredFlashes(world)

	flashes = world.GetEntitiesWith(flashType)
	if len(flashes) != 1 {
		t.Errorf("Flash should still exist before expiration, got %d flashes", len(flashes))
	}

	// Advance time past flash expiration
	mockTime.Advance(20 * time.Millisecond)

	// Run cleanup - flash should be removed
	cleanerSystem.cleanupExpiredFlashes(world)

	flashes = world.GetEntitiesWith(flashType)
	if len(flashes) != 0 {
		t.Errorf("Flash should be cleaned up after expiration, got %d flashes", len(flashes))
	}
}

// TestCleanersNoFlashForBlueGreen verifies flash is only created for red characters
func TestCleanersNoFlashForBlueGreen(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Create Red character first (so cleaner spawns), then Blue and Green on same row
	createRedCharacterAt(world, 50, 5) // Red at position 50
	createBlueCharacterAt(world, 40, 5)   // Blue at position 40
	createGreenCharacterAt(world, 41, 5)  // Green at position 41

	// Trigger cleaners (will spawn because Red exists)
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

	// Move cleaner across Blue and Green positions (avoiding Red at 50)
	for x := 40; x <= 41; x++ {
		cleanerComp, _ := world.GetComponent(cleanerEntity, cleanerType)
		cleaner := cleanerComp.(components.CleanerComponent)
		cleaner.XPosition = float64(x)
		cleaner.TrailPositions = []float64{float64(x)}
		world.AddComponent(cleanerEntity, cleaner)

		// Check collisions via trail
		cleanerSystem.checkTrailCollisions(world, cleaner.Row, cleaner.TrailPositions)
	}

	// Verify no flash effects were created (only for Red)
	flashType := reflect.TypeOf(components.RemovalFlashComponent{})
	flashes := world.GetEntitiesWith(flashType)

	if len(flashes) != 0 {
		t.Errorf("No flash effects should be created for Blue/Green, got %d", len(flashes))
	}

	// Verify Blue and Green still exist
	blueType := reflect.TypeOf(components.SequenceComponent{})
	entities := world.GetEntitiesWith(blueType)

	blueGreenCount := 0
	for _, entity := range entities {
		seqComp, ok := world.GetComponent(entity, blueType)
		if !ok {
			continue
		}
		seq := seqComp.(components.SequenceComponent)
		if seq.Type == components.SequenceBlue || seq.Type == components.SequenceGreen {
			blueGreenCount++
		}
	}

	if blueGreenCount != 2 {
		t.Errorf("Blue and Green should not be destroyed, expected 2, got %d", blueGreenCount)
	}
}

// TestCleanersMultipleFlashEffects verifies multiple flashes can exist simultaneously
func TestCleanersMultipleFlashEffects(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Create multiple Red characters on same row
	createRedCharacterAt(world, 10, 5)
	createRedCharacterAt(world, 20, 5)
	createRedCharacterAt(world, 30, 5)

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

	// Move cleaner across all red character positions
	for _, x := range []int{10, 20, 30} {
		cleanerComp, _ := world.GetComponent(cleanerEntity, cleanerType)
		cleaner := cleanerComp.(components.CleanerComponent)
		cleaner.XPosition = float64(x)
		cleaner.TrailPositions = []float64{float64(x)}
		world.AddComponent(cleanerEntity, cleaner)

		// Check collisions via trail
		cleanerSystem.checkTrailCollisions(world, cleaner.Row, cleaner.TrailPositions)
	}

	// Verify 3 flash effects were created
	flashType := reflect.TypeOf(components.RemovalFlashComponent{})
	flashes := world.GetEntitiesWith(flashType)

	if len(flashes) != 3 {
		t.Fatalf("Expected 3 flash effects, got %d", len(flashes))
	}

	// Verify each flash is at the correct position
	positions := make(map[int]bool)
	for _, flashEntity := range flashes {
		flashComp, _ := world.GetComponent(flashEntity, flashType)
		flash := flashComp.(components.RemovalFlashComponent)
		positions[flash.X] = true
	}

	for _, expectedX := range []int{10, 20, 30} {
		if !positions[expectedX] {
			t.Errorf("Expected flash at X=%d, but not found", expectedX)
		}
	}
}
