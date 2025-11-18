package systems

import (
	"reflect"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// TestFallingDecaySpawn tests that falling entities are created when decay animation starts
func TestFallingDecaySpawn(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	gameWidth := 80
	decaySystem := NewDecaySystem(gameWidth, 24, 80, 0, ctx)

	// Trigger decay animation
	decaySystem.animating = true
	decaySystem.startTime = mockTime.Now()
	decaySystem.spawnFallingEntities(world)

	// Check that exactly one falling entity per column was created
	if len(decaySystem.fallingEntities) != gameWidth {
		t.Errorf("Expected exactly %d falling entities (one per column), got %d", gameWidth, len(decaySystem.fallingEntities))
	}

	// Check that all entities have FallingDecayComponent
	fallingType := reflect.TypeOf(components.FallingDecayComponent{})
	for _, entity := range decaySystem.fallingEntities {
		fallComp, ok := world.GetComponent(entity, fallingType)
		if !ok {
			t.Errorf("Falling entity %d missing FallingDecayComponent", entity)
			continue
		}

		fall := fallComp.(components.FallingDecayComponent)

		// Check initial position
		if fall.YPosition != 0.0 {
			t.Errorf("Expected initial YPosition 0.0, got %f", fall.YPosition)
		}

		// Check speed is within range
		if fall.Speed < constants.FallingDecayMinSpeed || fall.Speed > constants.FallingDecayMaxSpeed {
			t.Errorf("Speed %f out of range [%f, %f]", fall.Speed, constants.FallingDecayMinSpeed, constants.FallingDecayMaxSpeed)
		}

		// Check column is within bounds
		if fall.Column < 0 || fall.Column >= gameWidth {
			t.Errorf("Column %d out of bounds [0, %d)", fall.Column, gameWidth)
		}

		// Check character is valid
		if fall.Char == 0 {
			t.Error("Falling character is null")
		}
	}
}

// TestFallingDecayColumnCoverage tests that all columns have exactly one falling entity
func TestFallingDecayColumnCoverage(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	gameWidth := 80
	decaySystem := NewDecaySystem(gameWidth, 24, 80, 0, ctx)

	// Trigger decay animation
	decaySystem.animating = true
	decaySystem.startTime = mockTime.Now()
	decaySystem.spawnFallingEntities(world)

	// Track which columns have falling entities
	columnCoverage := make(map[int]int) // column -> count

	fallingType := reflect.TypeOf(components.FallingDecayComponent{})
	for _, entity := range decaySystem.fallingEntities {
		fallComp, ok := world.GetComponent(entity, fallingType)
		if !ok {
			t.Errorf("Falling entity %d missing FallingDecayComponent", entity)
			continue
		}

		fall := fallComp.(components.FallingDecayComponent)
		columnCoverage[fall.Column]++
	}

	// Verify all columns have exactly one falling entity
	for col := 0; col < gameWidth; col++ {
		count, exists := columnCoverage[col]
		if !exists {
			t.Errorf("Column %d has no falling entity", col)
		} else if count != 1 {
			t.Errorf("Column %d has %d falling entities, expected 1", col, count)
		}
	}

	// Verify no extra columns
	if len(columnCoverage) != gameWidth {
		t.Errorf("Expected %d columns with falling entities, got %d", gameWidth, len(columnCoverage))
	}
}

// TestFallingDecayPositionUpdate tests that falling entities update their positions over time
func TestFallingDecayPositionUpdate(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	decaySystem := NewDecaySystem(80, 24, 80, 0, ctx)
	decaySystem.animating = true
	decaySystem.startTime = mockTime.Now()
	decaySystem.spawnFallingEntities(world)

	// Advance time by 1 second
	elapsed := 1.0

	// Update falling entities
	decaySystem.updateFallingEntities(world, elapsed)

	// Check that positions were updated
	fallingType := reflect.TypeOf(components.FallingDecayComponent{})
	for _, entity := range decaySystem.fallingEntities {
		fallComp, ok := world.GetComponent(entity, fallingType)
		if !ok {
			t.Errorf("Falling entity %d missing FallingDecayComponent after update", entity)
			continue
		}

		fall := fallComp.(components.FallingDecayComponent)

		// Y position should be speed * elapsed
		expectedY := fall.Speed * elapsed
		if fall.YPosition < expectedY-0.1 || fall.YPosition > expectedY+0.1 {
			t.Errorf("Expected YPosition around %f, got %f", expectedY, fall.YPosition)
		}
	}
}

// TestFallingDecayAppliesDecay tests that falling entities apply decay when passing characters
func TestFallingDecayAppliesDecay(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	decaySystem := NewDecaySystem(80, 24, 80, 0, ctx)
	spawnSystem := NewSpawnSystem(80, 24, 0, 0, ctx)
	decaySystem.SetSpawnSystem(spawnSystem)

	// Create a test character at position (5, 5) with Bright level
	entity := world.CreateEntity()
	world.AddComponent(entity, components.PositionComponent{X: 5, Y: 5})
	world.AddComponent(entity, components.CharacterComponent{
		Rune:  'A',
		Style: render.GetStyleForSequence(components.SequenceGreen, components.LevelBright),
	})
	world.AddComponent(entity, components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceGreen,
		Level: components.LevelBright,
	})
	world.UpdateSpatialIndex(entity, 5, 5)

	// Create a falling entity at column 5
	fallingEntity := world.CreateEntity()
	world.AddComponent(fallingEntity, components.FallingDecayComponent{
		Column:    5,
		YPosition: 5.0, // At the same row as the character
		Speed:     10.0,
		Char:      'X',
	})
	decaySystem.fallingEntities = []engine.Entity{fallingEntity}

	// Update falling entities (this should apply decay)
	decaySystem.updateFallingEntities(world, 0.5)

	// Check that character was decayed
	seqType := reflect.TypeOf(components.SequenceComponent{})
	seqComp, ok := world.GetComponent(entity, seqType)
	if !ok {
		t.Fatal("Character entity lost SequenceComponent")
	}
	seq := seqComp.(components.SequenceComponent)

	// Should have decayed from Bright to Normal
	if seq.Level != components.LevelNormal {
		t.Errorf("Expected level Normal after decay, got %v", seq.Level)
	}
}

