package systems

import (
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestDrainSystem_EnergyDrainWhenOnCursor tests energy drain when drain is on cursor
func TestDrainSystem_EnergyDrainWhenOnCursor(t *testing.T) {
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

	// Set initial energy and cursor
	initialEnergy := 100
	cursorX, cursorY := 10, 10
	ctx.State.SetEnergy(initialEnergy)
	ctx.State.SetCursorX(cursorX)
	ctx.State.SetCursorY(cursorY)

	// Manually create drain at cursor position
	entity := world.CreateEntity()
	world.Positions.Add(entity, components.PositionComponent{X: cursorX, Y: cursorY})
	world.Drains.Add(entity, components.DrainComponent{
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

	// Energy should not change yet (DrainEnergyDrainInterval hasn't passed)
	if ctx.State.GetEnergy() != initialEnergy {
		t.Errorf("Energy should not change before DrainEnergyDrainInterval, got %d, expected %d",
			ctx.State.GetEnergy(), initialEnergy)
	}

	// Advance time by DrainEnergyDrainInterval
	mockTime.Advance(constants.DrainEnergyDrainInterval)
	drainSys.Update(world, 16*time.Millisecond)

	// Energy should be reduced by DrainEnergyDrainAmount (10)
	expectedEnergy := initialEnergy - constants.DrainEnergyDrainAmount
	actualEnergy := ctx.State.GetEnergy()
	if actualEnergy != expectedEnergy {
		t.Errorf("Expected energy %d after drain, got %d", expectedEnergy, actualEnergy)
	}
}

// TestDrainSystem_NoDrainWhenNotOnCursor tests that energy is not drained when drain is not on cursor
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

	// Set initial energy
	initialEnergy := 100
	ctx.State.SetEnergy(initialEnergy)

	// Place cursor at (10, 10)
	ctx.State.SetCursorX(10)
	ctx.State.SetCursorY(10)

	// Create drain at different position (5, 5)
	drainX, drainY := 5, 5
	entity := world.CreateEntity()
	world.Positions.Add(entity, components.PositionComponent{X: drainX, Y: drainY})
	world.Drains.Add(entity, components.DrainComponent{
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

	// Advance time by DrainEnergyDrainInterval
	mockTime.Advance(constants.DrainEnergyDrainInterval)
	drainSys.Update(world, 16*time.Millisecond)

	// Energy should NOT change (drain not on cursor)
	if ctx.State.GetEnergy() != initialEnergy {
		t.Errorf("Energy should not change when drain is not on cursor, got %d, expected %d",
			ctx.State.GetEnergy(), initialEnergy)
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

	// Set energy and cursor
	ctx.State.SetEnergy(100)
	cursorX, cursorY := 10, 10
	ctx.State.SetCursorX(cursorX)
	ctx.State.SetCursorY(cursorY)

	// Create drain at cursor position
	entity := world.CreateEntity()
	world.Positions.Add(entity, components.PositionComponent{X: cursorX, Y: cursorY})
	world.Drains.Add(entity, components.DrainComponent{
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
	// Using direct store access
	drainComp, ok := world.Drains.Get(entity)
	if !ok {
		t.Fatal("Expected entity to have DrainComponent")
	}
	drain := drainComp

	if !drain.IsOnCursor {
		t.Error("Expected IsOnCursor to be true when drain is on cursor")
	}

	// Move cursor away
	ctx.State.SetCursorX(15)
	ctx.State.SetCursorY(15)

	// Update - should set IsOnCursor to false
	drainSys.Update(world, 16*time.Millisecond)

	// Check IsOnCursor is now false
	drainComp, ok = world.Drains.Get(entity)
	if !ok {
		t.Fatal("Expected entity to have DrainComponent")
	}
	drain = drainComp

	if drain.IsOnCursor {
		t.Error("Expected IsOnCursor to be false when drain is not on cursor")
	}
}

// TestDrainSystem_MultipleDrainTicks tests multiple energy drain ticks
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

	// Set initial energy
	initialEnergy := 100
	ctx.State.SetEnergy(initialEnergy)

	// Place cursor and drain at same position
	cursorX, cursorY := 10, 10
	ctx.State.SetCursorX(cursorX)
	ctx.State.SetCursorY(cursorY)

	entity := world.CreateEntity()
	world.Positions.Add(entity, components.PositionComponent{X: cursorX, Y: cursorY})
	world.Drains.Add(entity, components.DrainComponent{
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
		mockTime.Advance(constants.DrainEnergyDrainInterval)
		drainSys.Update(world, 16*time.Millisecond)

		// Verify energy decreased
		expectedEnergy := initialEnergy - (i+1)*constants.DrainEnergyDrainAmount
		actualEnergy := ctx.State.GetEnergy()
		if actualEnergy != expectedEnergy {
			t.Errorf("After tick %d: expected energy %d, got %d",
				i+1, expectedEnergy, actualEnergy)
		}
	}

	// Final energy should be initialEnergy - (numTicks * DrainAmount)
	expectedFinalEnergy := initialEnergy - (numTicks * constants.DrainEnergyDrainAmount)
	if ctx.State.GetEnergy() != expectedFinalEnergy {
		t.Errorf("Final energy should be %d, got %d",
			expectedFinalEnergy, ctx.State.GetEnergy())
	}
}

// TestDrainSystem_NoDrainBeforeInterval tests that drain doesn't occur before DrainEnergyDrainInterval
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

	// Set initial energy
	initialEnergy := 100
	ctx.State.SetEnergy(initialEnergy)

	// Place cursor and drain at same position
	cursorX, cursorY := 10, 10
	ctx.State.SetCursorX(cursorX)
	ctx.State.SetCursorY(cursorY)

	entity := world.CreateEntity()
	world.Positions.Add(entity, components.PositionComponent{X: cursorX, Y: cursorY})
	world.Drains.Add(entity, components.DrainComponent{
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

	// Test various time intervals less than DrainEnergyDrainInterval
	intervals := []time.Duration{
		constants.DrainEnergyDrainInterval / 20,
		constants.DrainEnergyDrainInterval / 10,
		constants.DrainEnergyDrainInterval / 5,
		constants.DrainEnergyDrainInterval - 1*time.Millisecond,
	}

	for _, interval := range intervals {
		// Reset time and energy
		mockTime.SetTime(startTime)
		ctx.State.SetEnergy(initialEnergy)

		// Recreate drain component with fresh times
		world.Drains.Add(entity, components.DrainComponent{
			X:             cursorX,
			Y:             cursorY,
			LastMoveTime:  startTime,
			LastDrainTime: startTime,
			IsOnCursor:    false,
		})

		// Advance by interval
		mockTime.Advance(interval)
		drainSys.Update(world, 16*time.Millisecond)

		// Energy should NOT have changed
		if ctx.State.GetEnergy() != initialEnergy {
			t.Errorf("Energy should not drain before DrainEnergyDrainInterval (tested at %v), got %d, expected %d",
				interval, ctx.State.GetEnergy(), initialEnergy)
		}
	}
}

// TestDrainSystem_EnergyDrainDespawnAtZero tests that drain despawns when energy reaches zero
func TestDrainSystem_EnergyDrainDespawnAtZero(t *testing.T) {
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

	// Set initial energy to exactly DrainEnergyDrainAmount (10)
	initialEnergy := constants.DrainEnergyDrainAmount
	ctx.State.SetEnergy(initialEnergy)

	// Place cursor and drain at same position
	cursorX, cursorY := 10, 10
	ctx.State.SetCursorX(cursorX)
	ctx.State.SetCursorY(cursorY)

	entity := world.CreateEntity()
	world.Positions.Add(entity, components.PositionComponent{X: cursorX, Y: cursorY})
	world.Drains.Add(entity, components.DrainComponent{
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
	mockTime.Advance(constants.DrainEnergyDrainInterval)
	drainSys.Update(world, 16*time.Millisecond)

	// Energy should now be 0
	if ctx.State.GetEnergy() != 0 {
		t.Errorf("Expected energy to be 0, got %d", ctx.State.GetEnergy())
	}

	// Drain should still be active (despawn check happens on next update)
	if !ctx.State.GetDrainActive() {
		t.Error("Drain should still be active after first update")
	}

	// Next update should despawn drain
	drainSys.Update(world, 16*time.Millisecond)

	// Drain should now be inactive
	if ctx.State.GetDrainActive() {
		t.Error("Drain should be despawned when energy <= 0")
	}

	// Entity should be destroyed
	// Using direct store access
	_, ok := world.Drains.Get(entity)
	if ok {
		t.Error("Drain entity should be destroyed when energy <= 0")
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

	// Set energy
	ctx.State.SetEnergy(100)

	// Place cursor and drain at same position
	cursorX, cursorY := 10, 10
	ctx.State.SetCursorX(cursorX)
	ctx.State.SetCursorY(cursorY)

	entity := world.CreateEntity()
	world.Positions.Add(entity, components.PositionComponent{X: cursorX, Y: cursorY})
	world.Drains.Add(entity, components.DrainComponent{
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
	mockTime.Advance(constants.DrainEnergyDrainInterval)
	drainSys.Update(world, 16*time.Millisecond)

	// Get drain component and verify LastDrainTime was updated
	// Using direct store access
	drainComp, ok := world.Drains.Get(entity)
	if !ok {
		t.Fatal("Expected entity to have DrainComponent")
	}
	drain := drainComp

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