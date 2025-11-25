package systems

// Race condition tests for content system and spawn operations.
// See also:
//   - cleaner_race_test.go: Cleaner system race conditions
//   - boost_race_test.go: Boost/heat system race conditions
//   - race_counters_test.go: Color counter race conditions
//   - race_snapshots_test.go: Snapshot consistency race conditions

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestConcurrentContentRefresh tests concurrent content refresh with spawning sequences
// This verifies that content refresh operations don't race with spawn operations
func TestConcurrentContentRefresh(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(ctx)
	world := engine.NewWorld()
	// Initialize Resources for the SpawnSystem
	engine.AddResource(world.Resources, &engine.ConfigResource{
		GameWidth:    80,
		GameHeight:   24,
		ScreenWidth:  80,
		ScreenHeight: 24,
	})

	// Set up initial test blocks
	initialBlocks := make([]CodeBlock, 10)
	for i := 0; i < 10; i++ {
		initialBlocks[i] = CodeBlock{
			Lines: []string{
				"func example() {",
				"    x := 42",
				"    return x",
				"}",
			},
		}
	}

	spawnSys.contentMutex.Lock()
	spawnSys.codeBlocks = initialBlocks
	spawnSys.contentMutex.Unlock()

	spawnSys.totalBlocks.Store(10)
	spawnSys.nextBlockIndex.Store(0)
	spawnSys.blocksConsumed.Store(0)

	var wg sync.WaitGroup
	errChan := make(chan error, 100)
	stopChan := make(chan struct{})

	// Goroutine 1: Continuously refresh content by triggering wraparound
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopChan:
				return
			default:
				// Force content refresh by consuming all blocks rapidly
				for i := 0; i < 12; i++ {
					block := spawnSys.getNextBlock()
					_ = block
					time.Sleep(1 * time.Millisecond)
				}
			}
		}
	}()

	// Goroutine 2: Simultaneously spawn sequences
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			select {
			case <-stopChan:
				return
			default:
				spawnSys.spawnSequence(world)
				time.Sleep(5 * time.Millisecond)
			}
		}
	}()

	// Goroutine 3: Read content state concurrently
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			select {
			case <-stopChan:
				return
			default:
				spawnSys.contentMutex.RLock()
				blocksLen := len(spawnSys.codeBlocks)
				spawnSys.contentMutex.RUnlock()

				total := spawnSys.totalBlocks.Load()
				idx := spawnSys.nextBlockIndex.Load()

				// Verify consistency
				if total > 0 && blocksLen > 0 && int32(blocksLen) != total {
					errChan <- nil // Signal inconsistency detected
				}
				if idx < 0 || (total > 0 && idx >= total) {
					errChan <- nil // Signal invalid index
				}

				time.Sleep(2 * time.Millisecond)
			}
		}
	}()

	// Goroutine 4: Simulate entity queries (renderer-like behavior)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 150; i++ {
			select {
			case <-stopChan:
				return
			default:
				// Query entities
				entities := world.Positions.All()
				for _, entity := range entities {
					// Simulate reading entity components
					_, _ = world.Positions.Get(entity)
					_, _ = world.Characters.Get(entity)
				}
				time.Sleep(8 * time.Millisecond)
			}
		}
	}()

	// Let tests run for a reasonable duration - reduced from 500ms to 150ms
	time.Sleep(150 * time.Millisecond)
	close(stopChan)

	wg.Wait()

	// Check for errors
	select {
	case <-errChan:
		t.Error("Detected inconsistency during concurrent content refresh and spawning")
	default:
		// No errors, test passed
	}
}

