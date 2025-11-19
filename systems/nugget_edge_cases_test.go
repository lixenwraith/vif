package systems

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestNuggetSingleInvariant verifies that only one nugget is active at a time
func TestNuggetSingleInvariant(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	screen.Init()
	screen.SetSize(100, 30)
	defer screen.Fini()

	ctx := engine.NewGameContext(screen)
	ctx.GameWidth = 100
	ctx.GameHeight = 30
	world := ctx.World
	timeProvider := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = timeProvider

	nuggetSystem := NewNuggetSystem(ctx)

	// Spawn first nugget
	nuggetSystem.Update(world, 0)
	timeProvider.Advance(6 * time.Second)
	nuggetSystem.Update(world, 0)

	firstNugget := nuggetSystem.GetActiveNugget()
	if firstNugget == 0 {
		t.Fatal("Expected nugget to be spawned")
	}

	// Try to spawn second nugget - should not happen
	timeProvider.Advance(6 * time.Second)
	nuggetSystem.Update(world, 0)

	secondCheck := nuggetSystem.GetActiveNugget()
	if secondCheck != firstNugget {
		t.Errorf("Expected active nugget to remain %d, got %d", firstNugget, secondCheck)
	}

	// Count nugget entities in world by querying all components
	nuggetType := reflect.TypeOf(components.NuggetComponent{})
	nuggetEntities := world.GetEntitiesWith(nuggetType)
	nuggetCount := len(nuggetEntities)

	if nuggetCount != 1 {
		t.Errorf("Expected exactly 1 nugget in world, found %d", nuggetCount)
	}
}

// TestNuggetRapidCollectionAndRespawn tests rapid collection and respawn cycles
func TestNuggetRapidCollectionAndRespawn(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	screen.Init()
	screen.SetSize(100, 30)
	defer screen.Fini()

	ctx := engine.NewGameContext(screen)
	ctx.GameWidth = 100
	ctx.GameHeight = 30
	world := ctx.World
	timeProvider := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = timeProvider

	nuggetSystem := NewNuggetSystem(ctx)

	// Perform 10 rapid collection/respawn cycles
	for i := 0; i < 10; i++ {
		// Spawn nugget
		timeProvider.Advance(6 * time.Second)
		nuggetSystem.Update(world, 0)

		activeNugget := nuggetSystem.GetActiveNugget()
		if activeNugget == 0 {
			t.Fatalf("Cycle %d: Expected nugget to be spawned", i)
		}

		// Collect nugget (simulate destruction)
		world.SafeDestroyEntity(engine.Entity(activeNugget))
		cleared := nuggetSystem.ClearActiveNuggetIfMatches(activeNugget)
		if !cleared {
			t.Errorf("Cycle %d: ClearActiveNuggetIfMatches should have succeeded", i)
		}

		// Verify no active nugget
		if nuggetSystem.GetActiveNugget() != 0 {
			t.Errorf("Cycle %d: Expected no active nugget after clear", i)
		}
	}

	// Verify exactly 0 nuggets remain
	nuggetType := reflect.TypeOf(components.NuggetComponent{})
	nuggetEntities := world.GetEntitiesWith(nuggetType)
	nuggetCount := len(nuggetEntities)

	if nuggetCount != 0 {
		t.Errorf("Expected 0 nuggets remaining, found %d", nuggetCount)
	}
}

// TestNuggetClearWithWrongEntityID tests that ClearActiveNuggetIfMatches fails with wrong ID
func TestNuggetClearWithWrongEntityID(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	screen.Init()
	screen.SetSize(100, 30)
	defer screen.Fini()

	ctx := engine.NewGameContext(screen)
	ctx.GameWidth = 100
	ctx.GameHeight = 30
	world := ctx.World
	timeProvider := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = timeProvider

	nuggetSystem := NewNuggetSystem(ctx)

	// Spawn nugget
	timeProvider.Advance(6 * time.Second)
	nuggetSystem.Update(world, 0)

	activeNugget := nuggetSystem.GetActiveNugget()
	if activeNugget == 0 {
		t.Fatal("Expected nugget to be spawned")
	}

	// Try to clear with wrong entity ID
	wrongID := activeNugget + 999
	cleared := nuggetSystem.ClearActiveNuggetIfMatches(wrongID)
	if cleared {
		t.Error("ClearActiveNuggetIfMatches should have failed with wrong entity ID")
	}

	// Verify nugget is still active
	if nuggetSystem.GetActiveNugget() != activeNugget {
		t.Error("Active nugget should not have been cleared")
	}
}

