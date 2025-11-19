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

// TestNuggetTypingIncreasesHeat verifies that typing on a nugget increases heat by 10%
func TestNuggetTypingIncreasesHeat(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(100, 24)

	ctx := engine.NewGameContext(screen)
	world := ctx.World
	scoreSystem := NewScoreSystem(ctx)
	nuggetSystem := NewNuggetSystem(ctx)
	scoreSystem.SetNuggetSystem(nuggetSystem)

	// Set screen width to 100 for easy calculation
	ctx.Width = 100
	ctx.GameWidth = 100

	// Initial heat should be 0
	initialHeat := ctx.State.GetHeat()
	if initialHeat != 0 {
		t.Fatalf("Initial heat should be 0, got %d", initialHeat)
	}

	// Create a nugget at position (10, 5) with character 'a'
	nuggetEntity := world.CreateEntity()
	world.AddComponent(nuggetEntity, components.PositionComponent{X: 10, Y: 5})
	style := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
	world.AddComponent(nuggetEntity, components.CharacterComponent{Rune: 'a', Style: style})
	world.AddComponent(nuggetEntity, components.NuggetComponent{ID: 1, SpawnTime: ctx.TimeProvider.Now()})
	world.UpdateSpatialIndex(nuggetEntity, 10, 5)
	nuggetSystem.activeNugget.Store(uint64(nuggetEntity))

	// Position cursor on nugget
	ctx.CursorX = 10
	ctx.CursorY = 5
	ctx.State.SetCursorX(10)
	ctx.State.SetCursorY(5)

	// Type matching character 'a' on the nugget
	scoreSystem.HandleCharacterTyping(world, 10, 5, 'a')

	// Verify heat increased by 10% of max (100 / 10 = 10)
	expectedHeat := 10
	finalHeat := ctx.State.GetHeat()
	if finalHeat != expectedHeat {
		t.Errorf("Expected heat to be %d after nugget collection, got %d", expectedHeat, finalHeat)
	}

	// Verify nugget was destroyed
	if world.HasComponent(nuggetEntity, nuggetComponentType()) {
		t.Error("Nugget should have been destroyed after collection")
	}

	// Verify active nugget reference was cleared
	if nuggetSystem.GetActiveNugget() != 0 {
		t.Error("Active nugget reference should be cleared after collection")
	}

	// Verify cursor moved right
	if ctx.CursorX != 11 {
		t.Errorf("Expected cursor X to be 11 after collection, got %d", ctx.CursorX)
	}
}

// TestNuggetTypingDestroysAndReturnsSpawn verifies complete cycle
func TestNuggetTypingDestroysAndReturnsSpawn(t *testing.T) {
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
		CursorX:      0,
		CursorY:      0,
	}

	scoreSystem := NewScoreSystem(ctx)
	nuggetSystem := NewNuggetSystem(ctx)
	scoreSystem.SetNuggetSystem(nuggetSystem)

	// Create first nugget with character 'x'
	nuggetEntity := world.CreateEntity()
	world.AddComponent(nuggetEntity, components.PositionComponent{X: 10, Y: 5})
	style := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
	world.AddComponent(nuggetEntity, components.CharacterComponent{Rune: 'x', Style: style})
	world.AddComponent(nuggetEntity, components.NuggetComponent{ID: 1, SpawnTime: ctx.TimeProvider.Now()})
	world.UpdateSpatialIndex(nuggetEntity, 10, 5)
	nuggetSystem.activeNugget.Store(uint64(nuggetEntity))

	// Position cursor on nugget
	ctx.CursorX = 10
	ctx.CursorY = 5
	ctx.State.SetCursorX(10)
	ctx.State.SetCursorY(5)

	// Type matching character to collect nugget
	scoreSystem.HandleCharacterTyping(world, 10, 5, 'x')

	// Verify nugget was collected
	if nuggetSystem.GetActiveNugget() != 0 {
		t.Fatal("Nugget should have been cleared after collection")
	}

	// Wait for respawn interval (5 seconds)
	mockTime.Advance(6 * time.Second)

	// Update nugget system to trigger respawn
	nuggetSystem.Update(world, time.Second)

	// Verify a new nugget was spawned
	if nuggetSystem.GetActiveNugget() == 0 {
		t.Error("New nugget should have spawned after interval")
	}

	// Verify the new nugget has components
	newNugget := engine.Entity(nuggetSystem.GetActiveNugget())
	if !world.HasComponent(newNugget, nuggetComponentType()) {
		t.Error("New nugget should have NuggetComponent")
	}
	if !world.HasComponent(newNugget, positionComponentType()) {
		t.Error("New nugget should have PositionComponent")
	}
	if !world.HasComponent(newNugget, characterComponentType()) {
		t.Error("New nugget should have CharacterComponent")
	}
}