// TestRenderWhileSpawning tests concurrent rendering and spawning/destruction
// This simulates the real game loop where renderer reads entities while spawn system modifies them
func TestRenderWhileSpawning(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(ctx)
	world := engine.NewWorld()
	// Initialize Resources for the SpawnSystem
	engine.AddResource(world.Resources, &engine.ConfigResource{
		GameWidth:    80,
		GameHeight:   24,
		ScreenWidth:  80,
		ScreenHeight: 24,
	})

	// Pre-populate with test blocks
	testBlocks := make([]CodeBlock, 5)
	for i := 0; i < 5; i++ {
		testBlocks[i] = CodeBlock{
			Lines: []string{"test line 1", "test line 2", "test line 3"},
		}
	}

	spawnSys.contentMutex.Lock()
	spawnSys.codeBlocks = testBlocks
	spawnSys.contentMutex.Unlock()

	spawnSys.totalBlocks.Store(5)
	spawnSys.nextBlockIndex.Store(0)
	spawnSys.blocksConsumed.Store(0)

	var wg sync.WaitGroup
	var entityCount atomic.Int32
	var panicCount atomic.Int32
	stopChan := make(chan struct{})

	// Goroutine 1: Continuously spawn entities
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				panicCount.Add(1)
				t.Errorf("Spawn goroutine panicked: %v", r)
			}
		}()

		for i := 0; i < 50; i++ {
			select {
			case <-stopChan:
				return
			default:
				spawnSys.spawnSequence(world)
				entityCount.Add(1)
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	// Goroutine 2: Mock renderer reading entities
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				panicCount.Add(1)
				t.Errorf("Renderer goroutine panicked: %v", r)
			}
		}()

		for i := 0; i < 200; i++ {
			select {
			case <-stopChan:
				return
			default:
				// Simulate what the renderer does: read all entities and their components
				entities := world.Positions.All()

				for _, entity := range entities {
					// Read position
					if pos, ok := world.Positions.Get(entity); ok {
						_ = pos.X
						_ = pos.Y
					}

					// Read character
					if char, ok := world.Characters.Get(entity); ok {
						_ = char.Rune
						_ = char.Style
					}

					// Read sequence
					if seq, ok := world.Sequences.Get(entity); ok {
						_ = seq.ID
						_ = seq.Type
						_ = seq.Level
					}
				}

				time.Sleep(2 * time.Millisecond)
			}
		}
	}()

	// Goroutine 3: Destroy random entities (simulates decay/typing)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				panicCount.Add(1)
				t.Errorf("Destroyer goroutine panicked: %v", r)
			}
		}()

		for i := 0; i < 30; i++ {
			select {
			case <-stopChan:
				return
			default:
				entities := world.Positions.All()
				if len(entities) > 0 {
					// Destroy random entity using DestroyEntity (handles spatial index atomically)
					toDestroy := entities[rand.Intn(len(entities))]
					world.DestroyEntity(toDestroy)
				}
				time.Sleep(15 * time.Millisecond)
			}
		}
	}()

	// Goroutine 4: Read spatial index
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				panicCount.Add(1)
				t.Errorf("Spatial index reader panicked: %v", r)
			}
		}()

		for i := 0; i < 250; i++ {
			select {
			case <-stopChan:
				return
			default:
				// Query random positions in spatial index
				for j := 0; j < 10; j++ {
					x := rand.Intn(80)
					y := rand.Intn(24)
					entity := world.GetEntityAtPosition(x, y)
					_ = entity
				}
				time.Sleep(3 * time.Millisecond)
			}
		}
	}()

	// Let the test run - reduced from 600ms to 150ms
	time.Sleep(150 * time.Millisecond)
	close(stopChan)

	wg.Wait()

	// Verify no panics occurred
	if panicCount.Load() > 0 {
		t.Errorf("Test completed with %d panic(s)", panicCount.Load())
	}

	// Verify system is in consistent state
	entities := world.Positions.All()
	t.Logf("Final entity count: %d", len(entities))
}

