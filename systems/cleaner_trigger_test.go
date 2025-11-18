package systems

import (
	"reflect"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestGoldCompletionWithMaxHeatTriggersCleaners verifies that completing
// a gold sequence with max heat triggers the cleaner system
func TestGoldCompletionWithMaxHeatTriggersCleaners(t *testing.T) {
	// Create test context with mock time provider
	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx := createTestContextForCleanerTriggerTests(mockTime, 80, 24)
	world := ctx.World

	// Create systems
	scoreSystem := NewScoreSystem(ctx)
	goldSystem := NewGoldSequenceSystem(ctx, nil, ctx.GameWidth, ctx.GameHeight, ctx.CursorX, ctx.CursorY)
	cleanerConfig := constants.DefaultCleanerConfig()
	cleanerConfig.AnimationDuration = 500 * time.Millisecond
	cleanerSystem := NewCleanerSystem(ctx, ctx.GameWidth, ctx.GameHeight, cleanerConfig)

	// Wire up systems
	scoreSystem.SetGoldSequenceSystem(goldSystem)
	goldSystem.SetCleanerTrigger(cleanerSystem.TriggerCleaners)

	// Create some Red characters on screen
	createRedCharacterAt(world, 10, 5)
	createRedCharacterAt(world, 15, 5)
	createRedCharacterAt(world, 20, 5)

	// Set heat to maximum
	maxHeat := ctx.Width - constants.HeatBarIndicatorWidth
	ctx.SetScoreIncrement(maxHeat)

	// Create a gold sequence manually
	goldSeqID := 1
	goldChars := "test123456"
	createGoldSequence(world, goldSeqID, goldChars, 30, 10)

	// Simulate typing the gold sequence
	cursorX, cursorY := 30, 10
	for i, ch := range goldChars {
		ctx.CursorX = cursorX + i
		ctx.CursorY = cursorY

		// Type the character
		scoreSystem.HandleCharacterTyping(world, ctx.CursorX, ctx.CursorY, ch)
	}

	// Process cleaner spawn request
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Verify cleaners were triggered
	if !cleanerSystem.IsActive() {
		t.Errorf("Expected cleaners to be active after gold completion with max heat")
	}

	// Verify cleaner entities were spawned
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)
	if len(cleaners) == 0 {
		t.Errorf("Expected cleaner entities to be spawned, got 0")
	}

	t.Logf("Test passed: Gold completion with max heat triggered %d cleaners", len(cleaners))
}

// TestCleanersSpawnOnRowsWithRedCharacters verifies that cleaners
// only spawn on rows that contain Red characters
func TestCleanersSpawnOnRowsWithRedCharacters(t *testing.T) {
	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx := createTestContextForCleanerTriggerTests(mockTime, 80, 24)
	world := ctx.World

	cleanerConfig := constants.DefaultCleanerConfig()
	cleanerConfig.AnimationDuration = 500 * time.Millisecond
	cleanerSystem := NewCleanerSystem(ctx, ctx.GameWidth, ctx.GameHeight, cleanerConfig)

	// Create Red characters on specific rows
	createRedCharacterAt(world, 10, 5)  // Row 5
	createRedCharacterAt(world, 20, 5)  // Row 5
	createRedCharacterAt(world, 15, 10) // Row 10

	// Create some Blue/Green characters on other rows (should not trigger cleaners)
	createBlueCharacterAt(world, 10, 7)
	createGreenCharacterAt(world, 20, 12)

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)

	// Process spawn request
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Verify cleaners were spawned
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	if len(cleaners) != 2 {
		t.Errorf("Expected 2 cleaners (rows 5 and 10), got %d", len(cleaners))
	}

	// Verify cleaner rows
	rowSet := make(map[int]bool)
	for _, entity := range cleaners {
		comp, ok := world.GetComponent(entity, cleanerType)
		if !ok {
			continue
		}
		cleaner := comp.(components.CleanerComponent)
		rowSet[cleaner.Row] = true
	}

	if !rowSet[5] || !rowSet[10] {
		t.Errorf("Expected cleaners on rows 5 and 10, got rows: %v", rowSet)
	}

	t.Logf("Test passed: Cleaners spawned on correct rows: %v", rowSet)
}