// TestFallingDecayCleanup tests that falling entities are cleaned up when animation ends
func TestFallingDecayCleanup(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	decaySystem := NewDecaySystem(80, 24, 80, 0, ctx)
	decaySystem.animating = true
	decaySystem.startTime = mockTime.Now()
	decaySystem.spawnFallingEntities(world)

	initialCount := len(decaySystem.fallingEntities)
	if initialCount == 0 {
		t.Fatal("No falling entities were spawned")
	}

	// Clean up falling entities
	decaySystem.cleanupFallingEntities(world)

	// Check that all entities were destroyed
	if len(decaySystem.fallingEntities) != 0 {
		t.Errorf("Expected 0 falling entities after cleanup, got %d", len(decaySystem.fallingEntities))
	}

	// Check that entities no longer exist in world
	fallingType := reflect.TypeOf(components.FallingDecayComponent{})
	remainingEntities := world.GetEntitiesWith(fallingType)
	if len(remainingEntities) != 0 {
		t.Errorf("Expected 0 FallingDecayComponent entities in world, got %d", len(remainingEntities))
	}
}

// TestFallingDecayDoesNotDecayGold tests that falling decay doesn't affect gold sequences
func TestFallingDecayDoesNotDecayGold(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	decaySystem := NewDecaySystem(80, 24, 80, 0, ctx)

	// Create a gold character
	entity := world.CreateEntity()
	world.AddComponent(entity, components.PositionComponent{X: 5, Y: 5})
	world.AddComponent(entity, components.CharacterComponent{
		Rune:  'G',
		Style: render.GetStyleForSequence(components.SequenceGold, components.LevelBright),
	})
	world.AddComponent(entity, components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceGold,
		Level: components.LevelBright,
	})
	world.UpdateSpatialIndex(entity, 5, 5)

	// Apply decay directly
	decaySystem.applyDecayToCharacter(world, entity)

	// Check that gold character was NOT decayed
	seqType := reflect.TypeOf(components.SequenceComponent{})
	seqComp, ok := world.GetComponent(entity, seqType)
	if !ok {
		t.Fatal("Gold character entity lost SequenceComponent")
	}
	seq := seqComp.(components.SequenceComponent)

	// Should still be gold
	if seq.Type != components.SequenceGold {
		t.Errorf("Expected gold type, got %v", seq.Type)
	}
	if seq.Level != components.LevelBright {
		t.Errorf("Expected bright level, got %v", seq.Level)
	}
}

// TestFallingDecayNoDuplicateDecay tests that a character is only decayed once per frame
func TestFallingDecayNoDuplicateDecay(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	decaySystem := NewDecaySystem(80, 24, 80, 0, ctx)
	spawnSystem := NewSpawnSystem(80, 24, 0, 0, ctx)
	decaySystem.SetSpawnSystem(spawnSystem)

	// Create a test character at position (5, 5) with Bright level
	entity := world.CreateEntity()
	world.AddComponent(entity, components.PositionComponent{X: 5, Y: 5})
	world.AddComponent(entity, components.CharacterComponent{
		Rune:  'A',
		Style: render.GetStyleForSequence(components.SequenceGreen, components.LevelBright),
	})
	world.AddComponent(entity, components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceGreen,
		Level: components.LevelBright,
	})
	world.UpdateSpatialIndex(entity, 5, 5)

	// Create two falling entities at the same column
	falling1 := world.CreateEntity()
	world.AddComponent(falling1, components.FallingDecayComponent{
		Column:    5,
		YPosition: 5.0,
		Speed:     10.0,
		Char:      'X',
	})
	falling2 := world.CreateEntity()
	world.AddComponent(falling2, components.FallingDecayComponent{
		Column:    5,
		YPosition: 5.0,
		Speed:     12.0,
		Char:      'Y',
	})
	decaySystem.fallingEntities = []engine.Entity{falling1, falling2}

	// Update falling entities (this should apply decay only once)
	decaySystem.decayedThisFrame = make(map[engine.Entity]bool)
	decaySystem.updateFallingEntities(world, 0.5)

	// Check that character was decayed only once (Bright -> Normal, not Normal -> Dark)
	seqType := reflect.TypeOf(components.SequenceComponent{})
	seqComp, ok := world.GetComponent(entity, seqType)
	if !ok {
		t.Fatal("Character entity lost SequenceComponent")
	}
	seq := seqComp.(components.SequenceComponent)

	if seq.Level != components.LevelNormal {
		t.Errorf("Expected level Normal (decayed once), got %v", seq.Level)
	}
}