// TestContentSwapDuringRead tests reading dataLines while simultaneously refreshing content
// This ensures no panics or corruption when content is swapped mid-read
func TestContentSwapDuringRead(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(ctx)

	// Set up small initial content to trigger frequent swaps
	smallBlocks := []CodeBlock{
		{Lines: []string{"line1", "line2"}},
		{Lines: []string{"line3", "line4"}},
	}

	spawnSys.contentMutex.Lock()
	spawnSys.codeBlocks = smallBlocks
	spawnSys.contentMutex.Unlock()

	spawnSys.totalBlocks.Store(2)
	spawnSys.nextBlockIndex.Store(0)
	spawnSys.blocksConsumed.Store(0)

	var wg sync.WaitGroup
	var readErrors atomic.Int32
	var swapCount atomic.Int32
	stopChan := make(chan struct{})

	// Goroutine 1: Continuously read content (simulates multiple readers)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					readErrors.Add(1)
					t.Errorf("Reader %d panicked: %v", readerID, r)
				}
			}()

			for {
				select {
				case <-stopChan:
					return
				default:
					// Read dataLines with proper locking
					spawnSys.contentMutex.RLock()
					blocks := spawnSys.codeBlocks
					blocksLen := len(blocks)
					spawnSys.contentMutex.RUnlock()

					// Iterate through blocks
					for j := 0; j < blocksLen; j++ {
						spawnSys.contentMutex.RLock()
						if j < len(spawnSys.codeBlocks) {
							block := spawnSys.codeBlocks[j]
							// Access lines
							for _, line := range block.Lines {
								_ = line
							}
						}
						spawnSys.contentMutex.RUnlock()
					}

					time.Sleep(1 * time.Millisecond)
				}
			}
		}(i)
	}

	// Goroutine 2: Continuously trigger content refresh/swap
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Swap goroutine panicked: %v", r)
			}
		}()

		for i := 0; i < 100; i++ {
			select {
			case <-stopChan:
				return
			default:
				// Consume blocks to trigger swap
				spawnSys.getNextBlock()
				spawnSys.getNextBlock()
				spawnSys.getNextBlock() // This will trigger wraparound and swap

				swapCount.Add(1)
				time.Sleep(5 * time.Millisecond)
			}
		}
	}()

	// Goroutine 3: Read atomic counters
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopChan:
				return
			default:
				total := spawnSys.totalBlocks.Load()
				idx := spawnSys.nextBlockIndex.Load()
				consumed := spawnSys.blocksConsumed.Load()

				// Verify atomicity: index should never exceed total
				if total > 0 && idx >= total {
					readErrors.Add(1)
				}
				// Consumed should never exceed total
				if total > 0 && consumed > total {
					readErrors.Add(1)
				}

				time.Sleep(500 * time.Microsecond)
			}
		}
	}()

	// Run test - reduced from 700ms to 150ms
	time.Sleep(150 * time.Millisecond)
	close(stopChan)

	wg.Wait()

	t.Logf("Completed with %d content swaps", swapCount.Load())

	// Verify no errors
	if readErrors.Load() > 0 {
		t.Errorf("Detected %d read errors or panics during content swap", readErrors.Load())
	}
}

