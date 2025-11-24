package systems

import (
	"testing"
	"time"
	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// TestDecayDestroysNugget verifies that falling decay entities destroy nuggets
func TestDecayDestroysNugget(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	ctx := engine.NewGameContext(screen)
	world := ctx.World
	nuggetSystem := NewNuggetSystem(ctx)
	decaySystem := NewDecaySystem(ctx)
	decaySystem.SetNuggetSystem(nuggetSystem)

	// Create a nugget at position (10, 5)
	nuggetEntity := world.CreateEntity()
	style := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
	// Use alphanumeric character for nugget (as defined in constants.AlphanumericRunes)
	world.Characters.Add(nuggetEntity, components.CharacterComponent{Rune: 'a', Style: style})
	world.Nuggets.Add(nuggetEntity, components.NuggetComponent{ID: 1, SpawnTime: ctx.TimeProvider.Now()})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(nuggetEntity, 10, 5)
		tx.Commit()
	}
	nuggetSystem.activeNugget.Store(uint64(nuggetEntity))

	// Verify nugget exists
	// Using direct store access
	if !world.Nuggets.Has(nuggetEntity) {
		t.Fatal("Nugget should exist before decay")
	}

	// Start decay animation
	ctx.State.StartDecayAnimation()
	decaySystem.TriggerDecayAnimation(world)

	// Simulate animation frame-by-frame matching real game frame rate (16ms)
	// This prevents row skipping at high speeds (max 15 rows/s * 0.016s = 0.24 rows per frame)
	dt := 0.016    // 16ms per frame (real game frame rate)
	maxTime := 5.0 // Maximum time to wait (should be way more than enough)
	for elapsed := dt; elapsed < maxTime; elapsed += dt {
		decaySystem.updateFallingEntities(world, elapsed)

		// Check if nugget has been destroyed
		// Using direct store access
		if !world.Nuggets.Has(nuggetEntity) {
			// Nugget destroyed, test successful
			break
		}
	}

	// Verify nugget was destroyed
	if world.Nuggets.Has(nuggetEntity) {
		t.Error("Nugget should be destroyed after decay passes over it")
	}

	// Verify active nugget reference was cleared
	activeNugget := nuggetSystem.GetActiveNugget()
	if activeNugget != 0 {
		t.Errorf("Active nugget reference should be cleared, got %d", activeNugget)
	}

	// Verify nugget is no longer in spatial index
	entityAtPosition := world.GetEntityAtPosition(10, 5)
	if entityAtPosition == nuggetEntity {
		t.Error("Nugget should be removed from spatial index")
	}
}

// TestDecayDoesNotDestroyNuggetAtDifferentPosition verifies decay only destroys nugget at the exact position
func TestDecayDoesNotDestroyNuggetAtDifferentPosition(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	ctx := engine.NewGameContext(screen)
	world := ctx.World
	nuggetSystem := NewNuggetSystem(ctx)
	decaySystem := NewDecaySystem(ctx)
	decaySystem.SetNuggetSystem(nuggetSystem)

	// Create a nugget at position (10, 10) - deeper in the screen
	nuggetEntity := world.CreateEntity()
	style := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
	// Use alphanumeric character for nugget (as defined in constants.AlphanumericRunes)
	world.Characters.Add(nuggetEntity, components.CharacterComponent{Rune: 'a', Style: style})
	world.Nuggets.Add(nuggetEntity, components.NuggetComponent{ID: 1, SpawnTime: ctx.TimeProvider.Now()})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(nuggetEntity, 10, 10)
		tx.Commit()
	}
	nuggetSystem.activeNugget.Store(uint64(nuggetEntity))

	// Start decay animation
	ctx.State.StartDecayAnimation()
	decaySystem.TriggerDecayAnimation(world)

	// Simulate only a short time so falling entity is still above the nugget (before row 10)
	// At 16ms frame rate, after ~30 frames (0.48s) at min speed (5.0 rows/s): YPosition = 2.4 (row 2)
	elapsed := 0.48

	// Update falling entities
	decaySystem.updateFallingEntities(world, elapsed)

	// Verify nugget still exists (decay hasn't reached it yet)
	// Using direct store access
	if !world.Nuggets.Has(nuggetEntity) {
		t.Error("Nugget should still exist before decay reaches it")
	}

	// Verify active nugget reference is still set
	activeNugget := nuggetSystem.GetActiveNugget()
	if activeNugget == 0 {
		t.Error("Active nugget reference should still be set")
	}
}

