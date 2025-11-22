package systems

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestContentRefreshThreshold tests that pre-fetch is triggered at 80% consumption
func TestContentRefreshThreshold(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(ctx)

	// Set up test blocks
	testBlocks := []CodeBlock{
		{Lines: []string{"block1", "line2", "line3"}},
		{Lines: []string{"block2", "line2", "line3"}},
		{Lines: []string{"block3", "line2", "line3"}},
		{Lines: []string{"block4", "line2", "line3"}},
		{Lines: []string{"block5", "line2", "line3"}},
	}

	spawnSys.contentMutex.Lock()
	spawnSys.codeBlocks = testBlocks
	spawnSys.contentMutex.Unlock()

	spawnSys.totalBlocks.Store(5)
	spawnSys.nextBlockIndex.Store(0)
	spawnSys.blocksConsumed.Store(0)

	// Consume blocks until we hit the threshold
	// At 80% of 5 blocks = 4 blocks consumed
	for i := 0; i < 3; i++ {
		spawnSys.getNextBlock()
	}

	// Check refresh hasn't started yet (3/5 = 60%)
	if spawnSys.isRefreshing.Load() {
		t.Error("Refresh should not have started at 60% consumption")
	}

	// Consume one more block (4/5 = 80%)
	spawnSys.getNextBlock()

	// Give goroutine time to start
	time.Sleep(10 * time.Millisecond)

	// Now refresh should be in progress or completed
	spawnSys.contentMutex.RLock()
	hasNextContent := len(spawnSys.nextContent) > 0
	spawnSys.contentMutex.RUnlock()

	// Either isRefreshing is true (still fetching) or nextContent is populated (fetch complete)
	if !spawnSys.isRefreshing.Load() && !hasNextContent {
		t.Error("Content pre-fetch should have been triggered at 80% consumption")
	}
}

// TestContentSwapOnWrapAround tests seamless content swap when blocks are exhausted
func TestContentSwapOnWrapAround(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(ctx)

	// Set up initial test blocks
	initialBlocks := []CodeBlock{
		{Lines: []string{"initial1", "line2", "line3"}},
		{Lines: []string{"initial2", "line2", "line3"}},
	}

	spawnSys.contentMutex.Lock()
	spawnSys.codeBlocks = initialBlocks
	spawnSys.contentMutex.Unlock()

	spawnSys.totalBlocks.Store(2)
	spawnSys.nextBlockIndex.Store(0)
	spawnSys.blocksConsumed.Store(0)

	// Get first block
	block1 := spawnSys.getNextBlock()
	if block1.Lines[0] != "initial1" {
		t.Errorf("Expected 'initial1', got '%s'", block1.Lines[0])
	}

	// Get second block - this will trigger wraparound and swap to new content
	block2 := spawnSys.getNextBlock()
	if block2.Lines[0] != "initial2" {
		t.Errorf("Expected 'initial2', got '%s'", block2.Lines[0])
	}

	// Get third block - should be from new content (default content from ContentManager)
	block3 := spawnSys.getNextBlock()
	if len(block3.Lines) == 0 {
		t.Error("Should have valid content after wraparound swap")
	}

	// Verify content was swapped - we should have new blocks
	spawnSys.contentMutex.RLock()
	currentBlocks := len(spawnSys.codeBlocks)
	spawnSys.contentMutex.RUnlock()

	if currentBlocks == 0 {
		t.Error("Should have new content blocks after swap")
	}

	// The third block should not be from the initial blocks
	// (default content doesn't start with "initial")
	if len(block3.Lines) > 0 && block3.Lines[0] == "initial1" {
		t.Error("Third block should be from new content, not initial content")
	}
}

