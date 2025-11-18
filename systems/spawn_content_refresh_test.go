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

	spawnSys := NewSpawnSystem(80, 24, 40, 12, ctx)

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
	spawnSys.totalBlocks = 5
	spawnSys.nextBlockIndex = 0
	spawnSys.blocksConsumed = 0
	spawnSys.contentMutex.Unlock()

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

	spawnSys := NewSpawnSystem(80, 24, 40, 12, ctx)

	// Set up initial test blocks
	initialBlocks := []CodeBlock{
		{Lines: []string{"initial1", "line2", "line3"}},
		{Lines: []string{"initial2", "line2", "line3"}},
	}

	spawnSys.contentMutex.Lock()
	spawnSys.codeBlocks = initialBlocks
	spawnSys.totalBlocks = 2
	spawnSys.nextBlockIndex = 0
	spawnSys.blocksConsumed = 0
	spawnSys.contentMutex.Unlock()

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

	spawnSys := NewSpawnSystem(80, 24, 40, 12, ctx)

	// Set up test blocks
	testBlocks := []CodeBlock{
		{Lines: []string{"test1", "line2", "line3"}},
		{Lines: []string{"test2", "line2", "line3"}},
	}

	spawnSys.contentMutex.Lock()
	spawnSys.codeBlocks = testBlocks
	spawnSys.totalBlocks = 2
	spawnSys.nextBlockIndex = 0
	spawnSys.blocksConsumed = 0
	spawnSys.contentMutex.Unlock()

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

	spawnSys := NewSpawnSystem(80, 24, 40, 12, ctx)

	// Manually set empty content
	spawnSys.contentMutex.Lock()
	spawnSys.codeBlocks = []CodeBlock{}
	spawnSys.totalBlocks = 0
	spawnSys.nextBlockIndex = 0
	spawnSys.blocksConsumed = 0
	spawnSys.contentMutex.Unlock()

	// Should return empty block gracefully
	block := spawnSys.getNextBlock()
	if len(block.Lines) != 0 {
		t.Errorf("Expected empty block, got %d lines", len(block.Lines))
	}

	// System should still be in valid state
	spawnSys.contentMutex.RLock()
	idx := spawnSys.nextBlockIndex
	spawnSys.contentMutex.RUnlock()

	if idx != 0 {
		t.Errorf("nextBlockIndex should remain 0 with empty content, got %d", idx)
	}
}

// TestContentRefreshDoesNotBlockGameplay tests that content refresh happens in background
func TestContentRefreshDoesNotBlockGameplay(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(80, 24, 40, 12, ctx)

	// Set up test blocks
	testBlocks := make([]CodeBlock, 10)
	for i := 0; i < 10; i++ {
		testBlocks[i] = CodeBlock{Lines: []string{"test", "line2", "line3"}}
	}

	spawnSys.contentMutex.Lock()
	spawnSys.codeBlocks = testBlocks
	spawnSys.totalBlocks = 10
	spawnSys.nextBlockIndex = 0
	spawnSys.blocksConsumed = 0
	spawnSys.contentMutex.Unlock()

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
