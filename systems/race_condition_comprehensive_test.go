package systems

// Race condition tests for spawn system and content management.
// See also:
//   - cleaner_race_test.go: Cleaner system race conditions
//   - boost_race_test.go: Boost/heat system race conditions

import (
	"math/rand"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestConcurrentContentRefresh tests concurrent content refresh with spawning sequences
// This verifies that content refresh operations don't race with spawn operations
func TestConcurrentContentRefresh(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(80, 24, 40, 12, ctx)
	world := engine.NewWorld()

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
				entities := world.GetEntitiesWith()
				for _, entity := range entities {
					// Simulate reading entity components
					_, _ = world.GetComponent(entity, reflect.TypeOf(components.PositionComponent{}))
					_, _ = world.GetComponent(entity, reflect.TypeOf(components.CharacterComponent{}))
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

	spawnSys := NewSpawnSystem(80, 24, 40, 12, ctx)
	world := engine.NewWorld()

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
				entities := world.GetEntitiesWith()

				for _, entity := range entities {
					// Read position
					if pos, ok := world.GetComponent(entity, reflect.TypeOf(components.PositionComponent{})); ok {
						posComp := pos.(components.PositionComponent)
						_ = posComp.X
						_ = posComp.Y
					}

					// Read character
					if char, ok := world.GetComponent(entity, reflect.TypeOf(components.CharacterComponent{})); ok {
						charComp := char.(components.CharacterComponent)
						_ = charComp.Rune
						_ = charComp.Style
					}

					// Read sequence
					if seq, ok := world.GetComponent(entity, reflect.TypeOf(components.SequenceComponent{})); ok {
						seqComp := seq.(components.SequenceComponent)
						_ = seqComp.ID
						_ = seqComp.Type
						_ = seqComp.Level
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
				entities := world.GetEntitiesWith()
				if len(entities) > 0 {
					// Destroy random entity
					toDestroy := entities[rand.Intn(len(entities))]

					// Remove from spatial index first (as per architecture requirements)
					if pos, ok := world.GetComponent(toDestroy, reflect.TypeOf(components.PositionComponent{})); ok {
						posComp := pos.(components.PositionComponent)
						world.RemoveFromSpatialIndex(posComp.X, posComp.Y)
					}

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
	entities := world.GetEntitiesWith()
	t.Logf("Final entity count: %d", len(entities))
}

// TestContentSwapDuringRead tests reading dataLines while simultaneously refreshing content
// This ensures no panics or corruption when content is swapped mid-read
func TestContentSwapDuringRead(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(80, 24, 40, 12, ctx)

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

	spawnSys := NewSpawnSystem(80, 24, 40, 12, ctx)
	world := engine.NewWorld()

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
					_ = spawnSys.GetColorCount(components.SequenceBlue, components.LevelBright)
					_ = spawnSys.GetColorCount(components.SequenceGreen, components.LevelNormal)

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
				entities := world.GetEntitiesWith()
				if len(entities) > 10 {
					// Destroy several entities
					for i := 0; i < 5 && i < len(entities); i++ {
						entity := entities[i]
						if pos, ok := world.GetComponent(entity, reflect.TypeOf(components.PositionComponent{})); ok {
							posComp := pos.(components.PositionComponent)
							world.RemoveFromSpatialIndex(posComp.X, posComp.Y)
						}
						world.DestroyEntity(entity)
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

// TestConcurrentColorCounterUpdates tests cross-system color counter race conditions.
// Simulates spawn (increment), score (decrement), and render (read) systems accessing counters concurrently.
// For basic atomic increment tests, see TestColorCountersConcurrency in spawn_file_based_test.go.
func TestConcurrentColorCounterUpdates(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(80, 24, 40, 12, ctx)

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
				_ = spawnSys.GetColorCount(components.SequenceBlue, components.LevelBright)
				_ = spawnSys.GetColorCount(components.SequenceBlue, components.LevelNormal)
				_ = spawnSys.GetColorCount(components.SequenceBlue, components.LevelDark)
				_ = spawnSys.GetColorCount(components.SequenceGreen, components.LevelBright)
				_ = spawnSys.GetColorCount(components.SequenceGreen, components.LevelNormal)
				_ = spawnSys.GetColorCount(components.SequenceGreen, components.LevelDark)
				time.Sleep(500 * time.Microsecond)
			}
		}
	}()

	// Let test run - reduced from 600ms to 150ms
	time.Sleep(150 * time.Millisecond)
	close(stopChan)

	wg.Wait()

	// Verify counters are accessible and consistent
	blueCount := spawnSys.GetColorCount(components.SequenceBlue, components.LevelBright)
	greenCount := spawnSys.GetColorCount(components.SequenceGreen, components.LevelBright)

	t.Logf("Final blue bright count: %d, green bright count: %d", blueCount, greenCount)

	// Counts should be non-negative (we added more than we subtracted)
	if blueCount < 0 {
		t.Errorf("Blue count is negative: %d", blueCount)
	}
	if greenCount < 0 {
		t.Errorf("Green count is negative: %d", greenCount)
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

// TestSnapshotConsistencyUnderRapidChanges tests that GameState snapshots remain consistent
// even when state is changing rapidly across multiple goroutines
func TestSnapshotConsistencyUnderRapidChanges(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	var wg sync.WaitGroup
	stopChan := make(chan struct{})
	var inconsistencyCount atomic.Int32

	// Writer 1: Rapidly update spawn state
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			select {
			case <-stopChan:
				return
			default:
				ctx.State.UpdateSpawnRate(i%100, 100)
				time.Sleep(time.Millisecond)
			}
		}
	}()

	// Writer 2: Rapidly update atomic state (heat, score, cursor)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			select {
			case <-stopChan:
				return
			default:
				ctx.State.SetHeat(i % 80)
				ctx.State.SetScore(i * 10)
				ctx.State.SetCursorX(i % 80)
				ctx.State.SetCursorY(i % 24)
				time.Sleep(500 * time.Microsecond)
			}
		}
	}()

	// Writer 3: Rapidly update color counters
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			select {
			case <-stopChan:
				return
			default:
				ctx.State.AddColorCount(0, 2, 1) // Blue Bright
				// Only decrement if positive to avoid negative counts
				if ctx.State.ReadColorCounts().GreenNormal > 0 {
					ctx.State.AddColorCount(1, 1, -1) // Green Normal
				}
				time.Sleep(time.Millisecond)
			}
		}
	}()

	// Reader goroutines: Take snapshots and verify consistency
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				select {
				case <-stopChan:
					return
				default:
					// Take all snapshot types
					spawnSnap := ctx.State.ReadSpawnState()
					colorSnap := ctx.State.ReadColorCounts()
					cursorSnap := ctx.State.ReadCursorPosition()
					heat, score := ctx.State.ReadHeatAndScore()

					// Verify spawn snapshot internal consistency
					expectedDensity := float64(spawnSnap.EntityCount) / float64(spawnSnap.MaxEntities)
					if spawnSnap.ScreenDensity != expectedDensity {
						inconsistencyCount.Add(1)
						t.Errorf("Reader %d: Spawn snapshot inconsistent - density=%f, expected=%f",
							readerID, spawnSnap.ScreenDensity, expectedDensity)
					}

					// Verify no negative values
					if colorSnap.BlueBright < 0 || colorSnap.GreenNormal < 0 {
						inconsistencyCount.Add(1)
						t.Errorf("Reader %d: Color counters negative", readerID)
					}

					if cursorSnap.X < 0 || cursorSnap.Y < 0 {
						inconsistencyCount.Add(1)
						t.Errorf("Reader %d: Cursor negative: (%d, %d)", readerID, cursorSnap.X, cursorSnap.Y)
					}

					if heat < 0 || score < 0 {
						inconsistencyCount.Add(1)
						t.Errorf("Reader %d: Heat or score negative: heat=%d, score=%d", readerID, heat, score)
					}

					// Verify cursor bounds
					if cursorSnap.X >= 80 || cursorSnap.Y >= 24 {
						inconsistencyCount.Add(1)
						t.Errorf("Reader %d: Cursor out of bounds: (%d, %d)", readerID, cursorSnap.X, cursorSnap.Y)
					}

					time.Sleep(500 * time.Microsecond)
				}
			}
		}(i)
	}

	// Run test
	time.Sleep(200 * time.Millisecond)
	close(stopChan)
	wg.Wait()

	if inconsistencyCount.Load() > 0 {
		t.Errorf("Detected %d snapshot inconsistencies", inconsistencyCount.Load())
	}
}

