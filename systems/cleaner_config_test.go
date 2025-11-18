package systems

import (
	"reflect"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestCleanerConfigDefaults tests that default configuration is properly applied
func TestCleanerConfigDefaults(t *testing.T) {
	config := constants.DefaultCleanerConfig()

	// Verify default values
	if config.AnimationDuration != constants.CleanerAnimationDuration {
		t.Errorf("Expected AnimationDuration=%v, got %v", constants.CleanerAnimationDuration, config.AnimationDuration)
	}

	if config.Speed != 0 {
		t.Errorf("Expected Speed=0 (auto-calculate), got %v", config.Speed)
	}

	if config.TrailLength != constants.CleanerTrailLength {
		t.Errorf("Expected TrailLength=%d, got %d", constants.CleanerTrailLength, config.TrailLength)
	}

	if config.TrailFadeTime != constants.CleanerTrailFadeTime {
		t.Errorf("Expected TrailFadeTime=%v, got %v", constants.CleanerTrailFadeTime, config.TrailFadeTime)
	}

	if config.FadeCurve != constants.TrailFadeLinear {
		t.Errorf("Expected FadeCurve=TrailFadeLinear, got %v", config.FadeCurve)
	}

	if config.MaxConcurrentCleaners != 0 {
		t.Errorf("Expected MaxConcurrentCleaners=0 (unlimited), got %d", config.MaxConcurrentCleaners)
	}

	if config.ScanInterval != 0 {
		t.Errorf("Expected ScanInterval=0 (disabled), got %v", config.ScanInterval)
	}

	if config.FPS != constants.CleanerFPS {
		t.Errorf("Expected FPS=%d, got %d", constants.CleanerFPS, config.FPS)
	}

	if config.Char != constants.CleanerChar {
		t.Errorf("Expected Char=%c, got %c", constants.CleanerChar, config.Char)
	}

	if config.FlashDuration != constants.RemovalFlashDuration {
		t.Errorf("Expected FlashDuration=%d, got %d", constants.RemovalFlashDuration, config.FlashDuration)
	}
}

// TestCleanerConfigCustomSpeed tests that custom speed is used when specified
func TestCleanerConfigCustomSpeed(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	// Create config with custom speed
	config := constants.DefaultCleanerConfig()
	config.Speed = 200.0 // 200 characters per second

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, config)
	defer cleanerSystem.Shutdown()

	// Create Red characters
	for x := 10; x < 70; x += 10 {
		createRedCharacterAt(world, x, 5)
	}

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	// Check that cleaner was created with custom speed
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	if len(cleaners) == 0 {
		t.Fatal("No cleaners were created")
	}

	cleanerComp, ok := world.GetComponent(cleaners[0], cleanerType)
	if !ok {
		t.Fatal("Could not get cleaner component")
	}

	cleaner := cleanerComp.(components.CleanerComponent)
	if cleaner.Speed != 200.0 {
		t.Errorf("Expected Speed=200.0, got %v", cleaner.Speed)
	}
}

// TestCleanerConfigCustomTrailLength tests that custom trail length is used
func TestCleanerConfigCustomTrailLength(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	// Create config with custom trail length
	config := constants.DefaultCleanerConfig()
	config.TrailLength = 5

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, config)
	defer cleanerSystem.Shutdown()

	// Create Red characters
	for x := 10; x < 70; x += 10 {
		createRedCharacterAt(world, x, 5)
	}

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(200 * time.Millisecond)

	// Check that trail length is limited to config value
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	if len(cleaners) == 0 {
		t.Fatal("No cleaners were created")
	}

	cleanerComp, ok := world.GetComponent(cleaners[0], cleanerType)
	if !ok {
		t.Fatal("Could not get cleaner component")
	}

	cleaner := cleanerComp.(components.CleanerComponent)
	if len(cleaner.TrailPositions) > 5 {
		t.Errorf("Expected TrailPositions length <= 5, got %d", len(cleaner.TrailPositions))
	}
}

// TestCleanerConfigMaxConcurrentCleaners tests that max concurrent cleaners limit is enforced
func TestCleanerConfigMaxConcurrentCleaners(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	// Create config with max concurrent cleaners limit
	config := constants.DefaultCleanerConfig()
	config.MaxConcurrentCleaners = 3

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, config)
	defer cleanerSystem.Shutdown()

	// Create Red characters on 10 different rows
	for row := 0; row < 10; row++ {
		for x := 10; x < 70; x += 10 {
			createRedCharacterAt(world, x, row)
		}
	}

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	// Check that only 3 cleaners were created
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	if len(cleaners) != 3 {
		t.Errorf("Expected 3 cleaners (max concurrent), got %d", len(cleaners))
	}
}

// TestCleanerConfigCustomChar tests that custom character is used for rendering
func TestCleanerConfigCustomChar(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	// Create config with custom character
	config := constants.DefaultCleanerConfig()
	config.Char = '▓' // Different block character

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, config)
	defer cleanerSystem.Shutdown()

	// Create Red characters
	for x := 10; x < 70; x += 10 {
		createRedCharacterAt(world, x, 5)
	}

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	// Check that cleaner was created with custom char
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	if len(cleaners) == 0 {
		t.Fatal("No cleaners were created")
	}

	cleanerComp, ok := world.GetComponent(cleaners[0], cleanerType)
	if !ok {
		t.Fatal("Could not get cleaner component")
	}

	cleaner := cleanerComp.(components.CleanerComponent)
	if cleaner.Char != '▓' {
		t.Errorf("Expected Char='▓', got %c", cleaner.Char)
	}
}

// TestCleanerConfigTrailFadeTime tests that custom trail fade time is used
func TestCleanerConfigTrailFadeTime(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	// Create config with custom trail fade time
	config := constants.DefaultCleanerConfig()
	config.TrailFadeTime = 0.5 // 500ms fade

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, config)
	defer cleanerSystem.Shutdown()

	// Create Red characters
	for x := 10; x < 70; x += 10 {
		createRedCharacterAt(world, x, 5)
	}

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	// Check that cleaner was created with custom fade time
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	if len(cleaners) == 0 {
		t.Fatal("No cleaners were created")
	}

	cleanerComp, ok := world.GetComponent(cleaners[0], cleanerType)
	if !ok {
		t.Fatal("Could not get cleaner component")
	}

	cleaner := cleanerComp.(components.CleanerComponent)
	if cleaner.TrailMaxAge != 0.5 {
		t.Errorf("Expected TrailMaxAge=0.5, got %v", cleaner.TrailMaxAge)
	}
}

// TestCleanerConfigAnimationDuration tests that config animation duration is properly set
func TestCleanerConfigAnimationDuration(t *testing.T) {
	ctx := createCleanerTestContext()

	// Create config with shorter animation duration
	config := constants.DefaultCleanerConfig()
	config.AnimationDuration = 500 * time.Millisecond

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, config)
	defer cleanerSystem.Shutdown()

	// Verify that the config was properly set
	cleanerSystem.mu.RLock()
	duration := cleanerSystem.animationDuration
	cleanerSystem.mu.RUnlock()

	if duration != 500*time.Millisecond {
		t.Errorf("Expected animationDuration=500ms, got %v", duration)
	}
}
