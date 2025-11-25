package systems

import (
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestDrainSystem_MovementAfterInterval tests that drain moves after DrainMoveInterval
func TestDrainSystem_MovementAfterInterval(t *testing.T) {
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

	drainSys := NewDrainSystem(ctx)

	// Set cursor to (0, 0) so drain spawns there
	ctx.State.SetCursorX(0)
	ctx.State.SetCursorY(0)

	// Spawn drain at cursor position (0, 0)
	ctx.State.SetEnergy(100)
	drainSys.Update(world, 16*time.Millisecond)

	if !ctx.State.GetDrainActive() {
		t.Fatal("Expected drain to be active")
	}

	// Verify drain spawned at cursor position (0, 0)
	if ctx.State.GetDrainX() != 0 || ctx.State.GetDrainY() != 0 {
		t.Errorf("Expected drain to spawn at (0, 0), got (%d, %d)",
			ctx.State.GetDrainX(), ctx.State.GetDrainY())
	}

	// Set cursor to (5, 3) - drain should move toward it
	ctx.State.SetCursorX(5)
	ctx.State.SetCursorY(3)

	// Advance time by less than interval (not enough for movement)
	mockTime.Advance(constants.DrainMoveInterval / 2)
	drainSys.Update(world, 16*time.Millisecond)

	// Drain should NOT have moved yet
	if ctx.State.GetDrainX() != 0 || ctx.State.GetDrainY() != 0 {
		t.Errorf("Expected drain to still be at (0, 0) before interval, got (%d, %d)",
			ctx.State.GetDrainX(), ctx.State.GetDrainY())
	}

	// Advance time to complete the interval
	mockTime.Advance(constants.DrainMoveInterval/2 + 1*time.Millisecond)
	drainSys.Update(world, 16*time.Millisecond)

	// Drain should have moved one step toward cursor (1, 1) - diagonal movement
	expectedX, expectedY := 1, 1
	actualX := ctx.State.GetDrainX()
	actualY := ctx.State.GetDrainY()

	if actualX != expectedX || actualY != expectedY {
		t.Errorf("Expected drain to move to (%d, %d) after interval, got (%d, %d)",
			expectedX, expectedY, actualX, actualY)
	}
}

// TestDrainSystem_MovementNoMoveBeforeInterval tests that drain doesn't move before interval
func TestDrainSystem_MovementNoMoveBeforeInterval(t *testing.T) {
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

	drainSys := NewDrainSystem(ctx)

	// Spawn drain
	ctx.State.SetEnergy(100)
	drainSys.Update(world, 16*time.Millisecond)

	initialX := ctx.State.GetDrainX()
	initialY := ctx.State.GetDrainY()

	// Set cursor far away
	ctx.State.SetCursorX(50)
	ctx.State.SetCursorY(20)

	// Advance time by various amounts less than DrainMoveInterval
	intervals := []time.Duration{
		constants.DrainMoveInterval / 20,
		constants.DrainMoveInterval / 10,
		constants.DrainMoveInterval / 5,
		constants.DrainMoveInterval - 1*time.Millisecond,
	}

	for _, interval := range intervals {
		mockTime.SetTime(startTime)
		mockTime.Advance(interval)
		drainSys.Update(world, 16*time.Millisecond)

		if ctx.State.GetDrainX() != initialX || ctx.State.GetDrainY() != initialY {
			t.Errorf("Drain moved before DrainMoveInterval at %v", interval)
		}
	}
}

// TestDrainSystem_MovementBoundaryChecks tests that drain respects game boundaries
func TestDrainSystem_MovementBoundaryChecks(t *testing.T) {
	testCases := []struct {
		name        string
		gameWidth   int
		gameHeight  int
		drainX      int
		drainY      int
		cursorX     int
		cursorY     int
		expectedX   int
		expectedY   int
		description string
	}{
		{
			name:        "Top-left corner moving toward top-left",
			gameWidth:   80,
			gameHeight:  24,
			drainX:      0,
			drainY:      0,
			cursorX:     -10, // Cursor "outside" bounds
			cursorY:     -10,
			expectedX:   0, // Should stay at 0 (boundary)
			expectedY:   0,
			description: "Drain at top-left should not move beyond boundaries",
		},
		{
			name:        "Bottom-right corner moving toward bottom-right",
			gameWidth:   80,
			gameHeight:  24,
			drainX:      79,
			drainY:      23,
			cursorX:     100,
			cursorY:     50,
			expectedX:   79, // Should stay at max (boundary)
			expectedY:   23,
			description: "Drain at bottom-right should not move beyond boundaries",
		},
		{
			name:        "Right edge moving right",
			gameWidth:   80,
			gameHeight:  24,
			drainX:      79,
			drainY:      10,
			cursorX:     100,
			cursorY:     10,
			expectedX:   79, // Should stay at max X
			expectedY:   10,
			description: "Drain at right edge should not exceed width",
		},
		{
			name:        "Bottom edge moving down",
			gameWidth:   80,
			gameHeight:  24,
			drainX:      40,
			drainY:      23,
			cursorX:     40,
			cursorY:     50,
			expectedX:   40,
			expectedY:   23, // Should stay at max Y
			description: "Drain at bottom edge should not exceed height",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			startTime := time.Now()
			mockTime := engine.NewMockTimeProvider(startTime)

			world := engine.NewWorld()
			state := engine.NewGameState(tc.gameWidth, tc.gameHeight, tc.gameWidth, mockTime)
			ctx := &engine.GameContext{
				World:        world,
				State:        state,
				TimeProvider: mockTime,
				GameWidth:    tc.gameWidth,
				GameHeight:   tc.gameHeight,
				Width:        tc.gameWidth,
				Height:       tc.gameHeight,
				CursorX:      0,
				CursorY:      0,
			}

			// Manually create drain entity at specific position
			entity := world.CreateEntity()
			world.Positions.Add(entity, components.PositionComponent{X: tc.drainX, Y: tc.drainY})
			world.Drains.Add(entity, components.DrainComponent{
				X:            tc.drainX,
				Y:            tc.drainY,
				LastMoveTime: startTime,
			})

			tx := world.BeginSpatialTransaction()
			tx.Spawn(entity, tc.drainX, tc.drainY)
			tx.Commit()

			ctx.State.SetDrainActive(true)
			ctx.State.SetDrainEntity(uint64(entity))
			ctx.State.SetDrainX(tc.drainX)
			ctx.State.SetDrainY(tc.drainY)
			ctx.State.SetEnergy(100) // Keep drain active

			// Set cursor position
			ctx.State.SetCursorX(tc.cursorX)
			ctx.State.SetCursorY(tc.cursorY)

			// Advance time by DrainMoveInterval to trigger movement
			mockTime.Advance(constants.DrainMoveInterval)

			drainSys := NewDrainSystem(ctx)
			drainSys.Update(world, 16*time.Millisecond)

			// Verify position stayed within bounds
			actualX := ctx.State.GetDrainX()
			actualY := ctx.State.GetDrainY()

			if actualX != tc.expectedX || actualY != tc.expectedY {
				t.Errorf("%s: Expected drain at (%d, %d), got (%d, %d)",
					tc.description, tc.expectedX, tc.expectedY, actualX, actualY)
			}

			// Verify position is within valid bounds
			if actualX < 0 || actualX >= tc.gameWidth || actualY < 0 || actualY >= tc.gameHeight {
				t.Errorf("Drain position out of bounds: (%d, %d), bounds: [0-%d, 0-%d]",
					actualX, actualY, tc.gameWidth-1, tc.gameHeight-1)
			}
		})
	}
}