// TestCleanersMoveAndDestroyRedCharacters verifies that cleaners
// move across the screen and destroy Red characters on contact
func TestCleanersMoveAndDestroyRedCharacters(t *testing.T) {
	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx := createTestContextForCleanerTriggerTests(mockTime, 80, 24)
	world := ctx.World

	cleanerConfig := constants.DefaultCleanerConfig()
	cleanerConfig.AnimationDuration = 1 * time.Second
	cleanerConfig.FPS = 60
	cleanerSystem := NewCleanerSystem(ctx, ctx.GameWidth, ctx.GameHeight, cleanerConfig)

	// Create Red characters at specific positions on row 5
	redEntity1 := createRedCharacterAt(world, 10, 5)
	redEntity2 := createRedCharacterAt(world, 20, 5)
	redEntity3 := createRedCharacterAt(world, 30, 5)

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Verify cleaners were spawned
	if !cleanerSystem.IsActive() {
		t.Fatal("Cleaners should be active")
	}

	// Simulate animation by advancing time
	// Cleaner on odd row (5) moves L→R starting at x=-1
	for i := 0; i < 100; i++ {
		mockTime.Advance(16 * time.Millisecond)
		cleanerSystem.Update(world, 16*time.Millisecond)

		// Check if Red characters are destroyed
		if i > 20 { // Give some time for cleaner to reach first character
			// Check periodically if characters are being destroyed
			if world.GetEntityAtPosition(10, 5) == 0 && redEntity1 != 0 {
				t.Logf("Red character at (10,5) destroyed at iteration %d", i)
				redEntity1 = 0
			}
			if world.GetEntityAtPosition(20, 5) == 0 && redEntity2 != 0 {
				t.Logf("Red character at (20,5) destroyed at iteration %d", i)
				redEntity2 = 0
			}
			if world.GetEntityAtPosition(30, 5) == 0 && redEntity3 != 0 {
				t.Logf("Red character at (30,5) destroyed at iteration %d", i)
				redEntity3 = 0
			}
		}
	}

	// Verify all Red characters were destroyed
	if world.GetEntityAtPosition(10, 5) != 0 {
		t.Errorf("Red character at (10,5) should be destroyed")
	}
	if world.GetEntityAtPosition(20, 5) != 0 {
		t.Errorf("Red character at (20,5) should be destroyed")
	}
	if world.GetEntityAtPosition(30, 5) != 0 {
		t.Errorf("Red character at (30,5) should be destroyed")
	}

	t.Logf("Test passed: Cleaners destroyed all Red characters")
}

// TestCleanersDontAffectBlueGreenCharacters verifies that cleaners
// only destroy Red characters and leave Blue/Green characters intact
func TestCleanersDontAffectBlueGreenCharacters(t *testing.T) {
	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx := createTestContextForCleanerTriggerTests(mockTime, 80, 24)
	world := ctx.World

	cleanerConfig := constants.DefaultCleanerConfig()
	cleanerConfig.AnimationDuration = 1 * time.Second
	cleanerConfig.FPS = 60
	cleanerSystem := NewCleanerSystem(ctx, ctx.GameWidth, ctx.GameHeight, cleanerConfig)

	// Create mixed characters on row 5
	blueEntity := createBlueCharacterAt(world, 10, 5)
	redEntity := createRedCharacterAt(world, 20, 5)
	greenEntity := createGreenCharacterAt(world, 30, 5)

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Simulate animation
	for i := 0; i < 100; i++ {
		mockTime.Advance(16 * time.Millisecond)
		cleanerSystem.Update(world, 16*time.Millisecond)
	}

	// Verify Blue and Green characters still exist
	if world.GetEntityAtPosition(10, 5) != blueEntity {
		t.Errorf("Blue character at (10,5) should NOT be destroyed")
	}
	if world.GetEntityAtPosition(30, 5) != greenEntity {
		t.Errorf("Green character at (30,5) should NOT be destroyed")
	}

	// Verify Red character was destroyed
	if world.GetEntityAtPosition(20, 5) == redEntity {
		t.Errorf("Red character at (20,5) SHOULD be destroyed")
	}

	t.Logf("Test passed: Cleaners only destroyed Red characters")
}

