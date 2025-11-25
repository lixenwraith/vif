package systems

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestDrainSystem_SpawnWhenEnergyPositive tests that drain spawns when energy > 0
func TestDrainSystem_SpawnWhenEnergyPositive(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	drainSys := NewDrainSystem(ctx)

	// Initially no drain should be active
	if ctx.State.GetDrainActive() {
		t.Fatal("Expected drain to not be active initially")
	}

	// Set energy > 0
	ctx.State.SetEnergy(100)

	// Run system update
	drainSys.Update(world, 16*time.Millisecond)

	// Verify drain is now active
	if !ctx.State.GetDrainActive() {
		t.Fatal("Expected drain to be active after energy > 0")
	}

	// Verify drain entity exists
	entityID := ctx.State.GetDrainEntity()
	if entityID == 0 {
		t.Fatal("Expected drain entity ID to be non-zero")
	}

	// Verify entity has DrainComponent
	entity := engine.Entity(entityID)
	// Using direct store access
	if _, ok := world.Drains.Get(entity); !ok {
		t.Fatal("Expected drain entity to have DrainComponent")
	}

	// Verify entity has PositionComponent
	if _, ok := world.Positions.Get(entity); !ok {
		t.Fatal("Expected drain entity to have PositionComponent")
	}

	// Verify GameState position atomics are set
	drainX := ctx.State.GetDrainX()
	drainY := ctx.State.GetDrainY()
	if drainX < 0 || drainX >= ctx.GameWidth || drainY < 0 || drainY >= ctx.GameHeight {
		t.Errorf("Expected drain position to be within bounds, got (%d, %d)", drainX, drainY)
	}
}

// TestDrainSystem_DespawnWhenEnergyZero tests that drain despawns when energy <= 0
func TestDrainSystem_DespawnWhenEnergyZero(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	drainSys := NewDrainSystem(ctx)

	// Set energy > 0 to spawn drain
	ctx.State.SetEnergy(100)
	drainSys.Update(world, 16*time.Millisecond)

	// Verify drain is active
	if !ctx.State.GetDrainActive() {
		t.Fatal("Expected drain to be active after spawn")
	}

	entityID := ctx.State.GetDrainEntity()
	entity := engine.Entity(entityID)

	// Set energy to 0
	ctx.State.SetEnergy(0)

	// Run system update
	drainSys.Update(world, 16*time.Millisecond)

	// Verify drain is no longer active
	if ctx.State.GetDrainActive() {
		t.Fatal("Expected drain to not be active after energy <= 0")
	}

	// Verify drain entity ID is cleared
	if ctx.State.GetDrainEntity() != 0 {
		t.Fatal("Expected drain entity ID to be 0 after despawn")
	}

	// Verify entity no longer has DrainComponent (or doesn't exist)
	// Using direct store access
	if _, ok := world.Drains.Get(entity); ok {
		t.Fatal("Expected drain entity to not have DrainComponent after despawn")
	}
}

// TestDrainSystem_DespawnWhenEnergyNegative tests that drain despawns when energy < 0
func TestDrainSystem_DespawnWhenEnergyNegative(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	drainSys := NewDrainSystem(ctx)

	// Set energy > 0 to spawn drain
	ctx.State.SetEnergy(50)
	drainSys.Update(world, 16*time.Millisecond)

	// Verify drain is active
	if !ctx.State.GetDrainActive() {
		t.Fatal("Expected drain to be active after spawn")
	}

	// Set energy to negative value
	ctx.State.SetEnergy(-10)

	// Run system update
	drainSys.Update(world, 16*time.Millisecond)

	// Verify drain is no longer active
	if ctx.State.GetDrainActive() {
		t.Fatal("Expected drain to not be active after energy < 0")
	}

	// Verify drain entity ID is cleared
	if ctx.State.GetDrainEntity() != 0 {
		t.Fatal("Expected drain entity ID to be 0 after despawn")
	}
}