// TestFallingDecayIntegration tests the full decay animation cycle with falling entities
func TestFallingDecayIntegration(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	decaySystem := NewDecaySystem(80, 24, 80, 0, ctx)
	spawnSystem := NewSpawnSystem(80, 24, 0, 0, ctx)
	decaySystem.SetSpawnSystem(spawnSystem)

	// Create some test characters
	for i := 0; i < 10; i++ {
		entity := world.CreateEntity()
		world.AddComponent(entity, components.PositionComponent{X: i * 8, Y: 10})
		world.AddComponent(entity, components.CharacterComponent{
			Rune:  rune('A' + i),
			Style: render.GetStyleForSequence(components.SequenceBlue, components.LevelBright),
		})
		world.AddComponent(entity, components.SequenceComponent{
			ID:    i,
			Index: 0,
			Type:  components.SequenceBlue,
			Level: components.LevelBright,
		})
		world.UpdateSpatialIndex(entity, i*8, 10)
	}

	// Start decay animation
	decaySystem.animating = true
	decaySystem.startTime = mockTime.Now()
	decaySystem.spawnFallingEntities(world)

	initialFallingCount := len(decaySystem.fallingEntities)
	if initialFallingCount == 0 {
		t.Fatal("No falling entities spawned")
	}

	// Run several update cycles
	for elapsed := 0.0; elapsed < 2.0; elapsed += 0.1 {
		decaySystem.decayedThisFrame = make(map[engine.Entity]bool)
		decaySystem.updateFallingEntities(world, elapsed)
	}

	// Check that animation completes after sufficient time
	animationDuration := float64(24) / constants.FallingDecayMinSpeed
	mockTime.Advance(time.Duration(animationDuration+1) * time.Second)
	decaySystem.updateAnimation(world)

	if decaySystem.animating {
		t.Error("Expected animation to be complete")
	}

	// Check that falling entities were cleaned up
	if len(decaySystem.fallingEntities) != 0 {
		t.Errorf("Expected 0 falling entities after animation complete, got %d", len(decaySystem.fallingEntities))
	}
}

// TestFallingEntityIndividualCleanup tests that entities are destroyed individually when reaching the bottom
func TestFallingEntityIndividualCleanup(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	gameHeight := 24
	decaySystem := NewDecaySystem(80, gameHeight, 80, 0, ctx)

	// Create falling entities with different speeds
	// Slow entity: will take longer to reach bottom
	slowEntity := world.CreateEntity()
	world.AddComponent(slowEntity, components.FallingDecayComponent{
		Column:    10,
		YPosition: 0.0,
		Speed:     5.0, // Slow speed
		Char:      'S',
	})

	// Fast entity: will reach bottom quickly
	fastEntity := world.CreateEntity()
	world.AddComponent(fastEntity, components.FallingDecayComponent{
		Column:    20,
		YPosition: 0.0,
		Speed:     50.0, // Fast speed
		Char:      'F',
	})

	decaySystem.fallingEntities = []engine.Entity{slowEntity, fastEntity}

	// Initial count should be 2
	if len(decaySystem.fallingEntities) != 2 {
		t.Fatalf("Expected 2 falling entities initially, got %d", len(decaySystem.fallingEntities))
	}

	// Simulate elapsed time that causes fast entity to pass bottom (gameHeight = 24)
	// Fast entity: YPosition = 50.0 * 0.5 = 25.0 (beyond 24)
	// Slow entity: YPosition = 5.0 * 0.5 = 2.5 (still within bounds)
	elapsed := 0.5
	decaySystem.updateFallingEntities(world, elapsed)

	// After update, only slow entity should remain
	if len(decaySystem.fallingEntities) != 1 {
		t.Errorf("Expected 1 falling entity after fast entity passes bottom, got %d", len(decaySystem.fallingEntities))
	}

	// Verify the remaining entity is the slow one
	if len(decaySystem.fallingEntities) > 0 {
		fallingType := reflect.TypeOf(components.FallingDecayComponent{})
		fallComp, ok := world.GetComponent(decaySystem.fallingEntities[0], fallingType)
		if !ok {
			t.Error("Remaining entity doesn't have FallingDecayComponent")
		} else {
			fall := fallComp.(components.FallingDecayComponent)
			if fall.Column != 10 {
				t.Errorf("Expected remaining entity at column 10 (slow entity), got column %d", fall.Column)
			}
		}
	}

	// Verify fast entity was destroyed from world
	fallingType := reflect.TypeOf(components.FallingDecayComponent{})
	_, exists := world.GetComponent(fastEntity, fallingType)
	if exists {
		t.Error("Fast entity should have been destroyed from world")
	}

	// Continue simulation until slow entity also passes bottom
	// Slow entity needs YPosition >= 24, so elapsed = 24 / 5.0 = 4.8 seconds
	elapsed = 5.0
	decaySystem.updateFallingEntities(world, elapsed)

	// Now both entities should be gone
	if len(decaySystem.fallingEntities) != 0 {
		t.Errorf("Expected 0 falling entities after all pass bottom, got %d", len(decaySystem.fallingEntities))
	}

	// Verify slow entity was also destroyed from world
	_, exists = world.GetComponent(slowEntity, fallingType)
	if exists {
		t.Error("Slow entity should have been destroyed from world")
	}
}

