package systems

import (
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestDrainSystem_IntegrationWithEnergySystem tests drain system integration with energy system
// This test verifies that:
// 1. Energy can be earned through typing (EnergySystem)
// 2. Energy can be drained when drain is on cursor (DrainSystem)
// 3. Both systems work together correctly
func TestDrainSystem_IntegrationWithEnergySystem(t *testing.T) {
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

	// Create systems
	drainSys := NewDrainSystem(ctx)

	// Initial energy is 0
	if ctx.State.GetEnergy() != 0 {
		t.Fatalf("Expected initial energy 0, got %d", ctx.State.GetEnergy())
	}

	// No drain should be active at energy 0
	if ctx.State.GetDrainActive() {
		t.Error("Drain should not be active when energy is 0")
	}

	// Add some energy (simulating user earning points)
	earnedEnergy := 50
	ctx.State.SetEnergy(earnedEnergy)

	// Update drain system - should spawn drain
	drainSys.Update(world, 16*time.Millisecond)

	// Drain should now be active
	if !ctx.State.GetDrainActive() {
		t.Fatal("Drain should be active when energy > 0")
	}

	// Verify energy is still 50
	if ctx.State.GetEnergy() != earnedEnergy {
		t.Errorf("Energy should be %d, got %d", earnedEnergy, ctx.State.GetEnergy())
	}

	// Move cursor to drain position to trigger draining
	drainX := ctx.State.GetDrainX()
	drainY := ctx.State.GetDrainY()
	ctx.State.SetCursorX(drainX)
	ctx.State.SetCursorY(drainY)

	// Update drain system (should update IsOnCursor state)
	drainSys.Update(world, 16*time.Millisecond)

	// Advance time by drain interval
	mockTime.Advance(constants.DrainEnergyDrainInterval)
	drainSys.Update(world, 16*time.Millisecond)

	// Energy should have decreased by DrainEnergyDrainAmount
	expectedEnergy := earnedEnergy - constants.DrainEnergyDrainAmount
	actualEnergy := ctx.State.GetEnergy()
	if actualEnergy != expectedEnergy {
		t.Errorf("Expected energy %d after first drain, got %d", expectedEnergy, actualEnergy)
	}

	// Add more energy (user earning while drain is active)
	additionalEnergy := 30
	ctx.State.AddEnergy(additionalEnergy)

	currentEnergy := ctx.State.GetEnergy()
	expectedCurrentEnergy := expectedEnergy + additionalEnergy
	if currentEnergy != expectedCurrentEnergy {
		t.Errorf("Expected energy %d after earning more, got %d",
			expectedCurrentEnergy, currentEnergy)
	}

	// Move cursor away from drain
	ctx.State.SetCursorX(drainX + 10)
	ctx.State.SetCursorY(drainY + 10)

	// Advance time again
	mockTime.Advance(constants.DrainEnergyDrainInterval)
	drainSys.Update(world, 16*time.Millisecond)

	// Energy should NOT have changed (drain not on cursor)
	if ctx.State.GetEnergy() != expectedCurrentEnergy {
		t.Errorf("Energy should not change when drain is not on cursor, got %d, expected %d",
			ctx.State.GetEnergy(), expectedCurrentEnergy)
	}

	// Move cursor back to drain
	ctx.State.SetCursorX(drainX)
	ctx.State.SetCursorY(drainY)

	// Let drain catch up to cursor (it might have moved while cursor was away)
	// Run movement updates until drain reaches cursor
	maxMoves := 20
	for i := 0; i < maxMoves; i++ {
		mockTime.Advance(constants.DrainMoveInterval)
		drainSys.Update(world, 16*time.Millisecond)

		currentDrainX := ctx.State.GetDrainX()
		currentDrainY := ctx.State.GetDrainY()

		if currentDrainX == drainX && currentDrainY == drainY {
			break
		}
	}

	// Now drain should be on cursor again, advance time for drain
	beforeDrainEnergy := ctx.State.GetEnergy()
	mockTime.Advance(constants.DrainEnergyDrainInterval)
	drainSys.Update(world, 16*time.Millisecond)

	// Energy should have decreased again
	afterDrainEnergy := ctx.State.GetEnergy()
	expectedDecrease := constants.DrainEnergyDrainAmount
	actualDecrease := beforeDrainEnergy - afterDrainEnergy

	if actualDecrease != expectedDecrease {
		t.Errorf("Expected energy to decrease by %d, but decreased by %d (before: %d, after: %d)",
			expectedDecrease, actualDecrease, beforeDrainEnergy, afterDrainEnergy)
	}
}

// TestDrainSystem_EnergyDrainSpawnDespawnCycle tests the full lifecycle with energy changes
func TestDrainSystem_EnergyDrainSpawnDespawnCycle(t *testing.T) {
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

	// Start with energy 0 - no drain
	drainSys.Update(world, 16*time.Millisecond)
	if ctx.State.GetDrainActive() {
		t.Error("Drain should not spawn at energy 0")
	}

	// Earn some energy - drain should spawn
	ctx.State.SetEnergy(50)
	drainSys.Update(world, 16*time.Millisecond)
	if !ctx.State.GetDrainActive() {
		t.Error("Drain should spawn when energy > 0")
	}

	// Position cursor on drain
	drainX := ctx.State.GetDrainX()
	drainY := ctx.State.GetDrainY()
	ctx.State.SetCursorX(drainX)
	ctx.State.SetCursorY(drainY)

	// Drain energy to 0
	maxDrainTicks := 100 // Safety limit
	for i := 0; i < maxDrainTicks; i++ {
		currentEnergy := ctx.State.GetEnergy()
		if currentEnergy <= 0 {
			break
		}

		mockTime.Advance(constants.DrainEnergyDrainInterval)
		drainSys.Update(world, 16*time.Millisecond)
	}

	// Energy should be at or below 0
	if ctx.State.GetEnergy() > 0 {
		t.Errorf("Expected energy to reach 0, got %d", ctx.State.GetEnergy())
	}

	// Drain should despawn on next update
	drainSys.Update(world, 16*time.Millisecond)
	if ctx.State.GetDrainActive() {
		t.Error("Drain should despawn when energy <= 0")
	}

	// Add energy again - drain should respawn
	ctx.State.SetEnergy(100)
	drainSys.Update(world, 16*time.Millisecond)
	if !ctx.State.GetDrainActive() {
		t.Error("Drain should respawn when energy > 0 again")
	}
}

// TestDrainSystem_ContinuousDrainUntilZero tests draining until energy reaches zero
func TestDrainSystem_ContinuousDrainUntilZero(t *testing.T) {
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
	initialEnergy := 45
	ctx.State.SetEnergy(initialEnergy)

	// Spawn drain
	drainSys.Update(world, 16*time.Millisecond)
	if !ctx.State.GetDrainActive() {
		t.Fatal("Drain should be active")
	}

	// Position cursor on drain
	drainX := ctx.State.GetDrainX()
	drainY := ctx.State.GetDrainY()
	ctx.State.SetCursorX(drainX)
	ctx.State.SetCursorY(drainY)

	// Calculate expected number of drain ticks to reach 0
	expectedTicks := (initialEnergy + constants.DrainEnergyDrainAmount - 1) / constants.DrainEnergyDrainAmount
	actualTicks := 0

	// Drain continuously
	for actualTicks < expectedTicks+5 { // Add buffer for safety
		if ctx.State.GetEnergy() <= 0 {
			break
		}

		mockTime.Advance(constants.DrainEnergyDrainInterval)
		drainSys.Update(world, 16*time.Millisecond)
		actualTicks++
	}

	// Verify energy reached 0 or negative
	finalEnergy := ctx.State.GetEnergy()
	if finalEnergy > 0 {
		t.Errorf("Expected energy to be <= 0, got %d", finalEnergy)
	}

	// Verify number of ticks matches expectation
	if actualTicks < expectedTicks {
		t.Errorf("Expected at least %d drain ticks, got %d", expectedTicks, actualTicks)
	}

	// Next update should despawn drain
	drainSys.Update(world, 16*time.Millisecond)
	if ctx.State.GetDrainActive() {
		t.Error("Drain should be despawned after energy reaches 0")
	}
}

// TestDrainSystem_AlternatingEnergyChanges tests energy increasing and decreasing
func TestDrainSystem_AlternatingEnergyChanges(t *testing.T) {
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

	// Start with energy
	ctx.State.SetEnergy(50)
	drainSys.Update(world, 16*time.Millisecond)

	if !ctx.State.GetDrainActive() {
		t.Fatal("Drain should be active")
	}

	// Get drain position and move cursor there
	drainX := ctx.State.GetDrainX()
	drainY := ctx.State.GetDrainY()
	ctx.State.SetCursorX(drainX)
	ctx.State.SetCursorY(drainY)

	// Drain some energy
	mockTime.Advance(constants.DrainEnergyDrainInterval)
	drainSys.Update(world, 16*time.Millisecond)

	energy1 := ctx.State.GetEnergy()
	if energy1 != 40 { // 50 - 10
		t.Errorf("Expected energy 40, got %d", energy1)
	}

	// Add energy (simulate user earning points)
	ctx.State.AddEnergy(20)
	energy2 := ctx.State.GetEnergy()
	if energy2 != 60 { // 40 + 20
		t.Errorf("Expected energy 60, got %d", energy2)
	}

	// Drain again
	mockTime.Advance(constants.DrainEnergyDrainInterval)
	drainSys.Update(world, 16*time.Millisecond)

	energy3 := ctx.State.GetEnergy()
	if energy3 != 50 { // 60 - 10
		t.Errorf("Expected energy 50, got %d", energy3)
	}

	// Add more energy
	ctx.State.AddEnergy(30)
	energy4 := ctx.State.GetEnergy()
	if energy4 != 80 { // 50 + 30
		t.Errorf("Expected energy 80, got %d", energy4)
	}

	// Verify drain is still active
	if !ctx.State.GetDrainActive() {
		t.Error("Drain should remain active while energy > 0")
	}
}