package systems

import (
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// createVisualizationTestContext creates a test context for drain visualization tests
func createVisualizationTestContext() (*engine.GameContext, *engine.World, *engine.MockTimeProvider) {
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
	return ctx, world, mockTime
}

// getDrainComponent retrieves the drain component from the world
func getDrainComponent(t *testing.T, world *engine.World, entityID uint64) components.DrainComponent {
	entity := engine.Entity(entityID)
	// Using direct store access
	drainComp, ok := world.Drains.Get(entity)
	if !ok {
		t.Fatal("Drain component not found")
	}
	return drainComp
}

// TestDrainSystem_VisualizationYCoordinateSync verifies that Drain Y coordinate
// in GameState stays synchronized with actual drain position
func TestDrainSystem_VisualizationYCoordinateSync(t *testing.T) {
	ctx, world, _ := createVisualizationTestContext()
	drainSystem := NewDrainSystem(ctx)

	// Set cursor at a specific Y position (not middle of screen)
	cursorX, cursorY := 10, 5
	ctx.State.SetCursorX(cursorX)
	ctx.State.SetCursorY(cursorY)

	// Add energy to trigger drain spawn
	ctx.State.AddEnergy(100)

	// Run drain system to spawn drain
	drainSystem.Update(world, time.Millisecond*16)

	// Verify drain spawned at cursor Y position in GameState
	drainY := ctx.State.GetDrainY()
	if drainY != cursorY {
		t.Errorf("Drain should spawn at cursor Y=%d, but GameState.DrainY=%d", cursorY, drainY)
	}

	// Verify drain component position matches
	drainComp := getDrainComponent(t, world, ctx.State.GetDrainEntity())
	if drainComp.Y != cursorY {
		t.Errorf("Drain component Y should be %d, got %d", cursorY, drainComp.Y)
	}
}

// TestDrainSystem_VisualizationFollowsCursor verifies Drain Y coordinate
// updates when drain moves toward cursor at a different Y position
func TestDrainSystem_VisualizationFollowsCursor(t *testing.T) {
	ctx, world, mockTime := createVisualizationTestContext()
	drainSystem := NewDrainSystem(ctx)

	// Set cursor at initial position
	initialY := 10
	ctx.State.SetCursorX(10)
	ctx.State.SetCursorY(initialY)

	// Add energy to trigger drain spawn
	ctx.State.AddEnergy(100)

	// Spawn drain
	drainSystem.Update(world, time.Millisecond*16)

	// Move cursor to different Y position
	newCursorY := 20
	ctx.State.SetCursorY(newCursorY)

	// Advance time to trigger drain movement
	mockTime.Advance(constants.DrainMoveInterval)

	// Run drain system to process movement
	drainSystem.Update(world, time.Millisecond*16)

	// Verify drain moved toward new cursor Y
	drainY := ctx.State.GetDrainY()
	if drainY == initialY {
		t.Errorf("Drain should have moved from Y=%d toward cursor at Y=%d, but stayed at Y=%d",
			initialY, newCursorY, drainY)
	}

	// Drain should have moved 1 step closer (down) since cursor is below
	expectedY := initialY + 1
	if drainY != expectedY {
		t.Errorf("Drain should have moved to Y=%d (one step toward cursor), but GameState.DrainY=%d",
			expectedY, drainY)
	}

	// Verify component and GameState are in sync
	drainComp := getDrainComponent(t, world, ctx.State.GetDrainEntity())
	if drainComp.Y != drainY {
		t.Errorf("Drain component Y=%d does not match GameState.DrainY=%d", drainComp.Y, drainY)
	}
}

// TestDrainSystem_VisualizationAtScreenEdges verifies Drain Y coordinate
// works correctly at screen edges (top and bottom rows)
func TestDrainSystem_VisualizationAtScreenEdges(t *testing.T) {
	tests := []struct {
		name    string
		cursorY int
		desc    string
	}{
		{"TopRow", 0, "top row"},
		{"BottomRow", 23, "bottom row"},
		{"Row5", 5, "row 5"},
		{"Row18", 18, "row 18"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, world, _ := createVisualizationTestContext()
			drainSystem := NewDrainSystem(ctx)

			// Set cursor at test Y position
			ctx.State.SetCursorX(10)
			ctx.State.SetCursorY(tt.cursorY)

			// Add energy to trigger drain spawn
			ctx.State.AddEnergy(100)

			// Spawn drain
			drainSystem.Update(world, time.Millisecond*16)

			// Verify drain spawned at cursor Y
			drainY := ctx.State.GetDrainY()
			if drainY != tt.cursorY {
				t.Errorf("Drain at %s (Y=%d): GameState.DrainY=%d, expected %d",
					tt.desc, tt.cursorY, drainY, tt.cursorY)
			}

			// Verify component matches
			drainComp := getDrainComponent(t, world, ctx.State.GetDrainEntity())
			if drainComp.Y != tt.cursorY {
				t.Errorf("Drain component at %s: Y=%d, expected %d",
					tt.desc, drainComp.Y, tt.cursorY)
			}
		})
	}
}

