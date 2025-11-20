package systems

import (
	"reflect"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestDrainSystem_ScoreDrainWhenOnCursor tests score drain when drain is on cursor
func TestDrainSystem_ScoreDrainWhenOnCursor(t *testing.T) {
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

	// Set initial score and cursor
	initialScore := 100
	cursorX, cursorY := 10, 10
	ctx.State.SetScore(initialScore)
	ctx.State.SetCursorX(cursorX)
	ctx.State.SetCursorY(cursorY)

	// Manually create drain at cursor position
	entity := world.CreateEntity()
	world.AddComponent(entity, components.PositionComponent{X: cursorX, Y: cursorY})
	world.AddComponent(entity, components.DrainComponent{
		X:             cursorX,
		Y:             cursorY,
		LastMoveTime:  startTime,
		LastDrainTime: startTime,
		IsOnCursor:    false,
	})

	tx := world.BeginSpatialTransaction()
	tx.Spawn(entity, cursorX, cursorY)
	tx.Commit()

	ctx.State.SetDrainActive(true)
	ctx.State.SetDrainEntity(uint64(entity))
	ctx.State.SetDrainX(cursorX)
	ctx.State.SetDrainY(cursorY)

	// Update without advancing time - should not drain yet
	drainSys.Update(world, 16*time.Millisecond)

	// Score should not change yet (DrainScoreDrainInterval hasn't passed)
	if ctx.State.GetScore() != initialScore {
		t.Errorf("Score should not change before DrainScoreDrainInterval, got %d, expected %d",
			ctx.State.GetScore(), initialScore)
	}

	// Advance time by DrainScoreDrainInterval
	mockTime.Advance(constants.DrainScoreDrainInterval)
	drainSys.Update(world, 16*time.Millisecond)

	// Score should be reduced by DrainScoreDrainAmount (10)
	expectedScore := initialScore - constants.DrainScoreDrainAmount
	actualScore := ctx.State.GetScore()
	if actualScore != expectedScore {
		t.Errorf("Expected score %d after drain, got %d", expectedScore, actualScore)
	}
}

// TestDrainSystem_NoDrainWhenNotOnCursor tests that score is not drained when drain is not on cursor
func TestDrainSystem_NoDrainWhenNotOnCursor(t *testing.T) {
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

	// Set initial score
	initialScore := 100
	ctx.State.SetScore(initialScore)

	// Place cursor at (10, 10)
	ctx.State.SetCursorX(10)
	ctx.State.SetCursorY(10)

	// Create drain at different position (5, 5)
	drainX, drainY := 5, 5
	entity := world.CreateEntity()
	world.AddComponent(entity, components.PositionComponent{X: drainX, Y: drainY})
	world.AddComponent(entity, components.DrainComponent{
		X:             drainX,
		Y:             drainY,
		LastMoveTime:  startTime,
		LastDrainTime: startTime,
		IsOnCursor:    false,
	})

	tx := world.BeginSpatialTransaction()
	tx.Spawn(entity, drainX, drainY)
	tx.Commit()

	ctx.State.SetDrainActive(true)
	ctx.State.SetDrainEntity(uint64(entity))
	ctx.State.SetDrainX(drainX)
	ctx.State.SetDrainY(drainY)

	// Advance time by DrainScoreDrainInterval
	mockTime.Advance(constants.DrainScoreDrainInterval)
	drainSys.Update(world, 16*time.Millisecond)

	// Score should NOT change (drain not on cursor)
	if ctx.State.GetScore() != initialScore {
		t.Errorf("Score should not change when drain is not on cursor, got %d, expected %d",
			ctx.State.GetScore(), initialScore)
	}
}

// TestDrainSystem_IsOnCursorStateTracking tests that IsOnCursor state is updated correctly
func TestDrainSystem_IsOnCursorStateTracking(t *testing.T) {
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

	// Set score and cursor
	ctx.State.SetScore(100)
	cursorX, cursorY := 10, 10
	ctx.State.SetCursorX(cursorX)
	ctx.State.SetCursorY(cursorY)

	// Create drain at cursor position
	entity := world.CreateEntity()
	world.AddComponent(entity, components.PositionComponent{X: cursorX, Y: cursorY})
	world.AddComponent(entity, components.DrainComponent{
		X:             cursorX,
		Y:             cursorY,
		LastMoveTime:  startTime,
		LastDrainTime: startTime,
		IsOnCursor:    false, // Initially false
	})

	tx := world.BeginSpatialTransaction()
	tx.Spawn(entity, cursorX, cursorY)
	tx.Commit()

	ctx.State.SetDrainActive(true)
	ctx.State.SetDrainEntity(uint64(entity))
	ctx.State.SetDrainX(cursorX)
	ctx.State.SetDrainY(cursorY)

	// Update - should set IsOnCursor to true
	drainSys.Update(world, 16*time.Millisecond)

	// Check IsOnCursor is now true
	drainType := reflect.TypeOf(components.DrainComponent{})
	drainComp, ok := world.GetComponent(entity, drainType)
	if !ok {
		t.Fatal("Expected entity to have DrainComponent")
	}
	drain := drainComp.(components.DrainComponent)

	if !drain.IsOnCursor {
		t.Error("Expected IsOnCursor to be true when drain is on cursor")
	}

	// Move cursor away
	ctx.State.SetCursorX(15)
	ctx.State.SetCursorY(15)

	// Update - should set IsOnCursor to false
	drainSys.Update(world, 16*time.Millisecond)

	// Check IsOnCursor is now false
	drainComp, ok = world.GetComponent(entity, drainType)
	if !ok {
		t.Fatal("Expected entity to have DrainComponent")
	}
	drain = drainComp.(components.DrainComponent)

	if drain.IsOnCursor {
		t.Error("Expected IsOnCursor to be false when drain is not on cursor")
	}
}

// TestDrainSystem_MultipleDrainTicks tests multiple score drain ticks
func TestDrainSystem_MultipleDrainTicks(t *testing.T) {
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

	// Set initial score
	initialScore := 100
	ctx.State.SetScore(initialScore)

	// Place cursor and drain at same position
	cursorX, cursorY := 10, 10
	ctx.State.SetCursorX(cursorX)
	ctx.State.SetCursorY(cursorY)

	entity := world.CreateEntity()
	world.AddComponent(entity, components.PositionComponent{X: cursorX, Y: cursorY})
	world.AddComponent(entity, components.DrainComponent{
		X:             cursorX,
		Y:             cursorY,
		LastMoveTime:  startTime,
		LastDrainTime: startTime,
		IsOnCursor:    false,
	})

	tx := world.BeginSpatialTransaction()
	tx.Spawn(entity, cursorX, cursorY)
	tx.Commit()

	ctx.State.SetDrainActive(true)
	ctx.State.SetDrainEntity(uint64(entity))
	ctx.State.SetDrainX(cursorX)
	ctx.State.SetDrainY(cursorY)

	// Perform 5 drain ticks
	numTicks := 5
	for i := 0; i < numTicks; i++ {
		// Advance time by drain interval
		mockTime.Advance(constants.DrainScoreDrainInterval)
		drainSys.Update(world, 16*time.Millisecond)

		// Verify score decreased
		expectedScore := initialScore - (i+1)*constants.DrainScoreDrainAmount
		actualScore := ctx.State.GetScore()
		if actualScore != expectedScore {
			t.Errorf("After tick %d: expected score %d, got %d",
				i+1, expectedScore, actualScore)
		}
	}

	// Final score should be initialScore - (numTicks * DrainAmount)
	expectedFinalScore := initialScore - (numTicks * constants.DrainScoreDrainAmount)
	if ctx.State.GetScore() != expectedFinalScore {
		t.Errorf("Final score should be %d, got %d",
			expectedFinalScore, ctx.State.GetScore())
	}
}

// TestDrainSystem_NoDrainBeforeInterval tests that drain doesn't occur before DrainScoreDrainInterval
func TestDrainSystem_NoDrainBeforeInterval(t *testing.T) {
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

	// Set initial score
	initialScore := 100
	ctx.State.SetScore(initialScore)

	// Place cursor and drain at same position
	cursorX, cursorY := 10, 10
	ctx.State.SetCursorX(cursorX)
	ctx.State.SetCursorY(cursorY)

	entity := world.CreateEntity()
	world.AddComponent(entity, components.PositionComponent{X: cursorX, Y: cursorY})
	world.AddComponent(entity, components.DrainComponent{
		X:             cursorX,
		Y:             cursorY,
		LastMoveTime:  startTime,
		LastDrainTime: startTime,
		IsOnCursor:    false,
	})

	tx := world.BeginSpatialTransaction()
	tx.Spawn(entity, cursorX, cursorY)
	tx.Commit()

	ctx.State.SetDrainActive(true)
	ctx.State.SetDrainEntity(uint64(entity))
	ctx.State.SetDrainX(cursorX)
	ctx.State.SetDrainY(cursorY)

	// Test various time intervals less than DrainScoreDrainInterval
	intervals := []time.Duration{
		constants.DrainScoreDrainInterval / 20,
		constants.DrainScoreDrainInterval / 10,
		constants.DrainScoreDrainInterval / 5,
		constants.DrainScoreDrainInterval - 1*time.Millisecond,
	}

	for _, interval := range intervals {
		// Reset time and score
		mockTime.SetTime(startTime)
		ctx.State.SetScore(initialScore)

		// Recreate drain component with fresh times
		world.AddComponent(entity, components.DrainComponent{
			X:             cursorX,
			Y:             cursorY,
			LastMoveTime:  startTime,
			LastDrainTime: startTime,
			IsOnCursor:    false,
		})

		// Advance by interval
		mockTime.Advance(interval)
		drainSys.Update(world, 16*time.Millisecond)

		// Score should NOT have changed
		if ctx.State.GetScore() != initialScore {
			t.Errorf("Score should not drain before DrainScoreDrainInterval (tested at %v), got %d, expected %d",
				interval, ctx.State.GetScore(), initialScore)
		}
	}
}

// TestDrainSystem_ScoreDrainDespawnAtZero tests that drain despawns when score reaches zero
func TestDrainSystem_ScoreDrainDespawnAtZero(t *testing.T) {
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

	// Set initial score to exactly DrainScoreDrainAmount (10)
	initialScore := constants.DrainScoreDrainAmount
	ctx.State.SetScore(initialScore)

	// Place cursor and drain at same position
	cursorX, cursorY := 10, 10
	ctx.State.SetCursorX(cursorX)
	ctx.State.SetCursorY(cursorY)

	entity := world.CreateEntity()
	world.AddComponent(entity, components.PositionComponent{X: cursorX, Y: cursorY})
	world.AddComponent(entity, components.DrainComponent{
		X:             cursorX,
		Y:             cursorY,
		LastMoveTime:  startTime,
		LastDrainTime: startTime,
		IsOnCursor:    false,
	})

	tx := world.BeginSpatialTransaction()
	tx.Spawn(entity, cursorX, cursorY)
	tx.Commit()

	ctx.State.SetDrainActive(true)
	ctx.State.SetDrainEntity(uint64(entity))
	ctx.State.SetDrainX(cursorX)
	ctx.State.SetDrainY(cursorY)

	// Advance time to trigger drain
	mockTime.Advance(constants.DrainScoreDrainInterval)
	drainSys.Update(world, 16*time.Millisecond)

	// Score should now be 0
	if ctx.State.GetScore() != 0 {
		t.Errorf("Expected score to be 0, got %d", ctx.State.GetScore())
	}

	// Drain should still be active (despawn check happens on next update)
	if !ctx.State.GetDrainActive() {
		t.Error("Drain should still be active after first update")
	}

	// Next update should despawn drain
	drainSys.Update(world, 16*time.Millisecond)

	// Drain should now be inactive
	if ctx.State.GetDrainActive() {
		t.Error("Drain should be despawned when score <= 0")
	}

	// Entity should be destroyed
	drainType := reflect.TypeOf(components.DrainComponent{})
	_, ok := world.GetComponent(entity, drainType)
	if ok {
		t.Error("Drain entity should be destroyed when score <= 0")
	}
}

// TestDrainSystem_LastDrainTimeUpdated tests that LastDrainTime is updated after drain
func TestDrainSystem_LastDrainTimeUpdated(t *testing.T) {
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

	// Set score
	ctx.State.SetScore(100)

	// Place cursor and drain at same position
	cursorX, cursorY := 10, 10
	ctx.State.SetCursorX(cursorX)
	ctx.State.SetCursorY(cursorY)

	entity := world.CreateEntity()
	world.AddComponent(entity, components.PositionComponent{X: cursorX, Y: cursorY})
	world.AddComponent(entity, components.DrainComponent{
		X:             cursorX,
		Y:             cursorY,
		LastMoveTime:  startTime,
		LastDrainTime: startTime,
		IsOnCursor:    false,
	})

	tx := world.BeginSpatialTransaction()
	tx.Spawn(entity, cursorX, cursorY)
	tx.Commit()

	ctx.State.SetDrainActive(true)
	ctx.State.SetDrainEntity(uint64(entity))
	ctx.State.SetDrainX(cursorX)
	ctx.State.SetDrainY(cursorY)

	// Advance time and trigger drain
	mockTime.Advance(constants.DrainScoreDrainInterval)
	drainSys.Update(world, 16*time.Millisecond)

	// Get drain component and verify LastDrainTime was updated
	drainType := reflect.TypeOf(components.DrainComponent{})
	drainComp, ok := world.GetComponent(entity, drainType)
	if !ok {
		t.Fatal("Expected entity to have DrainComponent")
	}
	drain := drainComp.(components.DrainComponent)

	// LastDrainTime should be updated (not equal to startTime)
	if drain.LastDrainTime.Equal(startTime) {
		t.Error("Expected LastDrainTime to be updated after drain")
	}

	// LastDrainTime should be equal to current time
	currentTime := mockTime.Now()
	if !drain.LastDrainTime.Equal(currentTime) {
		t.Errorf("Expected LastDrainTime to be %v, got %v",
			currentTime, drain.LastDrainTime)
	}
}