// TestStressContentSystem performs intensive stress testing on content system
// with rapid refreshes and multiple concurrent readers/writers
func TestStressContentSystem(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(ctx)
	world := engine.NewWorld()
	// Initialize Resources for the SpawnSystem
	engine.AddResource(world.Resources, &engine.ConfigResource{
		GameWidth:    80,
		GameHeight:   24,
		ScreenWidth:  80,
		ScreenHeight: 24,
	})

	// Pre-populate with varied content
	testBlocks := make([]CodeBlock, 15)
	for i := 0; i < 15; i++ {
		lines := make([]string, 3+rand.Intn(5))
		for j := range lines {
			lines[j] = randomString(20 + rand.Intn(30))
		}
		testBlocks[i] = CodeBlock{Lines: lines}
	}

	spawnSys.contentMutex.Lock()
	spawnSys.codeBlocks = testBlocks
	spawnSys.contentMutex.Unlock()

	spawnSys.totalBlocks.Store(15)
	spawnSys.nextBlockIndex.Store(0)
	spawnSys.blocksConsumed.Store(0)

	var wg sync.WaitGroup
	var totalOps atomic.Int64
	var errorCount atomic.Int32
	stopChan := make(chan struct{})

	// Launch 10 reader goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errorCount.Add(1)
					t.Errorf("Reader %d panicked: %v", id, r)
				}
			}()

			ops := 0
			for {
				select {
				case <-stopChan:
					return
				default:
					// Read operations
					spawnSys.contentMutex.RLock()
					blocks := spawnSys.codeBlocks
					for _, block := range blocks {
						for _, line := range block.Lines {
							_ = len(line)
						}
					}
					spawnSys.contentMutex.RUnlock()

					// Read atomic state
					_ = spawnSys.totalBlocks.Load()
					_ = spawnSys.nextBlockIndex.Load()
					_ = spawnSys.blocksConsumed.Load()
					_ = spawnSys.isRefreshing.Load()

					// Read color counters
					_ = ctx.State.BlueCountBright.Load()
					_ = ctx.State.GreenCountNormal.Load()

					ops++
					if ops%100 == 0 {
						totalOps.Add(100)
					}

					// Small sleep to allow other goroutines
					time.Sleep(100 * time.Microsecond)
				}
			}
		}(i)
	}

	// Launch 5 writer goroutines (spawning)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errorCount.Add(1)
					t.Errorf("Writer %d panicked: %v", id, r)
				}
			}()

			for j := 0; j < 50; j++ {
				select {
				case <-stopChan:
					return
				default:
					spawnSys.spawnSequence(world)
					time.Sleep(10 * time.Millisecond)
				}
			}
		}(i)
	}

	// Launch 3 content refresh goroutines
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errorCount.Add(1)
					t.Errorf("Refresher %d panicked: %v", id, r)
				}
			}()

			for j := 0; j < 30; j++ {
				select {
				case <-stopChan:
					return
				default:
					// Force rapid content consumption
					for k := 0; k < 20; k++ {
						_ = spawnSys.getNextBlock()
					}
					time.Sleep(20 * time.Millisecond)
				}
			}
		}(i)
	}

	// Launch entity destroyer
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("Destroyer panicked: %v", r)
			}
		}()

		for {
			select {
			case <-stopChan:
				return
			default:
				entities := world.Positions.All()
				if len(entities) > 10 {
					// Destroy several entities using DestroyEntity
					for i := 0; i < 5 && i < len(entities); i++ {
						world.DestroyEntity(entities[i])
					}
				}
				time.Sleep(25 * time.Millisecond)
			}
		}
	}()

	// Launch consistency checker
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("Consistency checker panicked: %v", r)
			}
		}()

		for {
			select {
			case <-stopChan:
				return
			default:
				// Check atomic consistency
				total := spawnSys.totalBlocks.Load()
				idx := spawnSys.nextBlockIndex.Load()
				consumed := spawnSys.blocksConsumed.Load()

				// Verify invariants
				if total > 0 {
					if idx < 0 || idx >= total {
						errorCount.Add(1)
						t.Errorf("Invalid index: idx=%d, total=%d", idx, total)
					}
					if consumed < 0 || consumed > total {
						errorCount.Add(1)
						t.Errorf("Invalid consumed: consumed=%d, total=%d", consumed, total)
					}
				}

				time.Sleep(5 * time.Millisecond)
			}
		}
	}()

	// Run stress test - reduced from 1s to 200ms
	time.Sleep(200 * time.Millisecond)
	close(stopChan)

	wg.Wait()

	t.Logf("Completed stress test with ~%d total operations", totalOps.Load())

	// Verify no errors
	if errorCount.Load() > 0 {
		t.Errorf("Stress test detected %d errors", errorCount.Load())
	}

	// Verify system is still in valid state
	total := spawnSys.totalBlocks.Load()
	idx := spawnSys.nextBlockIndex.Load()

	if total > 0 && (idx < 0 || idx >= total) {
		t.Errorf("System left in invalid state: idx=%d, total=%d", idx, total)
	}
}

// randomString generates a random string of given length for testing
func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 "
	result := make([]byte, length)
	for i := range result {
		result[i] = charset[rand.Intn(len(charset))]
	}
	return string(result)
}
