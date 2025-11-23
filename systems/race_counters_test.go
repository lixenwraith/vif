package systems

// Race condition tests for color counter updates across systems.
// See also:
//   - cleaner_race_test.go: Cleaner system race conditions
//   - boost_race_test.go: Boost/heat system race conditions
//   - race_content_test.go: Content system race conditions
//   - race_snapshots_test.go: Snapshot consistency race conditions

import (
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestConcurrentColorCounterUpdates tests cross-system color counter race conditions.
// Simulates spawn (increment), score (decrement), and render (read) systems accessing counters concurrently.
// For basic atomic increment tests, see TestColorCountersConcurrency in spawn_file_based_test.go.
func TestConcurrentColorCounterUpdates(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(ctx)

	var wg sync.WaitGroup
	stopChan := make(chan struct{})

	// Goroutine 1: Increment blue counters (simulates spawning)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			select {
			case <-stopChan:
				return
			default:
				spawnSys.AddColorCount(components.SequenceBlue, components.LevelBright, 1)
				spawnSys.AddColorCount(components.SequenceBlue, components.LevelNormal, 2)
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// Goroutine 2: Increment green counters (simulates spawning)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			select {
			case <-stopChan:
				return
			default:
				spawnSys.AddColorCount(components.SequenceGreen, components.LevelBright, 1)
				spawnSys.AddColorCount(components.SequenceGreen, components.LevelDark, 1)
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// Goroutine 3: Decrement counters (simulates typing/scoring)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			select {
			case <-stopChan:
				return
			default:
				spawnSys.AddColorCount(components.SequenceBlue, components.LevelBright, -1)
				spawnSys.AddColorCount(components.SequenceGreen, components.LevelBright, -1)
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// Goroutine 4: Read all counters (simulates rendering/decisions)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			select {
			case <-stopChan:
				return
			default:
				_ = ctx.State.BlueCountBright.Load()
				_ = ctx.State.BlueCountNormal.Load()
				_ = ctx.State.BlueCountDark.Load()
				_ = ctx.State.GreenCountBright.Load()
				_ = ctx.State.GreenCountNormal.Load()
				_ = ctx.State.GreenCountDark.Load()
				time.Sleep(500 * time.Microsecond)
			}
		}
	}()

	// Let test run - reduced from 600ms to 150ms
	time.Sleep(150 * time.Millisecond)
	close(stopChan)

	wg.Wait()

	// Verify counters are accessible and consistent
	blueCount := ctx.State.BlueCountBright.Load()
	greenCount := ctx.State.GreenCountBright.Load()

	t.Logf("Final blue bright count: %d, green bright count: %d", blueCount, greenCount)

	// Counts should be non-negative (we added more than we subtracted)
	if blueCount < 0 {
		t.Errorf("Blue count is negative: %d", blueCount)
	}
	if greenCount < 0 {
		t.Errorf("Green count is negative: %d", greenCount)
	}
}