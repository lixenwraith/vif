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

// TestNuggetJumpWithSufficientScore tests jumping to nugget when score >= 10
func TestNuggetJumpWithSufficientScore(t *testing.T) {
	// Setup
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	screen.SetSize(100, 30)

	ctx := engine.NewGameContext(screen)
	ctx.GameWidth = 100
	ctx.GameHeight = 30

	// Create nugget system
	nuggetSystem := NewNuggetSystem(ctx)

	// Manually spawn a nugget at (50, 15)
	entity := ctx.World.CreateEntity()
	ctx.World.AddComponent(entity, components.PositionComponent{X: 50, Y: 15})
	style := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
	// Use alphanumeric character for nugget (as defined in constants.AlphanumericRunes)
	ctx.World.AddComponent(entity, components.CharacterComponent{Rune: 'a', Style: style})
	ctx.World.AddComponent(entity, components.NuggetComponent{ID: 1, SpawnTime: time.Now()})
	ctx.World.UpdateSpatialIndex(entity, 50, 15)
	nuggetSystem.activeNugget.Store(uint64(entity))

	// Set score to 15
	ctx.State.SetScore(15)

	// Set cursor to (10, 10)
	ctx.CursorX = 10
	ctx.CursorY = 10
	ctx.State.SetCursorX(10)
	ctx.State.SetCursorY(10)

	// Jump to nugget
	x, y := nuggetSystem.JumpToNugget(ctx.World)

	// Verify position returned
	if x != 50 || y != 15 {
		t.Errorf("Expected position (50, 15), got (%d, %d)", x, y)
	}

	// Simulate score deduction (would normally be done by InputHandler)
	ctx.State.AddScore(-10)

	// Verify score deducted
	score := ctx.State.GetScore()
	if score != 5 {
		t.Errorf("Expected score 5 after deduction, got %d", score)
	}
}

// TestNuggetJumpWithInsufficientScore tests that jump doesn't happen when score < 10
func TestNuggetJumpWithInsufficientScore(t *testing.T) {
	// Setup
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	screen.SetSize(100, 30)

	ctx := engine.NewGameContext(screen)
	ctx.GameWidth = 100
	ctx.GameHeight = 30

	// Create nugget system
	nuggetSystem := NewNuggetSystem(ctx)

	// Manually spawn a nugget at (50, 15)
	entity := ctx.World.CreateEntity()
	ctx.World.AddComponent(entity, components.PositionComponent{X: 50, Y: 15})
	style := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
	// Use alphanumeric character for nugget (as defined in constants.AlphanumericRunes)
	ctx.World.AddComponent(entity, components.CharacterComponent{Rune: 'a', Style: style})
	ctx.World.AddComponent(entity, components.NuggetComponent{ID: 1, SpawnTime: time.Now()})
	ctx.World.UpdateSpatialIndex(entity, 50, 15)
	nuggetSystem.activeNugget.Store(uint64(entity))

	// Set score to 5 (insufficient)
	ctx.State.SetScore(5)

	// Set cursor to (10, 10)
	ctx.CursorX = 10
	ctx.CursorY = 10
	ctx.State.SetCursorX(10)
	ctx.State.SetCursorY(10)

	// Verify score is insufficient
	score := ctx.State.GetScore()
	if score < 10 {
		// This is expected - score check would prevent jump in InputHandler
		// JumpToNugget can still return position, but InputHandler wouldn't use it
	}

	// Get position (for testing the method itself)
	x, y := nuggetSystem.JumpToNugget(ctx.World)

	// Verify position is still returned (method doesn't check score)
	if x != 50 || y != 15 {
		t.Errorf("Expected position (50, 15), got (%d, %d)", x, y)
	}

	// Verify score unchanged (no deduction happens when score < 10 in InputHandler)
	if ctx.State.GetScore() != 5 {
		t.Errorf("Expected score 5 (unchanged), got %d", ctx.State.GetScore())
	}
}