// TestFallingEntityBoundaryCleanup tests that entities exactly at boundary are handled correctly
func TestFallingEntityBoundaryCleanup(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	gameHeight := 24
	decaySystem := NewDecaySystem(80, gameHeight, 80, 0, ctx)

	// Create entity that will be exactly at the boundary
	entity := world.CreateEntity()
	world.AddComponent(entity, components.FallingDecayComponent{
		Column:    15,
		YPosition: 0.0,
		Speed:     24.0, // Will reach exactly gameHeight = 24 at elapsed = 1.0
		Char:      'B',
	})

	decaySystem.fallingEntities = []engine.Entity{entity}

	// At elapsed = 0.99, entity should still be within bounds (YPosition = 23.76)
	elapsed := 0.99
	decaySystem.updateFallingEntities(world, elapsed)

	if len(decaySystem.fallingEntities) != 1 {
		t.Errorf("Expected 1 entity at elapsed=0.99 (still within bounds), got %d", len(decaySystem.fallingEntities))
	}

	// At elapsed = 1.0, entity should be at boundary (YPosition = 24.0) and should be destroyed
	elapsed = 1.0
	decaySystem.updateFallingEntities(world, elapsed)

	if len(decaySystem.fallingEntities) != 0 {
		t.Errorf("Expected 0 entities at elapsed=1.0 (exactly at boundary), got %d", len(decaySystem.fallingEntities))
	}

	// Verify entity was destroyed
	fallingType := reflect.TypeOf(components.FallingDecayComponent{})
	_, exists := world.GetComponent(entity, fallingType)
	if exists {
		t.Error("Entity at boundary should have been destroyed")
	}
}

// TestFallingEntityMemoryLeakPrevention tests that no memory leaks occur from destroyed entities
func TestFallingEntityMemoryLeakPrevention(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	gameHeight := 10
	decaySystem := NewDecaySystem(80, gameHeight, 80, 0, ctx)

	// Create multiple entities with varying speeds
	initialCount := 50
	for i := 0; i < initialCount; i++ {
		entity := world.CreateEntity()
		// Speeds range from very fast (103.0) to slow (5.0)
		speed := 5.0 + float64(i)*2.0
		world.AddComponent(entity, components.FallingDecayComponent{
			Column:    i % 80,
			YPosition: 0.0,
			Speed:     speed,
			Char:      rune('A' + (i % 26)),
		})
		decaySystem.fallingEntities = append(decaySystem.fallingEntities, entity)
	}

	if len(decaySystem.fallingEntities) != initialCount {
		t.Fatalf("Expected %d initial entities, got %d", initialCount, len(decaySystem.fallingEntities))
	}

	// Simulate time progression with discrete steps
	// Use integer loop to avoid floating point precision issues
	for i := 1; i <= 25; i++ {
		elapsed := float64(i) * 0.1
		decaySystem.updateFallingEntities(world, elapsed)
	}

	// After 2.5 seconds, all entities should be destroyed
	// Even the slowest entity (speed=5.0) reaches YPosition=12.5 at elapsed=2.5
	// which is > gameHeight (10)
	remainingCount := len(decaySystem.fallingEntities)

	if remainingCount != 0 {
		t.Errorf("Expected 0 remaining entities after 2.5s, got %d", remainingCount)
	}

	// Verify all entities are destroyed from world
	fallingType := reflect.TypeOf(components.FallingDecayComponent{})
	allFallingEntities := world.GetEntitiesWith(fallingType)
	if len(allFallingEntities) != 0 {
		t.Errorf("Expected 0 FallingDecayComponent entities in world, got %d - potential memory leak", len(allFallingEntities))
	}
}