// TestDrainSystem_VisualizationXYBothUpdate verifies both X and Y coordinates
// update correctly when drain moves diagonally
func TestDrainSystem_VisualizationXYBothUpdate(t *testing.T) {
	ctx, world, mockTime := createVisualizationTestContext()
	drainSystem := NewDrainSystem(ctx)

	// Set cursor at initial position
	initialX, initialY := 10, 10
	ctx.State.SetCursorX(initialX)
	ctx.State.SetCursorY(initialY)

	// Add energy and spawn drain
	ctx.State.AddEnergy(100)
	drainSystem.Update(world, time.Millisecond*16)

	// Move cursor to diagonal position (northeast)
	newCursorX, newCursorY := 15, 5
	ctx.State.SetCursorX(newCursorX)
	ctx.State.SetCursorY(newCursorY)

	// Advance time and process movement
	mockTime.Advance(constants.DrainMoveInterval)
	drainSystem.Update(world, time.Millisecond*16)

	// Verify drain moved diagonally (both X and Y changed)
	drainX := ctx.State.GetDrainX()
	drainY := ctx.State.GetDrainY()

	// Drain should have moved one step toward cursor (right and up)
	expectedX := initialX + 1
	expectedY := initialY - 1

	if drainX != expectedX {
		t.Errorf("Drain X should be %d (moved right), got %d", expectedX, drainX)
	}
	if drainY != expectedY {
		t.Errorf("Drain Y should be %d (moved up), got %d", expectedY, drainY)
	}

	// Verify component and GameState are in sync
	drainComp := getDrainComponent(t, world, ctx.State.GetDrainEntity())
	if drainComp.X != drainX || drainComp.Y != drainY {
		t.Errorf("Drain component position (%d,%d) does not match GameState (%d,%d)",
			drainComp.X, drainComp.Y, drainX, drainY)
	}
}

// TestDrainSystem_VisualizationNeverStuckAtMiddle verifies drain doesn't get
// stuck at middle row (gameHeight/2) when cursor is elsewhere
func TestDrainSystem_VisualizationNeverStuckAtMiddle(t *testing.T) {
	ctx, world, _ := createVisualizationTestContext()
	drainSystem := NewDrainSystem(ctx)

	middleY := ctx.GameHeight / 2

	// Test multiple cursor positions that are NOT at middle
	testPositions := []int{0, 3, 7, 15, 20, ctx.GameHeight - 1}

	for _, cursorY := range testPositions {
		if cursorY == middleY {
			continue // Skip middle position
		}

		// Reset state
		ctx.State.SetDrainActive(false)
		ctx.State.SetEnergy(0)

		// Set cursor at non-middle position
		ctx.State.SetCursorX(10)
		ctx.State.SetCursorY(cursorY)

		// Add energy and spawn drain
		ctx.State.AddEnergy(100)
		drainSystem.Update(world, time.Millisecond*16)

		// Verify drain is NOT stuck at middle
		drainY := ctx.State.GetDrainY()
		if drainY == middleY && cursorY != middleY {
			t.Errorf("BUG: Drain stuck at middle Y=%d when cursor is at Y=%d",
				middleY, cursorY)
		}

		// Verify drain spawned at cursor position
		if drainY != cursorY {
			t.Errorf("Drain should spawn at cursor Y=%d, got Y=%d", cursorY, drainY)
		}
	}
}