// TestDecayDestroyMultipleNuggetsInDifferentColumns verifies decay can destroy nuggets in multiple columns
func TestDecayDestroyMultipleNuggetsInDifferentColumns(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	ctx := engine.NewGameContext(screen)
	world := ctx.World
	nuggetSystem := NewNuggetSystem(ctx)
	decaySystem := NewDecaySystem(ctx)
	decaySystem.SetNuggetSystem(nuggetSystem)

	// Create multiple nugget-like entities at different columns, same row
	// (Note: Only one can be the "active" nugget, but we can test the destruction logic)
	nuggetEntity1 := world.CreateEntity()
	world.Positions.Add(nuggetEntity1, components.PositionComponent{X: 10, Y: 5})
	style := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
	// Use alphanumeric character for nugget (as defined in constants.AlphanumericRunes)
	world.Characters.Add(nuggetEntity1, components.CharacterComponent{Rune: 'a', Style: style})
	world.Nuggets.Add(nuggetEntity1, components.NuggetComponent{ID: 1, SpawnTime: ctx.TimeProvider.Now()})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(nuggetEntity1, 10, 5)
		tx.Commit()
	}

	nuggetEntity2 := world.CreateEntity()
	world.Positions.Add(nuggetEntity2, components.PositionComponent{X: 20, Y: 5})
	// Use alphanumeric character for nugget (as defined in constants.AlphanumericRunes)
	world.Characters.Add(nuggetEntity2, components.CharacterComponent{Rune: 'b', Style: style})
	world.Nuggets.Add(nuggetEntity2, components.NuggetComponent{ID: 2, SpawnTime: ctx.TimeProvider.Now()})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(nuggetEntity2, 20, 5)
		tx.Commit()
	}

	nuggetSystem.activeNugget.Store(uint64(nuggetEntity1))

	// Start decay animation
	ctx.State.StartDecayAnimation()
	decaySystem.TriggerDecayAnimation(world)

	// Simulate animation frame-by-frame matching real game frame rate (16ms)
	dt := 0.016
	maxTime := 5.0
	for elapsed := dt; elapsed < maxTime; elapsed += dt {
		decaySystem.updateFallingEntities(world, elapsed)

		// Check if both nuggets have been destroyed
		// Using direct store access
		if !world.Nuggets.Has(nuggetEntity1) && !world.Nuggets.Has(nuggetEntity2) {
			break
		}
	}

	// Verify both nuggets were destroyed
	// Using direct store access
	if world.Nuggets.Has(nuggetEntity1) {
		t.Error("First nugget should be destroyed")
	}
	if world.Nuggets.Has(nuggetEntity2) {
		t.Error("Second nugget should be destroyed")
	}

	// Verify active nugget reference was cleared
	activeNugget := nuggetSystem.GetActiveNugget()
	if activeNugget != 0 {
		t.Error("Active nugget reference should be cleared")
	}
}