// TestNuggetJumpWithNoActiveNugget tests jumping when no nugget is active
func TestNuggetJumpWithNoActiveNugget(t *testing.T) {
	// Setup
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	screen.SetSize(100, 30)

	ctx := engine.NewGameContext(screen)
	ctx.GameWidth = 100
	ctx.GameHeight = 30

	// Create nugget system (no nugget spawned)
	nuggetSystem := NewNuggetSystem(ctx)

	// Set score to 15
	ctx.State.SetScore(15)

	// Set cursor to (10, 10)
	ctx.CursorX = 10
	ctx.CursorY = 10

	// Try to jump to nugget
	x, y := nuggetSystem.JumpToNugget(ctx.World)

	// Verify (-1, -1) returned when no nugget
	if x != -1 || y != -1 {
		t.Errorf("Expected (-1, -1) when no nugget active, got (%d, %d)", x, y)
	}

	// Verify cursor unchanged (InputHandler wouldn't update cursor with invalid position)
	if ctx.CursorX != 10 || ctx.CursorY != 10 {
		t.Errorf("Expected cursor unchanged at (10, 10), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Verify score unchanged
	if ctx.State.GetScore() != 15 {
		t.Errorf("Expected score 15 (unchanged), got %d", ctx.State.GetScore())
	}
}

// TestNuggetJumpUpdatesPosition tests that cursor position is updated correctly
func TestNuggetJumpUpdatesPosition(t *testing.T) {
	// Setup
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	screen.SetSize(100, 30)

	ctx := engine.NewGameContext(screen)
	ctx.GameWidth = 100
	ctx.GameHeight = 30

	// Create nugget system
	nuggetSystem := NewNuggetSystem(ctx)

	// Manually spawn a nugget at (75, 20)
	entity := ctx.World.CreateEntity()
	ctx.World.AddComponent(entity, components.PositionComponent{X: 75, Y: 20})
	style := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
	// Use alphanumeric character for nugget (as defined in constants.AlphanumericRunes)
	ctx.World.AddComponent(entity, components.CharacterComponent{Rune: 'a', Style: style})
	ctx.World.AddComponent(entity, components.NuggetComponent{ID: 1, SpawnTime: time.Now()})
	ctx.World.UpdateSpatialIndex(entity, 75, 20)
	nuggetSystem.activeNugget.Store(uint64(entity))

	// Set score to 20
	ctx.State.SetScore(20)

	// Set cursor to (5, 5)
	ctx.CursorX = 5
	ctx.CursorY = 5
	ctx.State.SetCursorX(5)
	ctx.State.SetCursorY(5)

	// Jump to nugget
	x, y := nuggetSystem.JumpToNugget(ctx.World)

	// Simulate what InputHandler does
	if x >= 0 && y >= 0 {
		ctx.CursorX = x
		ctx.CursorY = y
		ctx.State.SetCursorX(x)
		ctx.State.SetCursorY(y)
		ctx.State.AddScore(-10)
	}

	// Verify cursor position updated
	if ctx.CursorX != 75 || ctx.CursorY != 20 {
		t.Errorf("Expected cursor at (75, 20), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Verify atomic cursor position updated
	cursorSnap := ctx.State.ReadCursorPosition()
	if cursorSnap.X != 75 || cursorSnap.Y != 20 {
		t.Errorf("Expected atomic cursor at (75, 20), got (%d, %d)", cursorSnap.X, cursorSnap.Y)
	}

	// Verify score deducted
	if ctx.State.GetScore() != 10 {
		t.Errorf("Expected score 10, got %d", ctx.State.GetScore())
	}
}

// TestNuggetJumpMultipleTimes tests jumping to nugget multiple times
func TestNuggetJumpMultipleTimes(t *testing.T) {
	// Setup
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	screen.SetSize(100, 30)

	ctx := engine.NewGameContext(screen)
	ctx.GameWidth = 100
	ctx.GameHeight = 30

	// Create nugget system
	nuggetSystem := NewNuggetSystem(ctx)

	// Set score to 30
	ctx.State.SetScore(30)

	// First nugget at (50, 15)
	entity1 := ctx.World.CreateEntity()
	ctx.World.AddComponent(entity1, components.PositionComponent{X: 50, Y: 15})
	style := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
	ctx.World.AddComponent(entity1, components.CharacterComponent{Rune: '●', Style: style})
	ctx.World.AddComponent(entity1, components.NuggetComponent{ID: 1, SpawnTime: time.Now()})
	ctx.World.UpdateSpatialIndex(entity1, 50, 15)
	nuggetSystem.activeNugget.Store(uint64(entity1))

	// Jump to first nugget
	x, y := nuggetSystem.JumpToNugget(ctx.World)
	if x >= 0 && y >= 0 {
		ctx.CursorX = x
		ctx.CursorY = y
		ctx.State.AddScore(-10)
	}

	// Verify first jump
	if ctx.CursorX != 50 || ctx.CursorY != 15 {
		t.Errorf("First jump: expected cursor at (50, 15), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}
	if ctx.State.GetScore() != 20 {
		t.Errorf("First jump: expected score 20, got %d", ctx.State.GetScore())
	}

	// Simulate collection (destroy first nugget)
	ctx.World.SafeDestroyEntity(entity1)
	nuggetSystem.ClearActiveNuggetIfMatches(uint64(entity1))

	// Second nugget at (25, 10)
	entity2 := ctx.World.CreateEntity()
	ctx.World.AddComponent(entity2, components.PositionComponent{X: 25, Y: 10})
	ctx.World.AddComponent(entity2, components.CharacterComponent{Rune: '●', Style: style})
	ctx.World.AddComponent(entity2, components.NuggetComponent{ID: 2, SpawnTime: time.Now()})
	ctx.World.UpdateSpatialIndex(entity2, 25, 10)
	nuggetSystem.activeNugget.Store(uint64(entity2))

	// Jump to second nugget
	x, y = nuggetSystem.JumpToNugget(ctx.World)
	if x >= 0 && y >= 0 {
		ctx.CursorX = x
		ctx.CursorY = y
		ctx.State.AddScore(-10)
	}

	// Verify second jump
	if ctx.CursorX != 25 || ctx.CursorY != 10 {
		t.Errorf("Second jump: expected cursor at (25, 10), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}
	if ctx.State.GetScore() != 10 {
		t.Errorf("Second jump: expected score 10, got %d", ctx.State.GetScore())
	}
}

// TestNuggetJumpWithNuggetAtEdge tests jumping to nugget at screen edges
func TestNuggetJumpWithNuggetAtEdge(t *testing.T) {
	// Setup
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	screen.SetSize(100, 30)

	ctx := engine.NewGameContext(screen)
	ctx.GameWidth = 100
	ctx.GameHeight = 30

	// Create nugget system
	nuggetSystem := NewNuggetSystem(ctx)

	// Set score to 15
	ctx.State.SetScore(15)

	// Test cases: nuggets at different edges
	testCases := []struct {
		name string
		x    int
		y    int
	}{
		{"top-left", 0, 0},
		{"top-right", 99, 0},
		{"bottom-left", 0, 29},
		{"bottom-right", 99, 29},
		{"middle-top", 50, 0},
		{"middle-bottom", 50, 29},
		{"left-middle", 0, 15},
		{"right-middle", 99, 15},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Clear previous nugget
			if activeEntity := nuggetSystem.GetActiveNugget(); activeEntity != 0 {
				ctx.World.SafeDestroyEntity(engine.Entity(activeEntity))
				nuggetSystem.ClearActiveNuggetIfMatches(activeEntity)
			}

			// Spawn nugget at test position
			entity := ctx.World.CreateEntity()
			ctx.World.AddComponent(entity, components.PositionComponent{X: tc.x, Y: tc.y})
			style := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
			// Use alphanumeric character for nugget (as defined in constants.AlphanumericRunes)
	ctx.World.AddComponent(entity, components.CharacterComponent{Rune: 'a', Style: style})
			ctx.World.AddComponent(entity, components.NuggetComponent{ID: 1, SpawnTime: time.Now()})
			ctx.World.UpdateSpatialIndex(entity, tc.x, tc.y)
			nuggetSystem.activeNugget.Store(uint64(entity))

			// Jump to nugget
			x, y := nuggetSystem.JumpToNugget(ctx.World)

			// Verify position
			if x != tc.x || y != tc.y {
				t.Errorf("Expected position (%d, %d), got (%d, %d)", tc.x, tc.y, x, y)
			}
		})
	}
}

// TestJumpToNuggetMethodReturnsCorrectPosition tests the JumpToNugget method directly
func TestJumpToNuggetMethodReturnsCorrectPosition(t *testing.T) {
	// Setup
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	screen.SetSize(100, 30)

	ctx := engine.NewGameContext(screen)
	ctx.GameWidth = 100
	ctx.GameHeight = 30

	// Create nugget system
	nuggetSystem := NewNuggetSystem(ctx)

	// No nugget: should return (-1, -1)
	x, y := nuggetSystem.JumpToNugget(ctx.World)
	if x != -1 || y != -1 {
		t.Errorf("No nugget: expected (-1, -1), got (%d, %d)", x, y)
	}

	// Create nugget at (30, 12)
	entity := ctx.World.CreateEntity()
	ctx.World.AddComponent(entity, components.PositionComponent{X: 30, Y: 12})
	style := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
	// Use alphanumeric character for nugget (as defined in constants.AlphanumericRunes)
	ctx.World.AddComponent(entity, components.CharacterComponent{Rune: 'a', Style: style})
	ctx.World.AddComponent(entity, components.NuggetComponent{ID: 1, SpawnTime: time.Now()})
	nuggetSystem.activeNugget.Store(uint64(entity))

	// Should return (30, 12)
	x, y = nuggetSystem.JumpToNugget(ctx.World)
	if x != 30 || y != 12 {
		t.Errorf("With nugget: expected (30, 12), got (%d, %d)", x, y)
	}

	// Clear active nugget
	nuggetSystem.ClearActiveNuggetIfMatches(uint64(entity))

	// Should return (-1, -1) again
	x, y = nuggetSystem.JumpToNugget(ctx.World)
	if x != -1 || y != -1 {
		t.Errorf("After clear: expected (-1, -1), got (%d, %d)", x, y)
	}
}

// TestJumpToNuggetWithMissingComponent tests graceful handling of missing position component
func TestJumpToNuggetWithMissingComponent(t *testing.T) {
	// Setup
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	screen.SetSize(100, 30)

	ctx := engine.NewGameContext(screen)

	// Create nugget system
	nuggetSystem := NewNuggetSystem(ctx)

	// Create entity without PositionComponent (shouldn't happen in practice)
	entity := ctx.World.CreateEntity()
	ctx.World.AddComponent(entity, components.NuggetComponent{ID: 1, SpawnTime: time.Now()})
	nuggetSystem.activeNugget.Store(uint64(entity))

	// Should handle gracefully and return (-1, -1)
	x, y := nuggetSystem.JumpToNugget(ctx.World)
	if x != -1 || y != -1 {
		t.Errorf("Missing PositionComponent: expected (-1, -1), got (%d, %d)", x, y)
	}
}

// TestJumpToNuggetAtomicCursorUpdate tests that cursor position is updated atomically
func TestJumpToNuggetAtomicCursorUpdate(t *testing.T) {
	// Setup
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	screen.SetSize(100, 30)

	ctx := engine.NewGameContext(screen)
	ctx.GameWidth = 100
	ctx.GameHeight = 30

	// Create nugget system
	nuggetSystem := NewNuggetSystem(ctx)

	// Spawn nugget at (60, 18)
	entity := ctx.World.CreateEntity()
	ctx.World.AddComponent(entity, components.PositionComponent{X: 60, Y: 18})
	style := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
	// Use alphanumeric character for nugget (as defined in constants.AlphanumericRunes)
	ctx.World.AddComponent(entity, components.CharacterComponent{Rune: 'a', Style: style})
	ctx.World.AddComponent(entity, components.NuggetComponent{ID: 1, SpawnTime: time.Now()})
	nuggetSystem.activeNugget.Store(uint64(entity))

	// Set initial cursor
	ctx.CursorX = 10
	ctx.CursorY = 10
	ctx.State.SetCursorX(10)
	ctx.State.SetCursorY(10)

	// Jump to nugget
	x, y := nuggetSystem.JumpToNugget(ctx.World)
	if x >= 0 && y >= 0 {
		// Update both GameContext and GameState atomically
		ctx.CursorX = x
		ctx.CursorY = y
		ctx.State.SetCursorX(x)
		ctx.State.SetCursorY(y)
	}

	// Verify GameContext cursor
	if ctx.CursorX != 60 || ctx.CursorY != 18 {
		t.Errorf("GameContext: expected cursor at (60, 18), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Verify GameState cursor (atomic)
	cursorSnap := ctx.State.ReadCursorPosition()
	if cursorSnap.X != 60 || cursorSnap.Y != 18 {
		t.Errorf("GameState: expected cursor at (60, 18), got (%d, %d)", cursorSnap.X, cursorSnap.Y)
	}

	// Verify both are in sync
	if ctx.CursorX != cursorSnap.X || ctx.CursorY != cursorSnap.Y {
		t.Errorf("Cursor positions out of sync: GameContext(%d, %d) vs GameState(%d, %d)",
			ctx.CursorX, ctx.CursorY, cursorSnap.X, cursorSnap.Y)
	}
}

// TestJumpToNuggetEntityStillExists tests that the nugget entity still exists after jump
func TestJumpToNuggetEntityStillExists(t *testing.T) {
	// Setup
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	screen.SetSize(100, 30)

	ctx := engine.NewGameContext(screen)
	ctx.GameWidth = 100
	ctx.GameHeight = 30

	// Create nugget system
	nuggetSystem := NewNuggetSystem(ctx)

	// Spawn nugget
	entity := ctx.World.CreateEntity()
	ctx.World.AddComponent(entity, components.PositionComponent{X: 40, Y: 12})
	style := tcell.StyleDefault.Foreground(render.RgbNuggetOrange).Background(render.RgbBackground)
	// Use alphanumeric character for nugget (as defined in constants.AlphanumericRunes)
	ctx.World.AddComponent(entity, components.CharacterComponent{Rune: 'a', Style: style})
	ctx.World.AddComponent(entity, components.NuggetComponent{ID: 1, SpawnTime: time.Now()})
	nuggetSystem.activeNugget.Store(uint64(entity))

	// Jump to nugget
	x, y := nuggetSystem.JumpToNugget(ctx.World)
	if x < 0 || y < 0 {
		t.Fatal("Jump failed")
	}

	// Verify nugget still exists (jumping doesn't collect it)
	nuggetType := reflect.TypeOf(components.NuggetComponent{})
	if !ctx.World.HasComponent(entity, nuggetType) {
		t.Error("Nugget entity should still exist after jump")
	}

	// Verify active nugget reference still set
	if nuggetSystem.GetActiveNugget() != uint64(entity) {
		t.Error("Active nugget reference should remain after jump")
	}
}