// TestSnapshotImmutabilityWithSystemUpdates tests that snapshots remain immutable
// even while systems are actively modifying GameState
func TestSnapshotImmutabilityWithSystemUpdates(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	// Set initial state
	ctx.State.UpdateSpawnRate(50, 100)
	ctx.State.ActivateGoldSequence(42, 10*time.Second)
	ctx.State.SetHeat(50)
	ctx.State.SetScore(1000)

	// Take initial snapshots
	initialSpawn := ctx.State.ReadSpawnState()
	initialGold := ctx.State.ReadGoldState()
	initialHeat, initialScore := ctx.State.ReadHeatAndScore()

	// Verify initial values
	if initialSpawn.EntityCount != 50 {
		t.Fatalf("Expected initial entity count 50, got %d", initialSpawn.EntityCount)
	}
	if !initialGold.Active {
		t.Fatal("Expected gold to be initially active")
	}
	if initialHeat != 50 || initialScore != 1000 {
		t.Fatalf("Expected heat=50, score=1000, got heat=%d, score=%d", initialHeat, initialScore)
	}

	var wg sync.WaitGroup
	stopChan := make(chan struct{})

	// Rapidly modify state in multiple goroutines
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			select {
			case <-stopChan:
				return
			default:
				ctx.State.UpdateSpawnRate(i, 100)
				ctx.State.SetHeat(i * 10)
				ctx.State.SetScore(i * 100)
				time.Sleep(time.Millisecond)
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			select {
			case <-stopChan:
				return
			default:
				if i == 5 {
					ctx.State.DeactivateGoldSequence()
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	// Let modifications happen
	time.Sleep(100 * time.Millisecond)
	close(stopChan)
	wg.Wait()

	// Verify original snapshots are unchanged
	if initialSpawn.EntityCount != 50 {
		t.Errorf("Initial spawn snapshot was mutated: expected 50, got %d", initialSpawn.EntityCount)
	}
	if !initialGold.Active {
		t.Error("Initial gold snapshot was mutated: expected active=true")
	}
	if initialGold.SequenceID != 42 {
		t.Errorf("Initial gold snapshot sequence ID mutated: expected 42, got %d", initialGold.SequenceID)
	}
	if initialHeat != 50 {
		t.Errorf("Initial heat mutated: expected 50, got %d", initialHeat)
	}
	if initialScore != 1000 {
		t.Errorf("Initial score mutated: expected 1000, got %d", initialScore)
	}

	// Verify new snapshots show changed state
	newSpawn := ctx.State.ReadSpawnState()
	newGold := ctx.State.ReadGoldState()

	if newSpawn.EntityCount == 50 {
		t.Error("New spawn snapshot didn't reflect changes")
	}
	if newGold.Active {
		t.Error("New gold snapshot should show inactive state")
	}
}

// TestNoPartialSnapshotReads tests that snapshots never show partial state updates
// This verifies that mutex-protected snapshots always capture consistent multi-field state
func TestNoPartialSnapshotReads(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	// Initialize with known state to avoid initial inconsistency
	ctx.State.UpdateSpawnRate(50, 100)

	var wg sync.WaitGroup
	stopChan := make(chan struct{})
	var partialReadCount atomic.Int32

	// Writer: Update related fields together
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 1; i <= 300; i++ { // Start from 1 to avoid zero density
			select {
			case <-stopChan:
				return
			default:
				// UpdateSpawnRate should atomically update multiple related fields
				ctx.State.UpdateSpawnRate(i, 100)
				time.Sleep(time.Millisecond)
			}
		}
	}()

	// Readers: Verify snapshots are never partial
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 150; j++ {
				select {
				case <-stopChan:
					return
				default:
					snapshot := ctx.State.ReadSpawnState()

					// Verify internal consistency - all fields should be from same update
					expectedDensity := float64(snapshot.EntityCount) / float64(snapshot.MaxEntities)
					if snapshot.ScreenDensity != expectedDensity {
						partialReadCount.Add(1)
						t.Errorf("Partial read detected: density=%f, expected=%f (count=%d, max=%d)",
							snapshot.ScreenDensity, expectedDensity, snapshot.EntityCount, snapshot.MaxEntities)
					}

					// Verify rate multiplier matches density
					var expectedRate float64
					if snapshot.ScreenDensity < 0.3 {
						expectedRate = 2.0
					} else if snapshot.ScreenDensity > 0.7 {
						expectedRate = 0.5
					} else {
						expectedRate = 1.0
					}

					if snapshot.RateMultiplier != expectedRate {
						partialReadCount.Add(1)
						t.Errorf("Partial read detected: rate=%f, expected=%f (density=%f)",
							snapshot.RateMultiplier, expectedRate, snapshot.ScreenDensity)
					}

					time.Sleep(300 * time.Microsecond)
				}
			}
		}()
	}

	// Run test
	time.Sleep(200 * time.Millisecond)
	close(stopChan)
	wg.Wait()

	if partialReadCount.Load() > 0 {
		t.Errorf("Detected %d partial snapshot reads", partialReadCount.Load())
	}
}