// TestDecayDestroyNuggetAndSequence verifies decay can process both nuggets and sequences
func TestDecayDestroyNuggetAndSequence(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	ctx := engine.NewGameContext(screen)
	world := ctx.World
	nuggetSystem := NewNuggetSystem(ctx)
	spawnSystem := NewSpawnSystem(ctx)
	decaySystem := NewDecaySystem(ctx)
	decaySystem.SetNuggetSystem(nuggetSystem)
	decaySystem.SetSpawnSystem(spawnSystem)

	// Create a nugget at position (10, 5)
	nuggetEntity := world.CreateEntity()
	world.Positions.Add(nuggetEntity, components.PositionComponent{X: 10, Y: 5})
	nuggetStyle := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
	// Use alphanumeric character for nugget (as defined in constants.AlphanumericRunes)
	world.Characters.Add(nuggetEntity, components.CharacterComponent{Rune: 'a', Style: nuggetStyle})
	world.Nuggets.Add(nuggetEntity, components.NuggetComponent{ID: 1, SpawnTime: ctx.TimeProvider.Now()})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(nuggetEntity, 10, 5)
		tx.Commit()
	}
	nuggetSystem.activeNugget.Store(uint64(nuggetEntity))

	// Create a blue sequence at position (20, 5)
	seqEntity := world.CreateEntity()
	world.Positions.Add(seqEntity, components.PositionComponent{X: 20, Y: 5})
	seqStyle := render.GetStyleForSequence(components.SequenceBlue, components.LevelBright)
	world.Characters.Add(seqEntity, components.CharacterComponent{Rune: 'a', Style: seqStyle})
	world.Sequences.Add(seqEntity, components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceBlue,
		Level: components.LevelBright,
	})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(seqEntity, 20, 5)
		tx.Commit()
	}

	// Increment color counter for blue bright
	spawnSystem.AddColorCount(components.SequenceBlue, components.LevelBright, 1)

	// Start decay animation
	ctx.State.StartDecayAnimation()
	decaySystem.TriggerDecayAnimation(world)

	// Simulate animation frame-by-frame matching real game frame rate (16ms)
	dt := 0.016
	maxTime := 5.0
	for elapsed := dt; elapsed < maxTime; elapsed += dt {
		decaySystem.updateFallingEntities(world, elapsed)

		// Check if nugget has been destroyed
		// Using direct store access

		nuggetDestroyed := !world.Nuggets.Has(nuggetEntity)
		seqDecayed := false
		if world.Sequences.Has(seqEntity) {
			seq, _ := world.Sequences.Get(seqEntity)
			seqDecayed = seq.Level == components.LevelNormal
		}

		if nuggetDestroyed && seqDecayed {
			break
		}
	}

	// Verify nugget was destroyed
	// Using direct store access
	if world.Nuggets.Has(nuggetEntity) {
		t.Error("Nugget should be destroyed")
	}

	// Verify sequence was decayed (should still exist but level changed)
	if !world.Sequences.Has(seqEntity) {
		t.Error("Sequence should still exist after decay")
	}

	// Get sequence component and verify it was decayed
	seq, ok := world.Sequences.Get(seqEntity)
	if !ok {
		t.Fatal("Failed to get sequence component")
	}
	if seq.Level != components.LevelNormal {
		t.Errorf("Sequence should decay to LevelNormal, got %d", seq.Level)
	}
}