// TestCleanerDeactivationAfterDuration verifies that cleaners
// deactivate and are cleaned up after the animation duration
func TestCleanerDeactivationAfterDuration(t *testing.T) {
	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx := createTestContextForCleanerTriggerTests(mockTime, 80, 24)
	world := ctx.World

	cleanerConfig := constants.DefaultCleanerConfig()
	cleanerConfig.AnimationDuration = 500 * time.Millisecond
	cleanerConfig.FPS = 60
	cleanerSystem := NewCleanerSystem(ctx, ctx.GameWidth, ctx.GameHeight, cleanerConfig)

	// Create Red characters
	createRedCharacterAt(world, 10, 5)

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Verify cleaners are active
	if !cleanerSystem.IsActive() {
		t.Fatal("Cleaners should be active after spawn")
	}

	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	initialCleaners := len(world.GetEntitiesWith(cleanerType))
	if initialCleaners == 0 {
		t.Fatal("Expected cleaner entities to exist")
	}

	// Advance time past the animation duration
	mockTime.Advance(600 * time.Millisecond)

	// Update cleaners multiple times to ensure cleanup
	for i := 0; i < 10; i++ {
		cleanerSystem.Update(world, 16*time.Millisecond)
		mockTime.Advance(16 * time.Millisecond)
	}

	// Verify cleaners are deactivated
	if cleanerSystem.IsActive() {
		t.Errorf("Cleaners should be deactivated after animation duration")
	}

	// Verify cleaner entities are cleaned up
	remainingCleaners := len(world.GetEntitiesWith(cleanerType))
	if remainingCleaners != 0 {
		t.Errorf("Expected 0 cleaner entities after cleanup, got %d", remainingCleaners)
	}

	t.Logf("Test passed: Cleaners deactivated after %v", cleanerConfig.AnimationDuration)
}

// TestNoCleanersWhenNoRedCharacters verifies that cleaners
// are not spawned if there are no Red characters on screen
func TestNoCleanersWhenNoRedCharacters(t *testing.T) {
	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx := createTestContextForCleanerTriggerTests(mockTime, 80, 24)
	world := ctx.World

	cleanerConfig := constants.DefaultCleanerConfig()
	cleanerSystem := NewCleanerSystem(ctx, ctx.GameWidth, ctx.GameHeight, cleanerConfig)

	// Create only Blue and Green characters (no Red)
	createBlueCharacterAt(world, 10, 5)
	createGreenCharacterAt(world, 20, 10)

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Verify cleaners were NOT activated
	if cleanerSystem.IsActive() {
		t.Errorf("Cleaners should NOT be active when no Red characters exist")
	}

	// Verify no cleaner entities were spawned
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)
	if len(cleaners) != 0 {
		t.Errorf("Expected 0 cleaner entities, got %d", len(cleaners))
	}

	t.Logf("Test passed: No cleaners spawned when no Red characters present")
}

// Helper functions

func createTestContextForCleanerTriggerTests(timeProvider engine.TimeProvider, gameWidth, gameHeight int) *engine.GameContext {
	ctx := &engine.GameContext{
		World:        engine.NewWorld(),
		TimeProvider: timeProvider,
		Width:        gameWidth + 10,
		Height:       gameHeight,
		GameWidth:    gameWidth,
		GameHeight:   gameHeight,
		CursorX:      0,
		CursorY:      0,
	}
	ctx.SetScore(0)
	ctx.SetScoreIncrement(0)
	return ctx
}

func createGoldSequence(world *engine.World, seqID int, chars string, startX, startY int) {
	for i, ch := range chars {
		entity := world.CreateEntity()
		world.AddComponent(entity, components.PositionComponent{X: startX + i, Y: startY})
		world.AddComponent(entity, components.CharacterComponent{Rune: ch})
		world.AddComponent(entity, components.SequenceComponent{
			ID:    seqID,
			Index: i,
			Type:  components.SequenceGold,
			Level: components.LevelBright,
		})
		world.UpdateSpatialIndex(entity, startX+i, startY)
	}
}