// TestDrainSystem_NoSpawnWhenEnergyZero tests that drain doesn't spawn when energy = 0
func TestDrainSystem_NoSpawnWhenEnergyZero(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	drainSys := NewDrainSystem(ctx)

	// Set energy to 0
	ctx.State.SetEnergy(0)

	// Run system update
	drainSys.Update(world, 16*time.Millisecond)

	// Verify drain is not active
	if ctx.State.GetDrainActive() {
		t.Fatal("Expected drain to not be active when energy = 0")
	}

	// Verify no drain entity
	if ctx.State.GetDrainEntity() != 0 {
		t.Fatal("Expected no drain entity when energy = 0")
	}
}

// TestDrainSystem_NoDespawnWhenEnergyStaysPositive tests that drain persists when energy > 0
func TestDrainSystem_NoDespawnWhenEnergyStaysPositive(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	drainSys := NewDrainSystem(ctx)

	// Set energy > 0 to spawn drain
	ctx.State.SetEnergy(100)
	drainSys.Update(world, 16*time.Millisecond)

	// Verify drain is active
	if !ctx.State.GetDrainActive() {
		t.Fatal("Expected drain to be active after spawn")
	}

	originalEntityID := ctx.State.GetDrainEntity()

	// Update energy but keep it positive
	ctx.State.SetEnergy(50)

	// Run system update multiple times
	for i := 0; i < 5; i++ {
		drainSys.Update(world, 16*time.Millisecond)
	}

	// Verify drain is still active
	if !ctx.State.GetDrainActive() {
		t.Fatal("Expected drain to remain active when energy > 0")
	}

	// Verify same entity ID (no respawn)
	if ctx.State.GetDrainEntity() != originalEntityID {
		t.Fatal("Expected drain entity to remain the same when energy > 0")
	}
}

// TestDrainSystem_SpawnDespawnCycle tests spawn-despawn-spawn cycle
func TestDrainSystem_SpawnDespawnCycle(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	drainSys := NewDrainSystem(ctx)

	// First spawn: energy > 0
	ctx.State.SetEnergy(100)
	drainSys.Update(world, 16*time.Millisecond)

	if !ctx.State.GetDrainActive() {
		t.Fatal("Expected drain to be active after first spawn")
	}

	firstEntityID := ctx.State.GetDrainEntity()

	// Despawn: energy <= 0
	ctx.State.SetEnergy(0)
	drainSys.Update(world, 16*time.Millisecond)

	if ctx.State.GetDrainActive() {
		t.Fatal("Expected drain to not be active after despawn")
	}

	// Second spawn: energy > 0 again
	ctx.State.SetEnergy(50)
	drainSys.Update(world, 16*time.Millisecond)

	if !ctx.State.GetDrainActive() {
		t.Fatal("Expected drain to be active after second spawn")
	}

	secondEntityID := ctx.State.GetDrainEntity()

	// Entity IDs should be different (new entity created)
	if firstEntityID == secondEntityID {
		t.Fatal("Expected different entity IDs for spawn-despawn-spawn cycle")
	}

	// Verify second entity has DrainComponent
	entity := engine.Entity(secondEntityID)
	// Using direct store access
	if _, ok := world.Drains.Get(entity); !ok {
		t.Fatal("Expected second drain entity to have DrainComponent")
	}
}

// TestDrainSystem_NoDoubleSpawn tests that system doesn't create duplicate drains
func TestDrainSystem_NoDoubleSpawn(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	drainSys := NewDrainSystem(ctx)

	// Set energy > 0
	ctx.State.SetEnergy(100)

	// Run system update multiple times
	for i := 0; i < 10; i++ {
		drainSys.Update(world, 16*time.Millisecond)
	}

	// Count entities with DrainComponent
	// Using direct store access
	drainEntities := world.Drains.All()

	if len(drainEntities) != 1 {
		t.Fatalf("Expected exactly 1 drain entity, got %d", len(drainEntities))
	}

	// Verify only one entity ID is tracked
	if !ctx.State.GetDrainActive() {
		t.Fatal("Expected drain to be active")
	}
}