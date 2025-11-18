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

	decaySystem := NewDecaySystem(80, 24, 80, 0, ctx)

	// Trigger decay animation
	decaySystem.animating = true
	decaySystem.startTime = mockTime.Now()
	decaySystem.spawnFallingEntities(world)

	// Check that falling entities were created
	if len(decaySystem.fallingEntities) < constants.FallingDecayMinCount {
		t.Errorf("Expected at least %d falling entities, got %d", constants.FallingDecayMinCount, len(decaySystem.fallingEntities))
	}

	if len(decaySystem.fallingEntities) > constants.FallingDecayMaxCount {
		t.Errorf("Expected at most %d falling entities, got %d", constants.FallingDecayMaxCount, len(decaySystem.fallingEntities))
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
		if fall.Column < 0 || fall.Column >= 80 {
			t.Errorf("Column %d out of bounds [0, 80)", fall.Column)
		}

		// Check character is valid
		if fall.Char == 0 {
			t.Error("Falling character is null")
		}
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
