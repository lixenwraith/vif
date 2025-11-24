package systems

import (
	"testing"
	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// TestDecaySystemCounterUpdates tests that decay system updates counters correctly
func TestDecaySystemCounterUpdates(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	spawnSys := NewSpawnSystem(ctx)
	decaySys := NewDecaySystem(ctx)
	decaySys.SetSpawnSystem(spawnSys)

	// Manually create some Blue Bright characters at row 0
	sequenceID := 1
	style := render.GetStyleForSequence(components.SequenceBlue, components.LevelBright)

	for i := 0; i < 5; i++ {
		entity := world.CreateEntity()
		world.Positions.Add(entity, components.PositionComponent{X: i, Y: 0})
		world.Characters.Add(entity, components.CharacterComponent{Rune: 'x', Style: style})
		world.Sequences.Add(entity, components.SequenceComponent{
			ID:    sequenceID,
			Index: i,
			Type:  components.SequenceBlue,
			Level: components.LevelBright,
		})

		tx := world.BeginSpatialTransaction()
		tx.Spawn(entity, i, 0)
		tx.Commit()
	}

	// Manually set the counter
	spawnSys.AddColorCount(components.SequenceBlue, components.LevelBright, 5)

	initialBlueBright := ctx.State.BlueCountBright.Load()
	if initialBlueBright != 5 {
		t.Fatalf("Expected initial Blue Bright count of 5, got %d", initialBlueBright)
	}

	// Apply decay to row 0 - should decay Blue Bright to Blue Normal
	decaySys.applyDecayToRow(world, 0)

	// Verify counters updated correctly
	blueBright := ctx.State.BlueCountBright.Load()
	blueNormal := ctx.State.BlueCountNormal.Load()

	if blueBright != 0 {
		t.Errorf("Expected Blue Bright count of 0 after decay, got %d", blueBright)
	}
	if blueNormal != 5 {
		t.Errorf("Expected Blue Normal count of 5 after decay, got %d", blueNormal)
	}

	// Verify entities were updated
	entities := world.Query().With(world.Positions).With(world.Sequences).Execute()

	for _, entity := range entities {
		seq, _ := world.Sequences.Get(entity)

		if seq.Type != components.SequenceBlue {
			t.Errorf("Expected entity type to still be Blue, got %v", seq.Type)
		}
		if seq.Level != components.LevelNormal {
			t.Errorf("Expected entity level to be Normal after decay, got %v", seq.Level)
		}
	}
}

// TestDecaySystemColorTransitionWithCounters tests Blue->Green and Green->Red transitions with counter updates
func TestDecaySystemColorTransitionWithCounters(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	spawnSys := NewSpawnSystem(ctx)
	decaySys := NewDecaySystem(ctx)
	decaySys.SetSpawnSystem(spawnSys)

	// Create Blue Dark characters at row 0
	sequenceID := 1
	style := render.GetStyleForSequence(components.SequenceBlue, components.LevelDark)

	for i := 0; i < 3; i++ {
		entity := world.CreateEntity()
		world.Positions.Add(entity, components.PositionComponent{X: i, Y: 0})
		world.Characters.Add(entity, components.CharacterComponent{Rune: 'x', Style: style})
		world.Sequences.Add(entity, components.SequenceComponent{
			ID:    sequenceID,
			Index: i,
			Type:  components.SequenceBlue,
			Level: components.LevelDark,
		})

		tx := world.BeginSpatialTransaction()
		tx.Spawn(entity, i, 0)
		tx.Commit()
	}

	spawnSys.AddColorCount(components.SequenceBlue, components.LevelDark, 3)

	// Apply decay - Blue Dark should become Green Bright
	decaySys.applyDecayToRow(world, 0)

	blueDark := ctx.State.BlueCountDark.Load()
	greenBright := ctx.State.GreenCountBright.Load()

	if blueDark != 0 {
		t.Errorf("Expected Blue Dark count of 0 after transition, got %d", blueDark)
	}
	if greenBright != 3 {
		t.Errorf("Expected Green Bright count of 3 after transition, got %d", greenBright)
	}

	// Now test Green Dark -> Red Bright
	// First decay the entities to Green Dark
	for i := 0; i < 2; i++ {
		decaySys.applyDecayToRow(world, 0)
	}

	// Verify they're now Green Dark
	greenDark := ctx.State.GreenCountDark.Load()
	if greenDark != 3 {
		t.Errorf("Expected Green Dark count of 3 before Red transition, got %d", greenDark)
	}

	// One more decay: Green Dark -> Red Bright (Red is not counted)
	decaySys.applyDecayToRow(world, 0)

	greenDarkAfter := ctx.State.GreenCountDark.Load()
	if greenDarkAfter != 0 {
		t.Errorf("Expected Green Dark count of 0 after Red transition, got %d", greenDarkAfter)
	}

	// Verify entities are now Red
	entities := world.Sequences.All()

	if len(entities) == 0 {
		t.Fatal("Expected entities to still exist as Red")
	}

	for _, entity := range entities {
		seq, _ := world.Sequences.Get(entity)

		if seq.Type != components.SequenceRed {
			t.Errorf("Expected entity type to be Red, got %v", seq.Type)
		}
	}
}

// TestScoreSystemCounterDecrement tests that score system decrements counters when typing
func TestScoreSystemCounterDecrement(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	spawnSys := NewSpawnSystem(ctx)
	scoreSys := NewScoreSystem(ctx)
	scoreSys.SetSpawnSystem(spawnSys)

	// Create a Green Normal character at cursor position
	entity := world.CreateEntity()
	style := render.GetStyleForSequence(components.SequenceGreen, components.LevelNormal)
	world.Positions.Add(entity, components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY})
	world.Characters.Add(entity, components.CharacterComponent{Rune: 'a', Style: style})
	world.Sequences.Add(entity, components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceGreen,
		Level: components.LevelNormal,
	})

	tx := world.BeginSpatialTransaction()
	tx.Spawn(entity, ctx.CursorX, ctx.CursorY)
	tx.Commit()

	// Set counter
	spawnSys.AddColorCount(components.SequenceGreen, components.LevelNormal, 1)

	initialCount := ctx.State.GreenCountNormal.Load()
	if initialCount != 1 {
		t.Fatalf("Expected initial count of 1, got %d", initialCount)
	}

	// Type the correct character
	scoreSys.HandleCharacterTyping(world, ctx.CursorX, ctx.CursorY, 'a')

	// Verify counter was decremented
	finalCount := ctx.State.GreenCountNormal.Load()
	if finalCount != 0 {
		t.Errorf("Expected count to be 0 after typing, got %d", finalCount)
	}

	// Verify entity was destroyed
	entityAfter := world.GetEntityAtPosition(ctx.CursorX, ctx.CursorY)
	if entityAfter != 0 {
		t.Error("Expected entity to be destroyed after typing")
	}
}