// TestPhaseSnapshotConsistency tests that phase snapshots remain consistent
// during rapid phase transitions (simulating gold→decay→normal cycle)
func TestPhaseSnapshotConsistency(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	var wg sync.WaitGroup
	stopChan := make(chan struct{})
	var inconsistencyCount atomic.Int32

	// Writer: Simulate phase transitions
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 1; i <= 50; i++ { // Start from 1 to ensure non-zero sequence IDs
			select {
			case <-stopChan:
				return
			default:
				// Cycle through phases
				ctx.State.ActivateGoldSequence(i, 10*time.Second)
				time.Sleep(5 * time.Millisecond)
				ctx.State.DeactivateGoldSequence()
				time.Sleep(5 * time.Millisecond)
				ctx.State.StartDecayTimer(80, 60, 50)
				time.Sleep(5 * time.Millisecond)
			}
		}
	}()

	// Readers: Verify phase and related state consistency
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				select {
				case <-stopChan:
					return
				default:
					goldSnap := ctx.State.ReadGoldState()
					decaySnap := ctx.State.ReadDecayState()

					// Verify phase-state consistency within each snapshot
					// Note: Different snapshots might be from different moments,
					// but each individual snapshot should be internally consistent

					// Gold snapshot should be internally consistent
					if goldSnap.Active && goldSnap.SequenceID == 0 {
						inconsistencyCount.Add(1)
						t.Error("Gold snapshot inconsistent: Active but SequenceID=0")
					}

					// Decay snapshot should be internally consistent
					if decaySnap.Animating && decaySnap.TimerActive {
						inconsistencyCount.Add(1)
						t.Error("Decay snapshot inconsistent: Both animating and timer active")
					}

					time.Sleep(time.Millisecond)
				}
			}
		}()
	}

	// Run test
	time.Sleep(200 * time.Millisecond)
	close(stopChan)
	wg.Wait()

	if inconsistencyCount.Load() > 0 {
		t.Errorf("Detected %d phase snapshot inconsistencies", inconsistencyCount.Load())
	}
}