// TestNuggetDoubleDestruction tests that double destruction is handled gracefully
func TestNuggetDoubleDestruction(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	screen.Init()
	screen.SetSize(100, 30)
	defer screen.Fini()

	ctx := engine.NewGameContext(screen)
	ctx.GameWidth = 100
	ctx.GameHeight = 30
	world := ctx.World
	timeProvider := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = timeProvider

	nuggetSystem := NewNuggetSystem(ctx)

	// Spawn nugget
	timeProvider.Advance(6 * time.Second)
	nuggetSystem.Update(world, 0)

	activeNugget := nuggetSystem.GetActiveNugget()
	if activeNugget == 0 {
		t.Fatal("Expected nugget to be spawned")
	}

	// First destruction
	world.SafeDestroyEntity(engine.Entity(activeNugget))
	cleared1 := nuggetSystem.ClearActiveNuggetIfMatches(activeNugget)
	if !cleared1 {
		t.Error("First ClearActiveNuggetIfMatches should have succeeded")
	}

	// Second destruction attempt (should fail)
	cleared2 := nuggetSystem.ClearActiveNuggetIfMatches(activeNugget)
	if cleared2 {
		t.Error("Second ClearActiveNuggetIfMatches should have failed (already cleared)")
	}

	// Verify no active nugget
	if nuggetSystem.GetActiveNugget() != 0 {
		t.Error("Expected no active nugget after double destruction")
	}
}

// TestNuggetClearAfterNewSpawn tests that clearing old nugget doesn't affect new one
func TestNuggetClearAfterNewSpawn(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	screen.Init()
	screen.SetSize(100, 30)
	defer screen.Fini()

	ctx := engine.NewGameContext(screen)
	ctx.GameWidth = 100
	ctx.GameHeight = 30
	world := ctx.World
	timeProvider := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = timeProvider

	nuggetSystem := NewNuggetSystem(ctx)

	// Spawn first nugget
	timeProvider.Advance(6 * time.Second)
	nuggetSystem.Update(world, 0)

	firstNugget := nuggetSystem.GetActiveNugget()
	if firstNugget == 0 {
		t.Fatal("Expected first nugget to be spawned")
	}

	// Destroy first nugget and clear reference
	world.SafeDestroyEntity(engine.Entity(firstNugget))
	nuggetSystem.ClearActiveNuggetIfMatches(firstNugget)

	// Spawn second nugget
	timeProvider.Advance(6 * time.Second)
	nuggetSystem.Update(world, 0)

	secondNugget := nuggetSystem.GetActiveNugget()
	if secondNugget == 0 {
		t.Fatal("Expected second nugget to be spawned")
	}

	if secondNugget == firstNugget {
		t.Error("Second nugget should have different entity ID than first")
	}

	// Try to clear first nugget again (should fail, second nugget should remain)
	cleared := nuggetSystem.ClearActiveNuggetIfMatches(firstNugget)
	if cleared {
		t.Error("Clearing old entity ID should have failed")
	}

	// Verify second nugget is still active
	if nuggetSystem.GetActiveNugget() != secondNugget {
		t.Error("Second nugget should still be active")
	}
}

// TestNuggetVerificationClearsStaleReference tests that Update() clears stale entity references
func TestNuggetVerificationClearsStaleReference(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	screen.Init()
	screen.SetSize(100, 30)
	defer screen.Fini()

	ctx := engine.NewGameContext(screen)
	ctx.GameWidth = 100
	ctx.GameHeight = 30
	world := ctx.World
	timeProvider := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = timeProvider

	nuggetSystem := NewNuggetSystem(ctx)

	// Spawn nugget
	timeProvider.Advance(6 * time.Second)
	nuggetSystem.Update(world, 0)

	activeNugget := nuggetSystem.GetActiveNugget()
	if activeNugget == 0 {
		t.Fatal("Expected nugget to be spawned")
	}

	// Destroy nugget WITHOUT calling ClearActiveNuggetIfMatches (simulating external destruction)
	world.SafeDestroyEntity(engine.Entity(activeNugget))

	// Next Update() should detect missing component and clear reference
	nuggetSystem.Update(world, 0)

	// Verify nugget reference was cleared
	if nuggetSystem.GetActiveNugget() != 0 {
		t.Error("Expected nugget reference to be cleared after entity destruction")
	}
}