// TestScoreSystemDoesNotDecrementRedCounter tests that Red characters don't affect counters
func TestScoreSystemDoesNotDecrementRedCounter(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	spawnSys := NewSpawnSystem(ctx)
	scoreSys := NewScoreSystem(ctx)
	scoreSys.SetSpawnSystem(spawnSys)

	// Create a Red character (Red is not tracked in counters)
	entity := world.CreateEntity()
	style := render.GetStyleForSequence(components.SequenceRed, components.LevelBright)
	world.Positions.Add(entity, components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY})
	world.Characters.Add(entity, components.CharacterComponent{Rune: 'r', Style: style})
	world.Sequences.Add(entity, components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceRed,
		Level: components.LevelBright,
	})

	tx := world.BeginSpatialTransaction()
	tx.Spawn(entity, ctx.CursorX, ctx.CursorY)
	tx.Commit()

	// Verify no counters changed (Red is not tracked)
	counts := ctx.State.ReadColorCounts()
	allCountsZero := (counts.BlueBright == 0 && counts.BlueNormal == 0 && counts.BlueDark == 0 &&
		counts.GreenBright == 0 && counts.GreenNormal == 0 && counts.GreenDark == 0)

	if !allCountsZero {
		t.Error("Expected all counters to remain 0 for Red character")
	}

	// Type the character
	scoreSys.HandleCharacterTyping(world, ctx.CursorX, ctx.CursorY, 'r')

	// Verify all counters still zero (Red doesn't affect counters)
	countsAfter := ctx.State.ReadColorCounts()
	if countsAfter.BlueBright != 0 || countsAfter.BlueNormal != 0 || countsAfter.BlueDark != 0 ||
		countsAfter.GreenBright != 0 || countsAfter.GreenNormal != 0 || countsAfter.GreenDark != 0 {
		t.Errorf("Expected all counts to remain 0 after typing Red, got %+v", countsAfter)
	}
}