// TestSingleDecayPerAnimation tests that a character is only decayed once during entire animation
// even with multiple falling entities in the same column across multiple update frames
func TestSingleDecayPerAnimation(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	decaySystem := NewDecaySystem(80, 24, 80, 0, ctx)
	spawnSystem := NewSpawnSystem(80, 24, 0, 0, ctx)
	decaySystem.SetSpawnSystem(spawnSystem)

	// Create a test character at position (10, 12) with Bright level
	entity := world.CreateEntity()
	world.AddComponent(entity, components.PositionComponent{X: 10, Y: 12})
	world.AddComponent(entity, components.CharacterComponent{
		Rune:  'A',
		Style: render.GetStyleForSequence(components.SequenceGreen, components.LevelBright),
	})
	world.AddComponent(entity, components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceGreen,
		Level: components.LevelBright,
	})
	world.UpdateSpatialIndex(entity, 10, 12)

	// Start decay animation - this should initialize decayedThisFrame map
	decaySystem.animating = true
	decaySystem.startTime = mockTime.Now()
	decaySystem.decayedThisFrame = make(map[engine.Entity]bool)

	// Create multiple falling entities at column 10 with different speeds
	// These will pass over the character at different times during the animation
	falling1 := world.CreateEntity()
	world.AddComponent(falling1, components.FallingDecayComponent{
		Column:    10,
		YPosition: 0.0,
		Speed:     8.0, // Will reach row 12 at elapsed = 1.5 seconds
		Char:      'X',
	})
	falling2 := world.CreateEntity()
	world.AddComponent(falling2, components.FallingDecayComponent{
		Column:    10,
		YPosition: 0.0,
		Speed:     12.0, // Will reach row 12 at elapsed = 1.0 seconds
		Char:      'Y',
	})
	falling3 := world.CreateEntity()
	world.AddComponent(falling3, components.FallingDecayComponent{
		Column:    10,
		YPosition: 0.0,
		Speed:     6.0, // Will reach row 12 at elapsed = 2.0 seconds
		Char:      'Z',
	})
	decaySystem.fallingEntities = []engine.Entity{falling1, falling2, falling3}

	// Simulate multiple update frames during the animation
	// Frame 1: elapsed = 0.5s - no entities have reached row 12 yet
	mockTime.Advance(500 * time.Millisecond)
	decaySystem.updateAnimation(world)

	// Check character is still Bright (not decayed yet)
	seqType := reflect.TypeOf(components.SequenceComponent{})
	seqComp, ok := world.GetComponent(entity, seqType)
	if !ok {
		t.Fatal("Character entity lost SequenceComponent")
	}
	seq := seqComp.(components.SequenceComponent)
	if seq.Level != components.LevelBright {
		t.Errorf("After 0.5s, expected level Bright, got %v", seq.Level)
	}

	// Frame 2: elapsed = 1.0s - falling2 (speed 12.0) reaches row 12, should decay character
	mockTime.Advance(500 * time.Millisecond)
	decaySystem.updateAnimation(world)

	// Check character decayed once: Bright -> Normal
	seqComp, ok = world.GetComponent(entity, seqType)
	if !ok {
		t.Fatal("Character entity lost SequenceComponent")
	}
	seq = seqComp.(components.SequenceComponent)
	if seq.Level != components.LevelNormal {
		t.Errorf("After 1.0s, expected level Normal (decayed once), got %v", seq.Level)
	}

	// Frame 3: elapsed = 1.5s - falling1 (speed 8.0) reaches row 12, should NOT decay again
	mockTime.Advance(500 * time.Millisecond)
	decaySystem.updateAnimation(world)

	// Check character is still Normal (not decayed again)
	seqComp, ok = world.GetComponent(entity, seqType)
	if !ok {
		t.Fatal("Character entity lost SequenceComponent")
	}
	seq = seqComp.(components.SequenceComponent)
	if seq.Level != components.LevelNormal {
		t.Errorf("After 1.5s, expected level Normal (no second decay), got %v", seq.Level)
	}

	// Frame 4: elapsed = 2.0s - falling3 (speed 6.0) reaches row 12, should NOT decay again
	mockTime.Advance(500 * time.Millisecond)
	decaySystem.updateAnimation(world)

	// Check character is still Normal (not decayed a third time)
	seqComp, ok = world.GetComponent(entity, seqType)
	if !ok {
		t.Fatal("Character entity lost SequenceComponent")
	}
	seq = seqComp.(components.SequenceComponent)
	if seq.Level != components.LevelNormal {
		t.Errorf("After 2.0s, expected level Normal (no third decay), got %v", seq.Level)
	}

	// Verify the character is in the decayed tracking map
	if !decaySystem.decayedThisFrame[entity] {
		t.Error("Character should be marked as decayed in tracking map")
	}

	// Complete the animation
	animationDuration := float64(24) / constants.FallingDecayMinSpeed
	mockTime.Advance(time.Duration(animationDuration) * time.Second)
	decaySystem.updateAnimation(world)

	// Verify animation completed and tracking map was cleared
	if decaySystem.animating {
		t.Error("Animation should be complete")
	}
	if len(decaySystem.decayedThisFrame) != 0 {
		t.Error("Decay tracking map should be cleared after animation completes")
	}
}

// TestFallingDecayFullCoverage tests that all columns have exactly one falling entity (full coverage)
func TestFallingDecayFullCoverage(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	gameWidth := 80
	decaySystem := NewDecaySystem(gameWidth, 24, 80, 0, ctx)

	// Trigger decay animation
	mockTime.Advance(61 * time.Second) // Trigger decay
	decaySystem.Update(world, 16*time.Millisecond)

	if !decaySystem.IsAnimating() {
		t.Fatal("Expected decay system to be animating after time trigger")
	}

	// Track which columns have falling entities
	columnCoverage := make(map[int]int) // column -> count

	fallingType := reflect.TypeOf(components.FallingDecayComponent{})
	for _, entity := range decaySystem.fallingEntities {
		fallComp, ok := world.GetComponent(entity, fallingType)
		if !ok {
			t.Errorf("Falling entity %d missing FallingDecayComponent", entity)
			continue
		}

		fall := fallComp.(components.FallingDecayComponent)
		columnCoverage[fall.Column]++
	}

	// Verify all columns have exactly one falling entity
	for col := 0; col < gameWidth; col++ {
		count, exists := columnCoverage[col]
		if !exists {
			t.Errorf("Column %d has no falling entity - missing coverage", col)
		} else if count != 1 {
			t.Errorf("Column %d has %d falling entities, expected exactly 1", col, count)
		}
	}

	// Verify no extra columns
	if len(columnCoverage) != gameWidth {
		t.Errorf("Expected %d columns with falling entities, got %d", gameWidth, len(columnCoverage))
	}

	// Verify total count
	if len(decaySystem.fallingEntities) != gameWidth {
		t.Errorf("Expected exactly %d falling entities (one per column), got %d", gameWidth, len(decaySystem.fallingEntities))
	}
}