// TestNuggetTypingNoScoreEffect verifies nugget collection doesn't affect score
func TestNuggetTypingNoScoreEffect(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	ctx := engine.NewGameContext(screen)
	world := ctx.World
	scoreSystem := NewScoreSystem(ctx)
	nuggetSystem := NewNuggetSystem(ctx)
	scoreSystem.SetNuggetSystem(nuggetSystem)

	ctx.Width = 80
	ctx.GameWidth = 80

	// Set initial score
	initialScore := 100
	ctx.State.SetScore(initialScore)

	// Create nugget
	nuggetEntity := world.CreateEntity()
	world.AddComponent(nuggetEntity, components.PositionComponent{X: 10, Y: 5})
	style := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
	world.AddComponent(nuggetEntity, components.CharacterComponent{Rune: '●', Style: style})
	world.AddComponent(nuggetEntity, components.NuggetComponent{ID: 1, SpawnTime: ctx.TimeProvider.Now()})
	world.UpdateSpatialIndex(nuggetEntity, 10, 5)
	nuggetSystem.activeNugget.Store(uint64(nuggetEntity))

	ctx.CursorX = 10
	ctx.CursorY = 5
	ctx.State.SetCursorX(10)
	ctx.State.SetCursorY(5)

	// Collect nugget
	scoreSystem.HandleCharacterTyping(world, 10, 5, 'q')

	// Verify score unchanged
	finalScore := ctx.State.GetScore()
	if finalScore != initialScore {
		t.Errorf("Score should remain unchanged after nugget collection, expected %d, got %d", initialScore, finalScore)
	}
}

// TestNuggetTypingNoErrorEffect verifies nugget collection doesn't trigger error state
func TestNuggetTypingNoErrorEffect(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	ctx := engine.NewGameContext(screen)
	world := ctx.World
	scoreSystem := NewScoreSystem(ctx)
	nuggetSystem := NewNuggetSystem(ctx)
	scoreSystem.SetNuggetSystem(nuggetSystem)

	ctx.Width = 80
	ctx.GameWidth = 80

	// Create nugget with character 'z'
	nuggetEntity := world.CreateEntity()
	world.AddComponent(nuggetEntity, components.PositionComponent{X: 10, Y: 5})
	style := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
	world.AddComponent(nuggetEntity, components.CharacterComponent{Rune: 'z', Style: style})
	world.AddComponent(nuggetEntity, components.NuggetComponent{ID: 1, SpawnTime: ctx.TimeProvider.Now()})
	world.UpdateSpatialIndex(nuggetEntity, 10, 5)
	nuggetSystem.activeNugget.Store(uint64(nuggetEntity))

	ctx.CursorX = 10
	ctx.CursorY = 5
	ctx.State.SetCursorX(10)
	ctx.State.SetCursorY(5)

	// Ensure no error state initially
	ctx.State.SetCursorError(false)

	// Collect nugget with matching character
	scoreSystem.HandleCharacterTyping(world, 10, 5, 'z')

	// Verify no error cursor was set
	if ctx.State.GetCursorError() {
		t.Error("Cursor error should not be set after nugget collection")
	}
}

// TestNuggetTypingMultipleCollections verifies multiple nugget collections accumulate heat
func TestNuggetTypingMultipleCollections(t *testing.T) {
	startTime := time.Now()
	mockTime := engine.NewMockTimeProvider(startTime)

	world := engine.NewWorld()
	state := engine.NewGameState(100, 24, 100, mockTime)
	ctx := &engine.GameContext{
		World:        world,
		State:        state,
		TimeProvider: mockTime,
		GameWidth:    100,
		GameHeight:   24,
		Width:        100,
		Height:       24,
		CursorX:      0,
		CursorY:      0,
	}

	scoreSystem := NewScoreSystem(ctx)
	nuggetSystem := NewNuggetSystem(ctx)
	scoreSystem.SetNuggetSystem(nuggetSystem)

	// Collect first nugget with character 'a'
	nugget1 := world.CreateEntity()
	world.AddComponent(nugget1, components.PositionComponent{X: 10, Y: 5})
	style := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
	world.AddComponent(nugget1, components.CharacterComponent{Rune: 'a', Style: style})
	world.AddComponent(nugget1, components.NuggetComponent{ID: 1, SpawnTime: mockTime.Now()})
	world.UpdateSpatialIndex(nugget1, 10, 5)
	nuggetSystem.activeNugget.Store(uint64(nugget1))

	ctx.CursorX = 10
	ctx.CursorY = 5
	ctx.State.SetCursorX(10)
	ctx.State.SetCursorY(5)

	scoreSystem.HandleCharacterTyping(world, 10, 5, 'a')

	// First collection: heat = 10
	if ctx.State.GetHeat() != 10 {
		t.Errorf("Expected heat 10 after first collection, got %d", ctx.State.GetHeat())
	}

	// Wait and spawn second nugget
	mockTime.Advance(6 * time.Second)
	nuggetSystem.Update(world, time.Second)

	// Find the new nugget and position cursor on it
	nugget2 := engine.Entity(nuggetSystem.GetActiveNugget())
	pos, ok := world.GetComponent(nugget2, positionComponentType())
	if !ok {
		t.Fatal("New nugget should have position")
	}
	posComp := pos.(components.PositionComponent)

	// Get the character from the second nugget
	char, ok := world.GetComponent(nugget2, characterComponentType())
	if !ok {
		t.Fatal("New nugget should have character")
	}
	charComp := char.(components.CharacterComponent)

	ctx.CursorX = posComp.X
	ctx.CursorY = posComp.Y
	ctx.State.SetCursorX(posComp.X)
	ctx.State.SetCursorY(posComp.Y)

	scoreSystem.HandleCharacterTyping(world, posComp.X, posComp.Y, charComp.Rune)

	// Second collection: heat = 20
	if ctx.State.GetHeat() != 20 {
		t.Errorf("Expected heat 20 after second collection, got %d", ctx.State.GetHeat())
	}
}