// TestMultiSnapshotAtomicity tests taking multiple snapshots in rapid succession
// Verifies that even if state changes between snapshots, each snapshot is internally consistent
func TestMultiSnapshotAtomicity(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	// Initialize state
	ctx.State.UpdateSpawnRate(50, 100)
	ctx.State.AddColorCount(0, 2, 10) // Blue Bright
	ctx.State.SetHeat(50)
	ctx.State.SetScore(500)

	var wg sync.WaitGroup
	stopChan := make(chan struct{})
	var errorCount atomic.Int32

	// Rapid state modifier
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			select {
			case <-stopChan:
				return
			default:
				ctx.State.UpdateSpawnRate(i%100, 100)
				ctx.State.AddColorCount(0, 2, 1)
				ctx.State.SetHeat((i * 3) % 80)
				ctx.State.SetScore(i * 10)
				time.Sleep(500 * time.Microsecond)
			}
		}
	}()

	// Multiple snapshot readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				select {
				case <-stopChan:
					return
				default:
					// Take multiple snapshots in rapid succession
					spawn1 := ctx.State.ReadSpawnState()
					color1 := ctx.State.ReadColorCounts()
					heat1, score1 := ctx.State.ReadHeatAndScore()

					// Each snapshot should be internally valid
					if spawn1.EntityCount < 0 || spawn1.EntityCount > spawn1.MaxEntities {
						errorCount.Add(1)
					}

					if color1.BlueBright < 0 {
						errorCount.Add(1)
					}

					if heat1 < 0 || score1 < 0 {
						errorCount.Add(1)
					}

					// Verify spawn snapshot internal consistency
					expectedDensity := float64(spawn1.EntityCount) / float64(spawn1.MaxEntities)
					if spawn1.ScreenDensity != expectedDensity {
						errorCount.Add(1)
					}

					time.Sleep(time.Millisecond)
				}
			}
		}()
	}

	// Run test
	time.Sleep(150 * time.Millisecond)
	close(stopChan)
	wg.Wait()

	if errorCount.Load() > 0 {
		t.Errorf("Detected %d snapshot atomicity errors", errorCount.Load())
	}
}