// TestDrainSystem_MovementUpdatesAllComponents tests that all position fields are updated
func TestDrainSystem_MovementUpdatesAllComponents(t *testing.T) {
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

	drainSys := NewDrainSystem(ctx)

	// Spawn drain
	ctx.State.SetEnergy(100)
	drainSys.Update(world, 16*time.Millisecond)

	entityID := ctx.State.GetDrainEntity()
	entity := engine.Entity(entityID)

	// Set cursor to trigger movement
	ctx.State.SetCursorX(10)
	ctx.State.SetCursorY(10)

	// Advance time to trigger movement
	mockTime.Advance(constants.DrainMoveInterval)
	drainSys.Update(world, 16*time.Millisecond)

	// Verify DrainComponent was updated
	// Using direct store access
	drainComp, ok := world.Drains.Get(entity)
	if !ok {
		t.Fatal("Expected entity to have DrainComponent")
	}
	drain := drainComp

	// Verify PositionComponent was updated
	posComp, ok := world.Positions.Get(entity)
	if !ok {
		t.Fatal("Expected entity to have PositionComponent")
	}
	pos := posComp

	// All position fields should match
	stateX := ctx.State.GetDrainX()
	stateY := ctx.State.GetDrainY()

	if drain.X != stateX || drain.Y != stateY {
		t.Errorf("DrainComponent position (%d, %d) doesn't match GameState (%d, %d)",
			drain.X, drain.Y, stateX, stateY)
	}

	if pos.X != stateX || pos.Y != stateY {
		t.Errorf("PositionComponent position (%d, %d) doesn't match GameState (%d, %d)",
			pos.X, pos.Y, stateX, stateY)
	}

	// Verify LastMoveTime was updated
	if drain.LastMoveTime == startTime {
		t.Error("Expected LastMoveTime to be updated after movement")
	}
}