// TestDecayNuggetRespawnAfterDestruction verifies nugget respawns after being destroyed by decay
func TestDecayNuggetRespawnAfterDestruction(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	startTime := time.Now()
	mockTime := engine.NewMockTimeProvider(startTime)

	world := engine.NewWorld()
	state := engine.NewGameState(80, 24, 80, mockTime)
	ctx := &engine.GameContext{
		World:        world,
		State:        state,
		TimeProvider: mockTime,
		GameWidth:    80,
		GameHeight:   24,
		Width:        80,
		Height:       24,
	}

	nuggetSystem := NewNuggetSystem(ctx)
	decaySystem := NewDecaySystem(ctx)
	decaySystem.SetNuggetSystem(nuggetSystem)

	// Create a nugget at position (10, 5)
	nuggetEntity := world.CreateEntity()
	world.Positions.Add(nuggetEntity, components.PositionComponent{X: 10, Y: 5})
	style := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
	// Use alphanumeric character for nugget (as defined in constants.AlphanumericRunes)
	world.Characters.Add(nuggetEntity, components.CharacterComponent{Rune: 'a', Style: style})
	world.Nuggets.Add(nuggetEntity, components.NuggetComponent{ID: 1, SpawnTime: mockTime.Now()})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(nuggetEntity, 10, 5)
		tx.Commit()
	}
	nuggetSystem.activeNugget.Store(uint64(nuggetEntity))

	// Start decay animation
	ctx.State.StartDecayAnimation()
	decaySystem.TriggerDecayAnimation(world)

	// Simulate animation frame-by-frame matching real game frame rate (16ms)
	dt := 0.016
	maxTime := 5.0
	for elapsed := dt; elapsed < maxTime; elapsed += dt {
		decaySystem.updateFallingEntities(world, elapsed)

		// Check if nugget has been destroyed
		// Using direct store access
		if !world.Nuggets.Has(nuggetEntity) {
			break
		}
	}

	// Verify nugget was destroyed
	// Using direct store access
	if world.Nuggets.Has(nuggetEntity) {
		t.Error("Nugget should be destroyed after decay")
	}

	// Verify active nugget reference was cleared
	activeNugget := nuggetSystem.GetActiveNugget()
	if activeNugget != 0 {
		t.Error("Active nugget reference should be cleared")
	}

	// Advance time by nuggetSpawnIntervalSeconds
	mockTime.Advance(nuggetSpawnIntervalSeconds * time.Second)

	// Update nugget system (should spawn new nugget)
	nuggetSystem.Update(world, 100*time.Millisecond)

	// Verify new nugget was spawned
	newActiveNugget := nuggetSystem.GetActiveNugget()
	if newActiveNugget == 0 {
		t.Error("New nugget should be spawned after nuggetSpawnIntervalSeconds")
	}

	// Verify new nugget exists and has components
	if !world.Nuggets.Has(engine.Entity(newActiveNugget)) {
		t.Error("New nugget should have NuggetComponent")
	}

	if !world.Positions.Has(engine.Entity(newActiveNugget)) {
		t.Error("New nugget should have PositionComponent")
	}
}

// TestDecayDoesNotProcessSameNuggetTwice verifies nugget is only destroyed once
func TestDecayDoesNotProcessSameNuggetTwice(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	ctx := engine.NewGameContext(screen)
	world := ctx.World
	nuggetSystem := NewNuggetSystem(ctx)
	decaySystem := NewDecaySystem(ctx)
	decaySystem.SetNuggetSystem(nuggetSystem)

	// Create a nugget at position (10, 5)
	nuggetEntity := world.CreateEntity()
	world.Positions.Add(nuggetEntity, components.PositionComponent{X: 10, Y: 5})
	style := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
	// Use alphanumeric character for nugget (as defined in constants.AlphanumericRunes)
	world.Characters.Add(nuggetEntity, components.CharacterComponent{Rune: 'a', Style: style})
	world.Nuggets.Add(nuggetEntity, components.NuggetComponent{ID: 1, SpawnTime: ctx.TimeProvider.Now()})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(nuggetEntity, 10, 5)
		tx.Commit()
	}
	nuggetSystem.activeNugget.Store(uint64(nuggetEntity))

	// Start decay animation
	ctx.State.StartDecayAnimation()
	decaySystem.TriggerDecayAnimation(world)

	// Simulate animation frame-by-frame matching real game frame rate (16ms)
	dt := 0.016
	maxTime := 5.0
	for elapsed := dt; elapsed < maxTime; elapsed += dt {
		decaySystem.updateFallingEntities(world, elapsed)

		// Check if nugget has been destroyed
		// Using direct store access
		if !world.Nuggets.Has(nuggetEntity) {
			break
		}
	}

	// Verify nugget was destroyed
	// Using direct store access
	if world.Nuggets.Has(nuggetEntity) {
		t.Error("Nugget should be destroyed after first decay pass")
	}

	// Call updateFallingEntities again with more elapsed time
	// This should not cause any errors (no double-processing)
	for elapsed := maxTime; elapsed < maxTime*2; elapsed += dt {
		decaySystem.updateFallingEntities(world, elapsed)
	}

	// Test passes if no panic or error occurred
}