// TestNuggetTypingWithSmallScreen verifies minimum heat increase of 1
func TestNuggetTypingWithSmallScreen(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(5, 24)

	ctx := engine.NewGameContext(screen)
	world := ctx.World
	scoreSystem := NewScoreSystem(ctx)
	nuggetSystem := NewNuggetSystem(ctx)
	scoreSystem.SetNuggetSystem(nuggetSystem)

	// Set very small screen width (less than 10)
	ctx.Width = 5
	ctx.GameWidth = 5

	// Create nugget with character 'x'
	nuggetEntity := world.CreateEntity()
	world.AddComponent(nuggetEntity, components.PositionComponent{X: 2, Y: 1})
	style := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
	world.AddComponent(nuggetEntity, components.CharacterComponent{Rune: 'x', Style: style})
	world.AddComponent(nuggetEntity, components.NuggetComponent{ID: 1, SpawnTime: ctx.TimeProvider.Now()})
	world.UpdateSpatialIndex(nuggetEntity, 2, 1)
	nuggetSystem.activeNugget.Store(uint64(nuggetEntity))

	ctx.CursorX = 2
	ctx.CursorY = 1
	ctx.State.SetCursorX(2)
	ctx.State.SetCursorY(1)

	scoreSystem.HandleCharacterTyping(world, 2, 1, 'x')

	// Even with width 5, 10% = 0, but we enforce minimum of 1
	finalHeat := ctx.State.GetHeat()
	if finalHeat < 1 {
		t.Errorf("Heat should increase by at least 1, got %d", finalHeat)
	}
}

// TestNuggetAlwaysIncreasesVisualBlocks verifies collecting a nugget always increases visual heat blocks by 1
func TestNuggetAlwaysIncreasesVisualBlocks(t *testing.T) {
	// Test various screen widths, including those not divisible by 10
	widths := []int{79, 80, 81, 95, 100, 120}

	for _, width := range widths {
		t.Run(string(rune('0'+width/10)), func(t *testing.T) {
			screen := tcell.NewSimulationScreen("UTF-8")
			screen.Init()
			defer screen.Fini()
			screen.SetSize(width, 24)

			ctx := engine.NewGameContext(screen)
			world := ctx.World
			scoreSystem := NewScoreSystem(ctx)
			nuggetSystem := NewNuggetSystem(ctx)
			scoreSystem.SetNuggetSystem(nuggetSystem)

			ctx.Width = width
			ctx.GameWidth = width

			// Helper to calculate visual blocks from heat value
			calcVisualBlocks := func(heat int) int {
				return int(float64(heat) / float64(width) * 10.0)
			}

			// Test from 0 heat
			ctx.State.SetHeat(0)
			initialBlocks := calcVisualBlocks(0)

			// Create and collect nugget
			nuggetEntity := world.CreateEntity()
			world.AddComponent(nuggetEntity, components.PositionComponent{X: 10, Y: 5})
			style := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
			world.AddComponent(nuggetEntity, components.CharacterComponent{Rune: 'a', Style: style})
			world.AddComponent(nuggetEntity, components.NuggetComponent{ID: 1, SpawnTime: ctx.TimeProvider.Now()})
			world.UpdateSpatialIndex(nuggetEntity, 10, 5)
			nuggetSystem.activeNugget.Store(uint64(nuggetEntity))

			ctx.CursorX = 10
			ctx.CursorY = 5
			ctx.State.SetCursorX(10)
			ctx.State.SetCursorY(5)

			scoreSystem.HandleCharacterTyping(world, 10, 5, 'a')

			finalHeat := ctx.State.GetHeat()
			finalBlocks := calcVisualBlocks(finalHeat)

			// Verify at least 1 block increase
			if finalBlocks-initialBlocks < 1 {
				t.Errorf("Width %d: Expected at least 1 visual block increase, got %d blocks (heat %d -> %d)",
					width, finalBlocks-initialBlocks, 0, finalHeat)
			}

			// Should be exactly 1 block increase from 0
			if finalBlocks != 1 {
				t.Errorf("Width %d: Expected exactly 1 visual block from 0 heat, got %d blocks (heat=%d)",
					width, finalBlocks, finalHeat)
			}
		})
	}
}

// Helper functions

func nuggetComponentType() reflect.Type {
	return reflect.TypeOf(components.NuggetComponent{})
}

func positionComponentType() reflect.Type {
	return reflect.TypeOf(components.PositionComponent{})
}

func characterComponentType() reflect.Type {
	return reflect.TypeOf(components.CharacterComponent{})
}