// TestDrainSystem_MovementUpdatesSpatialIndex tests spatial index updates correctly
func TestDrainSystem_MovementUpdatesSpatialIndex(t *testing.T) {
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

	drainSys := NewDrainSystem(ctx)

	// Spawn drain
	ctx.State.SetEnergy(100)
	drainSys.Update(world, 16*time.Millisecond)

	entityID := ctx.State.GetDrainEntity()
	entity := engine.Entity(entityID)

	// Get initial position
	initialX := ctx.State.GetDrainX()
	initialY := ctx.State.GetDrainY()

	// Verify entity is in spatial index at initial position
	spatialEntity := world.GetEntityAtPosition(initialX, initialY)
	if spatialEntity != entity {
		t.Errorf("Expected entity at initial position (%d, %d), got %v",
			initialX, initialY, spatialEntity)
	}

	// Set cursor to trigger movement
	ctx.State.SetCursorX(initialX + 5)
	ctx.State.SetCursorY(initialY + 5)

	// Advance time to trigger movement
	mockTime.Advance(constants.DrainMoveInterval)
	drainSys.Update(world, 16*time.Millisecond)

	// Get new position
	newX := ctx.State.GetDrainX()
	newY := ctx.State.GetDrainY()

	// Verify entity moved
	if newX == initialX && newY == initialY {
		t.Fatal("Expected drain to move")
	}

	// Verify entity is NOT in spatial index at old position
	oldPosEntity := world.GetEntityAtPosition(initialX, initialY)
	if oldPosEntity == entity {
		t.Errorf("Entity should not be at old position (%d, %d) in spatial index",
			initialX, initialY)
	}

	// Verify entity IS in spatial index at new position
	newPosEntity := world.GetEntityAtPosition(newX, newY)
	if newPosEntity != entity {
		t.Errorf("Expected entity at new position (%d, %d), got %v",
			newX, newY, newPosEntity)
	}
}