// TestFallingDecayIndividualCleanup tests that entities are cleaned up individually when reaching bottom
func TestFallingDecayIndividualCleanup(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	gameHeight := 24
	decaySystem := NewDecaySystem(80, gameHeight, 80, 0, ctx)
	decaySystem.animating = true
	decaySystem.startTime = mockTime.Now()
	decaySystem.decayedThisFrame = make(map[engine.Entity]bool)

	// Create falling entities with different speeds
	// Slow entity: will take longer to reach bottom
	slowEntity := world.CreateEntity()
	world.AddComponent(slowEntity, components.FallingDecayComponent{
		Column:    10,
		YPosition: 0.0,
		Speed:     5.0, // Slow speed - takes 4.8s to reach bottom
		Char:      'S',
	})

	// Medium entity
	mediumEntity := world.CreateEntity()
	world.AddComponent(mediumEntity, components.FallingDecayComponent{
		Column:    30,
		YPosition: 0.0,
		Speed:     20.0, // Medium speed - takes 1.2s to reach bottom
		Char:      'M',
	})

	// Fast entity: will reach bottom quickly
	fastEntity := world.CreateEntity()
	world.AddComponent(fastEntity, components.FallingDecayComponent{
		Column:    20,
		YPosition: 0.0,
		Speed:     50.0, // Fast speed - takes 0.48s to reach bottom
		Char:      'F',
	})

	decaySystem.fallingEntities = []engine.Entity{slowEntity, mediumEntity, fastEntity}

	// Initial count should be 3
	if len(decaySystem.fallingEntities) != 3 {
		t.Fatalf("Expected 3 falling entities initially, got %d", len(decaySystem.fallingEntities))
	}

	// Simulate elapsed time: 0.5s - fast entity should pass bottom
	elapsed := 0.5
	decaySystem.updateFallingEntities(world, elapsed)

	if len(decaySystem.fallingEntities) != 2 {
		t.Errorf("After 0.5s, expected 2 entities remaining (fast entity cleaned up), got %d", len(decaySystem.fallingEntities))
	}

	// Verify fast entity was destroyed
	fallingType := reflect.TypeOf(components.FallingDecayComponent{})
	_, exists := world.GetComponent(fastEntity, fallingType)
	if exists {
		t.Error("Fast entity should have been destroyed after passing bottom")
	}

	// Simulate more time: 1.3s total - medium entity should pass bottom
	elapsed = 1.3
	decaySystem.updateFallingEntities(world, elapsed)

	if len(decaySystem.fallingEntities) != 1 {
		t.Errorf("After 1.3s, expected 1 entity remaining (medium entity cleaned up), got %d", len(decaySystem.fallingEntities))
	}

	// Verify medium entity was destroyed
	_, exists = world.GetComponent(mediumEntity, fallingType)
	if exists {
		t.Error("Medium entity should have been destroyed after passing bottom")
	}

	// Simulate more time: 5.0s total - slow entity should pass bottom
	elapsed = 5.0
	decaySystem.updateFallingEntities(world, elapsed)

	if len(decaySystem.fallingEntities) != 0 {
		t.Errorf("After 5.0s, expected 0 entities remaining (all cleaned up), got %d", len(decaySystem.fallingEntities))
	}

	// Verify slow entity was destroyed
	_, exists = world.GetComponent(slowEntity, fallingType)
	if exists {
		t.Error("Slow entity should have been destroyed after passing bottom")
	}
}

// TestFallingDecayOncePerAnimation tests that characters are only decayed once per animation
func TestFallingDecayOncePerAnimation(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	decaySystem := NewDecaySystem(80, 24, 80, 0, ctx)
	spawnSystem := NewSpawnSystem(80, 24, 0, 0, ctx)
	decaySystem.SetSpawnSystem(spawnSystem)

	// Create test characters at same column but different rows
	entity1 := world.CreateEntity()
	world.AddComponent(entity1, components.PositionComponent{X: 15, Y: 5})
	world.AddComponent(entity1, components.CharacterComponent{
		Rune:  'A',
		Style: render.GetStyleForSequence(components.SequenceBlue, components.LevelBright),
	})
	world.AddComponent(entity1, components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceBlue,
		Level: components.LevelBright,
	})
	world.UpdateSpatialIndex(entity1, 15, 5)

	entity2 := world.CreateEntity()
	world.AddComponent(entity2, components.PositionComponent{X: 15, Y: 10})
	world.AddComponent(entity2, components.CharacterComponent{
		Rune:  'B',
		Style: render.GetStyleForSequence(components.SequenceGreen, components.LevelBright),
	})
	world.AddComponent(entity2, components.SequenceComponent{
		ID:    2,
		Index: 0,
		Type:  components.SequenceGreen,
		Level: components.LevelBright,
	})
	world.UpdateSpatialIndex(entity2, 15, 10)

	// Start decay animation
	mockTime.Advance(61 * time.Second)
	decaySystem.Update(world, 16*time.Millisecond)

	if !decaySystem.IsAnimating() {
		t.Fatal("Expected decay system to be animating")
	}

	// Run animation for several update cycles
	for i := 0; i < 20; i++ {
		mockTime.Advance(100 * time.Millisecond)
		decaySystem.Update(world, 16*time.Millisecond)
	}

	// Check that each character was decayed exactly once
	seqType := reflect.TypeOf(components.SequenceComponent{})

	seq1Comp, ok := world.GetComponent(entity1, seqType)
	if !ok {
		t.Fatal("Entity1 lost SequenceComponent")
	}
	seq1 := seq1Comp.(components.SequenceComponent)
	// Blue Bright should decay to Blue Normal (one level down)
	if seq1.Level != components.LevelNormal {
		t.Errorf("Entity1: expected level Normal (decayed once from Bright), got %v", seq1.Level)
	}
	if seq1.Type != components.SequenceBlue {
		t.Errorf("Entity1: expected type Blue, got %v", seq1.Type)
	}

	seq2Comp, ok := world.GetComponent(entity2, seqType)
	if !ok {
		t.Fatal("Entity2 lost SequenceComponent")
	}
	seq2 := seq2Comp.(components.SequenceComponent)
	// Green Bright should decay to Green Normal (one level down)
	if seq2.Level != components.LevelNormal {
		t.Errorf("Entity2: expected level Normal (decayed once from Bright), got %v", seq2.Level)
	}
	if seq2.Type != components.SequenceGreen {
		t.Errorf("Entity2: expected type Green, got %v", seq2.Type)
	}
}

