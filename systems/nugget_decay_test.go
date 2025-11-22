package systems

import (
	"reflect"
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
	world.AddComponent(nuggetEntity, components.PositionComponent{X: 10, Y: 5})
	style := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
	// Use alphanumeric character for nugget (as defined in constants.AlphanumericRunes)
	world.AddComponent(nuggetEntity, components.CharacterComponent{Rune: 'a', Style: style})
	world.AddComponent(nuggetEntity, components.NuggetComponent{ID: 1, SpawnTime: ctx.TimeProvider.Now()})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(nuggetEntity, 10, 5)
		tx.Commit()
	}
	nuggetSystem.activeNugget.Store(uint64(nuggetEntity))

	// Verify nugget exists
	nuggetType := reflect.TypeOf(components.NuggetComponent{})
	if !world.HasComponent(nuggetEntity, nuggetType) {
		t.Fatal("Nugget should exist before decay")
	}

	// Start decay animation
	ctx.State.StartDecayAnimation()
	decaySystem.TriggerDecayAnimation(world)

	// Simulate animation frame-by-frame with small time increments
	// This ensures the falling entity in column 10 will pass through row 5
	// regardless of its random speed
	dt := 0.1      // 100ms per frame
	maxTime := 5.0 // Maximum time to wait (should be way more than enough)
	for elapsed := dt; elapsed < maxTime; elapsed += dt {
		decaySystem.updateFallingEntities(world, elapsed)

		// Check if nugget has been destroyed
		nuggetType := reflect.TypeOf(components.NuggetComponent{})
		if !world.HasComponent(nuggetEntity, nuggetType) {
			// Nugget destroyed, test successful
			break
		}
	}

	// Verify nugget was destroyed
	if world.HasComponent(nuggetEntity, nuggetType) {
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
	world.AddComponent(nuggetEntity, components.PositionComponent{X: 10, Y: 10})
	style := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
	// Use alphanumeric character for nugget (as defined in constants.AlphanumericRunes)
	world.AddComponent(nuggetEntity, components.CharacterComponent{Rune: 'a', Style: style})
	world.AddComponent(nuggetEntity, components.NuggetComponent{ID: 1, SpawnTime: ctx.TimeProvider.Now()})
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
	// If elapsed=0.5s and speed=5.0 (min), then YPosition = 2.5 (row 2)
	elapsed := 0.5

	// Update falling entities
	decaySystem.updateFallingEntities(world, elapsed)

	// Verify nugget still exists (decay hasn't reached it yet)
	nuggetType := reflect.TypeOf(components.NuggetComponent{})
	if !world.HasComponent(nuggetEntity, nuggetType) {
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
	world.AddComponent(nuggetEntity1, components.PositionComponent{X: 10, Y: 5})
	style := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
	// Use alphanumeric character for nugget (as defined in constants.AlphanumericRunes)
	world.AddComponent(nuggetEntity1, components.CharacterComponent{Rune: 'a', Style: style})
	world.AddComponent(nuggetEntity1, components.NuggetComponent{ID: 1, SpawnTime: ctx.TimeProvider.Now()})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(nuggetEntity1, 10, 5)
		tx.Commit()
	}

	nuggetEntity2 := world.CreateEntity()
	world.AddComponent(nuggetEntity2, components.PositionComponent{X: 20, Y: 5})
	// Use alphanumeric character for nugget (as defined in constants.AlphanumericRunes)
	world.AddComponent(nuggetEntity2, components.CharacterComponent{Rune: 'b', Style: style})
	world.AddComponent(nuggetEntity2, components.NuggetComponent{ID: 2, SpawnTime: ctx.TimeProvider.Now()})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(nuggetEntity2, 20, 5)
		tx.Commit()
	}

	nuggetSystem.activeNugget.Store(uint64(nuggetEntity1))

	// Start decay animation
	ctx.State.StartDecayAnimation()
	decaySystem.TriggerDecayAnimation(world)

	// Simulate animation frame-by-frame with small time increments
	dt := 0.1
	maxTime := 5.0
	for elapsed := dt; elapsed < maxTime; elapsed += dt {
		decaySystem.updateFallingEntities(world, elapsed)

		// Check if both nuggets have been destroyed
		nuggetType := reflect.TypeOf(components.NuggetComponent{})
		if !world.HasComponent(nuggetEntity1, nuggetType) && !world.HasComponent(nuggetEntity2, nuggetType) {
			break
		}
	}

	// Verify both nuggets were destroyed
	nuggetType := reflect.TypeOf(components.NuggetComponent{})
	if world.HasComponent(nuggetEntity1, nuggetType) {
		t.Error("First nugget should be destroyed")
	}
	if world.HasComponent(nuggetEntity2, nuggetType) {
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
	world.AddComponent(nuggetEntity, components.PositionComponent{X: 10, Y: 5})
	nuggetStyle := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
	// Use alphanumeric character for nugget (as defined in constants.AlphanumericRunes)
	world.AddComponent(nuggetEntity, components.CharacterComponent{Rune: 'a', Style: nuggetStyle})
	world.AddComponent(nuggetEntity, components.NuggetComponent{ID: 1, SpawnTime: ctx.TimeProvider.Now()})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(nuggetEntity, 10, 5)
		tx.Commit()
	}
	nuggetSystem.activeNugget.Store(uint64(nuggetEntity))

	// Create a blue sequence at position (20, 5)
	seqEntity := world.CreateEntity()
	world.AddComponent(seqEntity, components.PositionComponent{X: 20, Y: 5})
	seqStyle := render.GetStyleForSequence(components.SequenceBlue, components.LevelBright)
	world.AddComponent(seqEntity, components.CharacterComponent{Rune: 'a', Style: seqStyle})
	world.AddComponent(seqEntity, components.SequenceComponent{
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

	// Simulate animation frame-by-frame with small time increments
	dt := 0.1
	maxTime := 5.0
	for elapsed := dt; elapsed < maxTime; elapsed += dt {
		decaySystem.updateFallingEntities(world, elapsed)

		// Check if nugget has been destroyed
		nuggetType := reflect.TypeOf(components.NuggetComponent{})
		seqType := reflect.TypeOf(components.SequenceComponent{})

		nuggetDestroyed := !world.HasComponent(nuggetEntity, nuggetType)
		seqDecayed := false
		if world.HasComponent(seqEntity, seqType) {
			seqComp, _ := world.GetComponent(seqEntity, seqType)
			seq := seqComp.(components.SequenceComponent)
			seqDecayed = seq.Level == components.LevelNormal
		}

		if nuggetDestroyed && seqDecayed {
			break
		}
	}

	// Verify nugget was destroyed
	nuggetType := reflect.TypeOf(components.NuggetComponent{})
	if world.HasComponent(nuggetEntity, nuggetType) {
		t.Error("Nugget should be destroyed")
	}

	// Verify sequence was decayed (should still exist but level changed)
	seqType := reflect.TypeOf(components.SequenceComponent{})
	if !world.HasComponent(seqEntity, seqType) {
		t.Error("Sequence should still exist after decay")
	}

	// Get sequence component and verify it was decayed
	seqComp, ok := world.GetComponent(seqEntity, seqType)
	if !ok {
		t.Fatal("Failed to get sequence component")
	}
	seq := seqComp.(components.SequenceComponent)
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
	world.AddComponent(nuggetEntity, components.PositionComponent{X: 10, Y: 5})
	style := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
	// Use alphanumeric character for nugget (as defined in constants.AlphanumericRunes)
	world.AddComponent(nuggetEntity, components.CharacterComponent{Rune: 'a', Style: style})
	world.AddComponent(nuggetEntity, components.NuggetComponent{ID: 1, SpawnTime: mockTime.Now()})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(nuggetEntity, 10, 5)
		tx.Commit()
	}
	nuggetSystem.activeNugget.Store(uint64(nuggetEntity))

	// Start decay animation
	ctx.State.StartDecayAnimation()
	decaySystem.TriggerDecayAnimation(world)

	// Simulate animation frame-by-frame with small time increments
	dt := 0.1
	maxTime := 5.0
	for elapsed := dt; elapsed < maxTime; elapsed += dt {
		decaySystem.updateFallingEntities(world, elapsed)

		// Check if nugget has been destroyed
		nuggetType := reflect.TypeOf(components.NuggetComponent{})
		if !world.HasComponent(nuggetEntity, nuggetType) {
			break
		}
	}

	// Verify nugget was destroyed
	nuggetType := reflect.TypeOf(components.NuggetComponent{})
	if world.HasComponent(nuggetEntity, nuggetType) {
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
	if !world.HasComponent(engine.Entity(newActiveNugget), nuggetType) {
		t.Error("New nugget should have NuggetComponent")
	}

	posType := reflect.TypeOf(components.PositionComponent{})
	if !world.HasComponent(engine.Entity(newActiveNugget), posType) {
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
	world.AddComponent(nuggetEntity, components.PositionComponent{X: 10, Y: 5})
	style := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
	// Use alphanumeric character for nugget (as defined in constants.AlphanumericRunes)
	world.AddComponent(nuggetEntity, components.CharacterComponent{Rune: 'a', Style: style})
	world.AddComponent(nuggetEntity, components.NuggetComponent{ID: 1, SpawnTime: ctx.TimeProvider.Now()})
	{
		tx := world.BeginSpatialTransaction()
		tx.Spawn(nuggetEntity, 10, 5)
		tx.Commit()
	}
	nuggetSystem.activeNugget.Store(uint64(nuggetEntity))

	// Start decay animation
	ctx.State.StartDecayAnimation()
	decaySystem.TriggerDecayAnimation(world)

	// Simulate animation frame-by-frame with small time increments
	dt := 0.1
	maxTime := 5.0
	for elapsed := dt; elapsed < maxTime; elapsed += dt {
		decaySystem.updateFallingEntities(world, elapsed)

		// Check if nugget has been destroyed
		nuggetType := reflect.TypeOf(components.NuggetComponent{})
		if !world.HasComponent(nuggetEntity, nuggetType) {
			break
		}
	}

	// Verify nugget was destroyed
	nuggetType := reflect.TypeOf(components.NuggetComponent{})
	if world.HasComponent(nuggetEntity, nuggetType) {
		t.Error("Nugget should be destroyed after first decay pass")
	}

	// Call updateFallingEntities again with more elapsed time
	// This should not cause any errors (no double-processing)
	for elapsed := maxTime; elapsed < maxTime*2; elapsed += dt {
		decaySystem.updateFallingEntities(world, elapsed)
	}

	// Test passes if no panic or error occurred
}