// TestDrainSystem_MovementDiagonal tests diagonal movement toward cursor
func TestDrainSystem_MovementDiagonal(t *testing.T) {
	testCases := []struct {
		name      string
		drainX    int
		drainY    int
		cursorX   int
		cursorY   int
		expectedX int
		expectedY int
	}{
		{
			name:      "Move northeast",
			drainX:    10,
			drainY:    10,
			cursorX:   15,
			cursorY:   5,
			expectedX: 11, // +1 in X
			expectedY: 9,  // -1 in Y
		},
		{
			name:      "Move southeast",
			drainX:    10,
			drainY:    10,
			cursorX:   15,
			cursorY:   15,
			expectedX: 11, // +1 in X
			expectedY: 11, // +1 in Y
		},
		{
			name:      "Move northwest",
			drainX:    10,
			drainY:    10,
			cursorX:   5,
			cursorY:   5,
			expectedX: 9, // -1 in X
			expectedY: 9, // -1 in Y
		},
		{
			name:      "Move southwest",
			drainX:    10,
			drainY:    10,
			cursorX:   5,
			cursorY:   15,
			expectedX: 9,  // -1 in X
			expectedY: 11, // +1 in Y
		},
		{
			name:      "Move north only",
			drainX:    10,
			drainY:    10,
			cursorX:   10,
			cursorY:   5,
			expectedX: 10, // No X change
			expectedY: 9,  // -1 in Y
		},
		{
			name:      "Move east only",
			drainX:    10,
			drainY:    10,
			cursorX:   15,
			cursorY:   10,
			expectedX: 11, // +1 in X
			expectedY: 10, // No Y change
		},
		{
			name:      "Already at cursor",
			drainX:    10,
			drainY:    10,
			cursorX:   10,
			cursorY:   10,
			expectedX: 10, // No change
			expectedY: 10, // No change
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
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

			// Manually create drain at specific position
			entity := world.CreateEntity()
			world.Positions.Add(entity, components.PositionComponent{X: tc.drainX, Y: tc.drainY})
			world.Drains.Add(entity, components.DrainComponent{
				X:            tc.drainX,
				Y:            tc.drainY,
				LastMoveTime: startTime,
			})

			tx := world.BeginSpatialTransaction()
			tx.Spawn(entity, tc.drainX, tc.drainY)
			tx.Commit()

			ctx.State.SetDrainActive(true)
			ctx.State.SetDrainEntity(uint64(entity))
			ctx.State.SetDrainX(tc.drainX)
			ctx.State.SetDrainY(tc.drainY)
			ctx.State.SetEnergy(100)

			// Set cursor position
			ctx.State.SetCursorX(tc.cursorX)
			ctx.State.SetCursorY(tc.cursorY)

			// Advance time to trigger movement
			mockTime.Advance(constants.DrainMoveInterval)

			drainSys := NewDrainSystem(ctx)
			drainSys.Update(world, 16*time.Millisecond)

			// Verify new position
			actualX := ctx.State.GetDrainX()
			actualY := ctx.State.GetDrainY()

			if actualX != tc.expectedX || actualY != tc.expectedY {
				t.Errorf("Expected drain at (%d, %d), got (%d, %d)",
					tc.expectedX, tc.expectedY, actualX, actualY)
			}
		})
	}
}

// TestDrainSystem_MovementMultipleSteps tests that drain moves multiple times correctly
func TestDrainSystem_MovementMultipleSteps(t *testing.T) {
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

	drainSys := NewDrainSystem(ctx)

	// Set cursor to (0, 0) so drain spawns there
	ctx.State.SetCursorX(0)
	ctx.State.SetCursorY(0)

	// Spawn drain at cursor position (0, 0)
	ctx.State.SetEnergy(100)
	drainSys.Update(world, 16*time.Millisecond)

	// Verify drain spawned at (0, 0)
	if ctx.State.GetDrainX() != 0 || ctx.State.GetDrainY() != 0 {
		t.Fatalf("Expected drain to spawn at (0, 0), got (%d, %d)",
			ctx.State.GetDrainX(), ctx.State.GetDrainY())
	}

	// Set cursor at (5, 5) - requires 5 steps in each direction
	ctx.State.SetCursorX(5)
	ctx.State.SetCursorY(5)

	// Move 5 times (should reach cursor)
	for i := 0; i < 5; i++ {
		mockTime.Advance(constants.DrainMoveInterval)
		drainSys.Update(world, 16*time.Millisecond)

		expectedX := i + 1
		expectedY := i + 1
		actualX := ctx.State.GetDrainX()
		actualY := ctx.State.GetDrainY()

		if actualX != expectedX || actualY != expectedY {
			t.Errorf("After step %d: expected (%d, %d), got (%d, %d)",
				i+1, expectedX, expectedY, actualX, actualY)
		}
	}

	// Verify drain reached cursor
	if ctx.State.GetDrainX() != 5 || ctx.State.GetDrainY() != 5 {
		t.Errorf("Expected drain to reach cursor at (5, 5), got (%d, %d)",
			ctx.State.GetDrainX(), ctx.State.GetDrainY())
	}

	// Move one more time - should stay at cursor
	mockTime.Advance(constants.DrainMoveInterval)
	drainSys.Update(world, 16*time.Millisecond)

	if ctx.State.GetDrainX() != 5 || ctx.State.GetDrainY() != 5 {
		t.Errorf("Expected drain to stay at cursor (5, 5), got (%d, %d)",
			ctx.State.GetDrainX(), ctx.State.GetDrainY())
	}
}