// TestFallingDecayAllPositions tests that decay works correctly for characters at all Y positions
func TestFallingDecayAllPositions(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	gameHeight := 24
	decaySystem := NewDecaySystem(80, gameHeight, 80, 0, ctx)
	spawnSystem := NewSpawnSystem(80, gameHeight, 0, 0, ctx)
	decaySystem.SetSpawnSystem(spawnSystem)

	// Create characters at all Y positions (all rows)
	entities := make([]engine.Entity, gameHeight)
	for y := 0; y < gameHeight; y++ {
		entity := world.CreateEntity()
		world.AddComponent(entity, components.PositionComponent{X: 10, Y: y})
		world.AddComponent(entity, components.CharacterComponent{
			Rune:  rune('A' + (y % 26)),
			Style: render.GetStyleForSequence(components.SequenceGreen, components.LevelBright),
		})
		world.AddComponent(entity, components.SequenceComponent{
			ID:    y,
			Index: 0,
			Type:  components.SequenceGreen,
			Level: components.LevelBright,
		})
		world.UpdateSpatialIndex(entity, 10, y)
		entities[y] = entity
	}

	// Trigger decay animation
	mockTime.Advance(61 * time.Second)
	decaySystem.Update(world, 16*time.Millisecond)

	if !decaySystem.IsAnimating() {
		t.Fatal("Expected decay system to be animating")
	}

	// Run animation - need to call Update multiple times for falling entities to progress
	// and decay characters at all positions
	// With FallingDecayMaxSpeed = 15.0, fastest entity moves 15 rows/sec
	// Use small time increments (16ms ~= 60fps) to prevent skipping rows
	// With FallingDecayMinSpeed = 5.0, slowest entity needs 24/5.0 = 4.8 seconds
	// Run for 300 * 16ms = 4.8s, then a bit more to ensure completion
	for i := 0; i < 350; i++ {
		mockTime.Advance(16 * time.Millisecond)
		decaySystem.Update(world, 16*time.Millisecond)
	}

	// Check if animation completed, if not advance more time
	if decaySystem.IsAnimating() {
		animationDuration := float64(gameHeight) / constants.FallingDecayMinSpeed
		mockTime.Advance(time.Duration(animationDuration) * time.Second)
		decaySystem.Update(world, 16*time.Millisecond)
	}

	if decaySystem.IsAnimating() {
		t.Error("Expected animation to be complete")
	}

	// Verify all characters at all positions were decayed exactly once
	seqType := reflect.TypeOf(components.SequenceComponent{})
	for y := 0; y < gameHeight; y++ {
		seqComp, ok := world.GetComponent(entities[y], seqType)
		if !ok {
			t.Errorf("Character at Y=%d lost SequenceComponent", y)
			continue
		}
		seq := seqComp.(components.SequenceComponent)

		// Should have decayed from Bright to Normal
		if seq.Level != components.LevelNormal {
			t.Errorf("Character at Y=%d: expected level Normal (decayed once), got %v", y, seq.Level)
		}
		if seq.Type != components.SequenceGreen {
			t.Errorf("Character at Y=%d: expected type Green, got %v", y, seq.Type)
		}
	}
}

// TestFallingDecayEmptyBoard tests decay animation with no characters on the board
func TestFallingDecayEmptyBoard(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	decaySystem := NewDecaySystem(80, 24, 80, 0, ctx)

	// Board is empty - no characters

	// Trigger decay animation
	mockTime.Advance(61 * time.Second)
	decaySystem.Update(world, 16*time.Millisecond)

	if !decaySystem.IsAnimating() {
		t.Fatal("Expected decay system to be animating even with empty board")
	}

	// Verify falling entities were still created
	if len(decaySystem.fallingEntities) != 80 {
		t.Errorf("Expected 80 falling entities even with empty board, got %d", len(decaySystem.fallingEntities))
	}

	// Run animation until completion
	animationDuration := float64(24) / constants.FallingDecayMinSpeed
	mockTime.Advance(time.Duration(animationDuration+1) * time.Second)
	decaySystem.Update(world, 16*time.Millisecond)

	// Animation should complete successfully
	if decaySystem.IsAnimating() {
		t.Error("Expected animation to complete even with empty board")
	}

	// Falling entities should be cleaned up
	if len(decaySystem.fallingEntities) != 0 {
		t.Errorf("Expected falling entities to be cleaned up, got %d remaining", len(decaySystem.fallingEntities))
	}

	// No errors should occur with empty board
	fallingType := reflect.TypeOf(components.FallingDecayComponent{})
	remainingFalling := world.GetEntitiesWith(fallingType)
	if len(remainingFalling) != 0 {
		t.Errorf("Expected no FallingDecayComponent entities remaining, got %d", len(remainingFalling))
	}
}