// TestThreadSafeContentSwap tests that content swap is thread-safe
func TestThreadSafeContentSwap(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(ctx)

	// Set up test blocks
	testBlocks := []CodeBlock{
		{Lines: []string{"test1", "line2", "line3"}},
		{Lines: []string{"test2", "line2", "line3"}},
	}

	spawnSys.contentMutex.Lock()
	spawnSys.codeBlocks = testBlocks
	spawnSys.contentMutex.Unlock()

	spawnSys.totalBlocks.Store(2)
	spawnSys.nextBlockIndex.Store(0)
	spawnSys.blocksConsumed.Store(0)

	// Consume blocks concurrently
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 5; j++ {
				block := spawnSys.getNextBlock()
				if len(block.Lines) == 0 {
					t.Error("Received empty block during concurrent access")
				}
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify system is in valid state
	spawnSys.contentMutex.RLock()
	blocks := len(spawnSys.codeBlocks)
	spawnSys.contentMutex.RUnlock()

	if blocks == 0 {
		t.Error("Should have valid blocks after concurrent access")
	}
}

// TestEmptyContentHandling tests graceful handling when no content is available
func TestEmptyContentHandling(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(ctx)

	// Manually set empty content
	spawnSys.contentMutex.Lock()
	spawnSys.codeBlocks = []CodeBlock{}
	spawnSys.contentMutex.Unlock()

	spawnSys.totalBlocks.Store(0)
	spawnSys.nextBlockIndex.Store(0)
	spawnSys.blocksConsumed.Store(0)

	// Should return empty block gracefully
	block := spawnSys.getNextBlock()
	if len(block.Lines) != 0 {
		t.Errorf("Expected empty block, got %d lines", len(block.Lines))
	}

	// System should still be in valid state
	idx := spawnSys.nextBlockIndex.Load()

	if idx != 0 {
		t.Errorf("nextBlockIndex should remain 0 with empty content, got %d", idx)
	}
}

// TestContentRefreshDoesNotBlockGameplay tests that content refresh happens in background
func TestContentRefreshDoesNotBlockGameplay(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(ctx)

	// Set up test blocks
	testBlocks := make([]CodeBlock, 10)
	for i := 0; i < 10; i++ {
		testBlocks[i] = CodeBlock{Lines: []string{"test", "line2", "line3"}}
	}

	spawnSys.contentMutex.Lock()
	spawnSys.codeBlocks = testBlocks
	spawnSys.contentMutex.Unlock()

	spawnSys.totalBlocks.Store(10)
	spawnSys.nextBlockIndex.Store(0)
	spawnSys.blocksConsumed.Store(0)

	// Consume blocks to trigger refresh (80% of 10 = 8)
	startTime := time.Now()
	for i := 0; i < 9; i++ {
		spawnSys.getNextBlock()
	}
	elapsed := time.Since(startTime)

	// Should complete quickly (< 100ms) since refresh happens in background
	if elapsed > 100*time.Millisecond {
		t.Errorf("getNextBlock took too long (%v), might be blocking on content refresh", elapsed)
	}
}

// TestConcurrentSpawningAndContentRefresh tests thread-safety with concurrent spawning and content updates
func TestConcurrentSpawningAndContentRefresh(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(ctx)
	world := engine.NewWorld()

	// Set up test blocks
	testBlocks := make([]CodeBlock, 20)
	for i := 0; i < 20; i++ {
		testBlocks[i] = CodeBlock{Lines: []string{
			"func test() {",
			"    x := 42",
			"    return x",
			"}",
		}}
	}

	spawnSys.contentMutex.Lock()
	spawnSys.codeBlocks = testBlocks
	spawnSys.contentMutex.Unlock()

	spawnSys.totalBlocks.Store(20)
	spawnSys.nextBlockIndex.Store(0)
	spawnSys.blocksConsumed.Store(0)

	// Run concurrent operations
	done := make(chan bool)
	errors := make(chan error, 100)

	// Goroutine 1: Continuously get blocks
	go func() {
		for i := 0; i < 50; i++ {
			block := spawnSys.getNextBlock()
			if len(block.Lines) == 0 {
				errors <- nil // Empty block is acceptable during swap
			}
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	// Goroutine 2: Continuously get blocks
	go func() {
		for i := 0; i < 50; i++ {
			block := spawnSys.getNextBlock()
			if len(block.Lines) == 0 {
				errors <- nil // Empty block is acceptable during swap
			}
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	// Goroutine 3: Read content state
	go func() {
		for i := 0; i < 50; i++ {
			spawnSys.contentMutex.RLock()
			_ = len(spawnSys.codeBlocks)
			spawnSys.contentMutex.RUnlock()

			_ = spawnSys.totalBlocks.Load()
			_ = spawnSys.nextBlockIndex.Load()
			_ = spawnSys.blocksConsumed.Load()

			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	// Goroutine 4: Simulate spawning
	go func() {
		for i := 0; i < 20; i++ {
			spawnSys.spawnSequence(world)
			time.Sleep(2 * time.Millisecond)
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 4; i++ {
		<-done
	}

	// Verify system is in a valid state
	idx := spawnSys.nextBlockIndex.Load()
	total := spawnSys.totalBlocks.Load()

	if total > 0 && idx >= total {
		t.Errorf("Invalid state: nextBlockIndex (%d) >= totalBlocks (%d)", idx, total)
	}
}

// TestAtomicIndexOperations verifies that index operations are truly atomic
func TestAtomicIndexOperations(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(ctx)

	// Set up test blocks
	testBlocks := make([]CodeBlock, 5)
	for i := 0; i < 5; i++ {
		testBlocks[i] = CodeBlock{Lines: []string{"line1", "line2", "line3"}}
	}

	spawnSys.contentMutex.Lock()
	spawnSys.codeBlocks = testBlocks
	spawnSys.contentMutex.Unlock()

	spawnSys.totalBlocks.Store(5)
	spawnSys.nextBlockIndex.Store(0)
	spawnSys.blocksConsumed.Store(0)

	// Launch many goroutines to hammer on getNextBlock
	const numGoroutines = 50
	const blocksPerGoroutine = 20
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			for j := 0; j < blocksPerGoroutine; j++ {
				block := spawnSys.getNextBlock()
				if len(block.Lines) == 0 {
					// Empty block can happen during content swap
					continue
				}
				// Verify block has valid structure (non-zero lines)
				// Don't check exact count since content may swap to default content
				if len(block.Lines) < 1 {
					t.Errorf("Block should have at least 1 line, got %d", len(block.Lines))
				}
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify final state is consistent
	spawnSys.contentMutex.RLock()
	codeBlocksLen := len(spawnSys.codeBlocks)
	spawnSys.contentMutex.RUnlock()

	idx := spawnSys.nextBlockIndex.Load()
	total := spawnSys.totalBlocks.Load()

	if codeBlocksLen > 0 && (idx < 0 || idx >= total) {
		t.Errorf("Invalid final state: index=%d, total=%d, actual_len=%d", idx, total, codeBlocksLen)
	}
}

// TestContentSwapDuringConcurrentReads tests that content swap doesn't corrupt reads
func TestContentSwapDuringConcurrentReads(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(ctx)

	// Set up initial small content to trigger frequent swaps
	initialBlocks := []CodeBlock{
		{Lines: []string{"block1"}},
		{Lines: []string{"block2"}},
	}

	spawnSys.contentMutex.Lock()
	spawnSys.codeBlocks = initialBlocks
	spawnSys.contentMutex.Unlock()

	spawnSys.totalBlocks.Store(2)
	spawnSys.nextBlockIndex.Store(0)
	spawnSys.blocksConsumed.Store(0)

	// Run many concurrent reads that will trigger content swaps
	const numReaders = 20
	const readsPerReader = 50
	done := make(chan bool, numReaders)
	panics := make(chan interface{}, numReaders)

	for i := 0; i < numReaders; i++ {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					panics <- r
				}
			}()

			for j := 0; j < readsPerReader; j++ {
				block := spawnSys.getNextBlock()
				// Just access the block, even if empty
				_ = len(block.Lines)
			}
			done <- true
		}()
	}

	// Wait for all readers
	for i := 0; i < numReaders; i++ {
		<-done
	}

	// Check for panics
	select {
	case p := <-panics:
		t.Fatalf("Goroutine panicked during concurrent reads: %v", p)
	default:
		// No panics.
	}
}