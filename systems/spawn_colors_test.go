package systems

import (
	"sync"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestColorCountersConcurrency tests basic atomic increment correctness under concurrent access.
// For comprehensive cross-system race testing, see TestConcurrentColorCounterUpdates in race_condition_comprehensive_test.go.
func TestColorCountersConcurrency(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(ctx)

	const numGoroutines = 100
	const incrementsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Launch multiple goroutines that increment the same counter
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGoroutine; j++ {
				spawnSys.AddColorCount(components.SequenceBlue, components.LevelBright, 1)
			}
		}()
	}

	wg.Wait()

	expectedCount := int64(numGoroutines * incrementsPerGoroutine)
	actualCount := ctx.State.BlueCountBright.Load()

	if actualCount != expectedCount {
		t.Errorf("Expected count %d after concurrent increments, got %d", expectedCount, actualCount)
	}
}

// TestGetAvailableColors tests the color availability tracking
func TestGetAvailableColors(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(ctx)

	// Initially, all 6 colors should be available
	available := spawnSys.getAvailableColors()
	if len(available) != 6 {
		t.Errorf("Expected 6 available colors initially, got %d", len(available))
	}

	// Add some characters of one color
	spawnSys.AddColorCount(components.SequenceBlue, components.LevelBright, 10)
	available = spawnSys.getAvailableColors()
	if len(available) != 5 {
		t.Errorf("Expected 5 available colors after adding one, got %d", len(available))
	}

	// Fill all 6 color slots
	spawnSys.AddColorCount(components.SequenceBlue, components.LevelNormal, 1)
	spawnSys.AddColorCount(components.SequenceBlue, components.LevelDark, 1)
	spawnSys.AddColorCount(components.SequenceGreen, components.LevelBright, 1)
	spawnSys.AddColorCount(components.SequenceGreen, components.LevelNormal, 1)
	spawnSys.AddColorCount(components.SequenceGreen, components.LevelDark, 1)
	available = spawnSys.getAvailableColors()
	if len(available) != 0 {
		t.Errorf("Expected 0 available colors when all 6 are filled, got %d", len(available))
	}

	// Clear one color
	spawnSys.AddColorCount(components.SequenceBlue, components.LevelBright, -10)
	available = spawnSys.getAvailableColors()
	if len(available) != 1 {
		t.Errorf("Expected 1 available color after clearing one, got %d", len(available))
	}
}

// TestSpawnWithNoAvailableColors tests that spawning stops when all 6 colors are present
func TestSpawnWithNoAvailableColors(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	spawnSys := NewSpawnSystem(ctx)

	// Simulate all 6 colors being present
	spawnSys.AddColorCount(components.SequenceBlue, components.LevelBright, 10)
	spawnSys.AddColorCount(components.SequenceBlue, components.LevelNormal, 10)
	spawnSys.AddColorCount(components.SequenceBlue, components.LevelDark, 10)
	spawnSys.AddColorCount(components.SequenceGreen, components.LevelBright, 10)
	spawnSys.AddColorCount(components.SequenceGreen, components.LevelNormal, 10)
	spawnSys.AddColorCount(components.SequenceGreen, components.LevelDark, 10)

	// Add some dummy code blocks
	spawnSys.codeBlocks = []CodeBlock{
		{Lines: []string{"test line 1", "test line 2", "test line 3"}},
	}

	// Get entities before spawn
	beforeCount := len(world.Positions.All())

	// Try to spawn - should do nothing since all colors are present
	spawnSys.spawnSequence(world)

	// Get entities after spawn
	afterCount := len(world.Positions.All())

	if afterCount != beforeCount {
		t.Errorf("Expected no new entities when all 6 colors present, before=%d, after=%d", beforeCount, afterCount)
	}
}