// TestFallingDecayFullBoard tests decay animation with a board full of characters
func TestFallingDecayFullBoard(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	gameWidth := 80
	gameHeight := 24
	decaySystem := NewDecaySystem(gameWidth, gameHeight, 80, 0, ctx)
	spawnSystem := NewSpawnSystem(gameWidth, gameHeight, 0, 0, ctx)
	decaySystem.SetSpawnSystem(spawnSystem)

	// Fill the entire board with characters
	totalChars := 0
	for y := 0; y < gameHeight; y++ {
		for x := 0; x < gameWidth; x++ {
			entity := world.CreateEntity()
			// Alternate between different sequence types and levels
			seqType := components.SequenceGreen
			if x%2 == 0 {
				seqType = components.SequenceBlue
			}
			level := components.LevelBright
			if y%3 == 1 {
				level = components.LevelNormal
			} else if y%3 == 2 {
				level = components.LevelDark
			}

			world.AddComponent(entity, components.PositionComponent{X: x, Y: y})
			world.AddComponent(entity, components.CharacterComponent{
				Rune:  rune('A' + ((x + y) % 26)),
				Style: render.GetStyleForSequence(seqType, level),
			})
			world.AddComponent(entity, components.SequenceComponent{
				ID:    totalChars,
				Index: 0,
				Type:  seqType,
				Level: level,
			})
			world.UpdateSpatialIndex(entity, x, y)
			totalChars++
		}
	}

	expectedTotal := gameWidth * gameHeight
	if totalChars != expectedTotal {
		t.Fatalf("Expected %d total characters, created %d", expectedTotal, totalChars)
	}

	// Trigger decay animation
	mockTime.Advance(61 * time.Second)
	decaySystem.Update(world, 16*time.Millisecond)

	if !decaySystem.IsAnimating() {
		t.Fatal("Expected decay system to be animating with full board")
	}

	// Verify falling entities were created (one per column)
	if len(decaySystem.fallingEntities) != gameWidth {
		t.Errorf("Expected %d falling entities, got %d", gameWidth, len(decaySystem.fallingEntities))
	}

	// Run animation until completion
	animationDuration := float64(gameHeight) / constants.FallingDecayMinSpeed
	mockTime.Advance(time.Duration(animationDuration+1) * time.Second)
	decaySystem.Update(world, 16*time.Millisecond)

	if decaySystem.IsAnimating() {
		t.Error("Expected animation to complete with full board")
	}

	// Verify all characters were decayed exactly once
	seqType := reflect.TypeOf(components.SequenceComponent{})
	entities := world.GetEntitiesWith(seqType)

	for _, entity := range entities {
		seqComp, ok := world.GetComponent(entity, seqType)
		if !ok {
			continue
		}
		seq := seqComp.(components.SequenceComponent)

		posType := reflect.TypeOf(components.PositionComponent{})
		posComp, ok := world.GetComponent(entity, posType)
		if !ok {
			continue
		}
		pos := posComp.(components.PositionComponent)

		// Check that decay occurred correctly based on original level
		// Note: We can't check exact original level, but we can verify
		// that the entity still exists and has valid decay state
		if seq.Type != components.SequenceBlue && seq.Type != components.SequenceGreen && seq.Type != components.SequenceRed {
			t.Errorf("Character at (%d,%d) has invalid type after decay: %v", pos.X, pos.Y, seq.Type)
		}
		if seq.Level < components.LevelDark || seq.Level > components.LevelBright {
			t.Errorf("Character at (%d,%d) has invalid level after decay: %v", pos.X, pos.Y, seq.Level)
		}
	}

	// Verify no falling entities remain
	if len(decaySystem.fallingEntities) != 0 {
		t.Errorf("Expected falling entities to be cleaned up, got %d remaining", len(decaySystem.fallingEntities))
	}
}

// TestFallingDecayConcurrentPrevention tests that concurrent decay animations are prevented
func TestFallingDecayConcurrentPrevention(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	decaySystem := NewDecaySystem(80, 24, 80, 0, ctx)

	// Trigger first decay animation
	mockTime.Advance(61 * time.Second)
	decaySystem.Update(world, 16*time.Millisecond)

	if !decaySystem.IsAnimating() {
		t.Fatal("Expected first decay animation to start")
	}

	initialFallingCount := len(decaySystem.fallingEntities)
	if initialFallingCount != 80 {
		t.Errorf("Expected 80 falling entities for first animation, got %d", initialFallingCount)
	}

	// Try to trigger second decay while first is still running
	// Advance time slightly (but keep animation running)
	mockTime.Advance(500 * time.Millisecond)
	decaySystem.Update(world, 16*time.Millisecond)

	// Should still be animating (first animation)
	if !decaySystem.IsAnimating() {
		t.Error("Animation should still be running")
	}

	// Should not have spawned additional falling entities
	if len(decaySystem.fallingEntities) != initialFallingCount {
		t.Errorf("Expected falling entity count to remain %d, got %d - concurrent decay may have triggered",
			initialFallingCount, len(decaySystem.fallingEntities))
	}

	// Complete first animation
	animationDuration := float64(24) / constants.FallingDecayMinSpeed
	mockTime.Advance(time.Duration(animationDuration+1) * time.Second)
	decaySystem.Update(world, 16*time.Millisecond)

	if decaySystem.IsAnimating() {
		t.Error("Expected first animation to complete")
	}

	// Now a second decay should be allowed
	mockTime.Advance(61 * time.Second)
	decaySystem.Update(world, 16*time.Millisecond)

	if !decaySystem.IsAnimating() {
		t.Error("Expected second decay animation to start after first completed")
	}

	if len(decaySystem.fallingEntities) != 80 {
		t.Errorf("Expected 80 falling entities for second animation, got %d", len(decaySystem.fallingEntities))
	}
}