// TestNuggetSpawnPositionExclusionZone verifies cursor exclusion zone is enforced
func TestNuggetSpawnPositionExclusionZone(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	screen.Init()
	screen.SetSize(100, 30)
	defer screen.Fini()

	ctx := engine.NewGameContext(screen)
	ctx.GameWidth = 100
	ctx.GameHeight = 30
	world := ctx.World
	timeProvider := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = timeProvider

	// Set cursor position
	ctx.CursorX = 50
	ctx.CursorY = 15
	ctx.State.SetCursorX(50)
	ctx.State.SetCursorY(15)

	nuggetSystem := NewNuggetSystem(ctx)

	// Spawn multiple nuggets and verify none are too close to cursor
	for i := 0; i < 50; i++ {
		timeProvider.Advance(6 * time.Second)
		nuggetSystem.Update(world, 0)

		activeNugget := nuggetSystem.GetActiveNugget()
		if activeNugget == 0 {
			continue // No valid position found, skip
		}

		// Get nugget position
		posType := reflect.TypeOf(components.PositionComponent{})
		posComp, ok := world.GetComponent(engine.Entity(activeNugget), posType)
		if !ok {
			t.Fatal("Expected nugget to have position component")
		}
		pos := posComp.(components.PositionComponent)

		// Verify not within exclusion zone
		if abs(float64(pos.X-50)) <= 5 && abs(float64(pos.Y-15)) <= 3 {
			t.Errorf("Nugget spawned at (%d, %d) within cursor exclusion zone (cursor at 50, 15)", pos.X, pos.Y)
		}

		// Clear for next iteration
		world.SafeDestroyEntity(engine.Entity(activeNugget))
		nuggetSystem.ClearActiveNuggetIfMatches(activeNugget)
	}
}

// TestNuggetGetSystemState tests the debug state string
func TestNuggetGetSystemState(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	screen.Init()
	screen.SetSize(100, 30)
	defer screen.Fini()

	ctx := engine.NewGameContext(screen)
	ctx.GameWidth = 100
	ctx.GameHeight = 30
	world := ctx.World
	timeProvider := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = timeProvider

	nuggetSystem := NewNuggetSystem(ctx)

	// State 1: No nugget, recent update
	state1 := nuggetSystem.GetSystemState()
	if state1 == "" {
		t.Error("GetSystemState should return non-empty string")
	}
	if len(state1) < 10 {
		t.Errorf("GetSystemState returned suspiciously short string: %s", state1)
	}

	// State 2: Spawn nugget
	timeProvider.Advance(6 * time.Second)
	nuggetSystem.Update(world, 0)

	state2 := nuggetSystem.GetSystemState()
	if state2 == state1 {
		t.Error("GetSystemState should change after nugget spawned")
	}

	// State 3: Destroy nugget
	activeNugget := nuggetSystem.GetActiveNugget()
	world.SafeDestroyEntity(engine.Entity(activeNugget))
	nuggetSystem.ClearActiveNuggetIfMatches(activeNugget)

	state3 := nuggetSystem.GetSystemState()
	if state3 == state2 {
		t.Error("GetSystemState should change after nugget destroyed")
	}
}

// TestNuggetConcurrentClearAttempts simulates concurrent destruction attempts (though systems run serially)
func TestNuggetConcurrentClearAttempts(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	screen.Init()
	screen.SetSize(100, 30)
	defer screen.Fini()

	ctx := engine.NewGameContext(screen)
	ctx.GameWidth = 100
	ctx.GameHeight = 30
	world := ctx.World
	timeProvider := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = timeProvider

	nuggetSystem := NewNuggetSystem(ctx)

	// Spawn nugget
	timeProvider.Advance(6 * time.Second)
	nuggetSystem.Update(world, 0)

	activeNugget := nuggetSystem.GetActiveNugget()
	if activeNugget == 0 {
		t.Fatal("Expected nugget to be spawned")
	}

	// Simulate concurrent clear attempts using goroutines
	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if nuggetSystem.ClearActiveNuggetIfMatches(activeNugget) {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	// Exactly one clear should have succeeded (CAS guarantees atomicity)
	if successCount != 1 {
		t.Errorf("Expected exactly 1 successful clear, got %d", successCount)
	}

	// Verify nugget is cleared
	if nuggetSystem.GetActiveNugget() != 0 {
		t.Error("Expected nugget to be cleared")
	}
}
