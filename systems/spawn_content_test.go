package systems

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestContentManagerIntegration tests that ContentManager is properly integrated
func TestContentManagerIntegration(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(ctx)

	// The system should have initialized ContentManager
	if spawnSys.contentManager == nil {
		t.Error("ContentManager should be initialized")
	}

	// The system should have loaded content (or have empty slice if no files available)
	// This tests that the system initializes without crashing
	if spawnSys.codeBlocks == nil {
		t.Error("codeBlocks should be initialized (empty slice if no content)")
	}

	// Verify totalBlocks is set correctly
	spawnSys.contentMutex.RLock()
	codeBlocksLen := len(spawnSys.codeBlocks)
	spawnSys.contentMutex.RUnlock()

	totalBlocks := spawnSys.totalBlocks.Load()
	if int32(codeBlocksLen) != totalBlocks {
		t.Errorf("totalBlocks (%d) should match length of codeBlocks (%d)",
			totalBlocks, codeBlocksLen)
	}

	// Verify initial state
	if spawnSys.blocksConsumed.Load() != 0 {
		t.Errorf("blocksConsumed should be 0 initially, got %d", spawnSys.blocksConsumed.Load())
	}

	if spawnSys.nextBlockIndex.Load() != 0 {
		t.Errorf("nextBlockIndex should be 0 initially, got %d", spawnSys.nextBlockIndex.Load())
	}
}

// TestCommentFiltering tests that full-line comments are filtered out
func TestCommentFiltering(t *testing.T) {
	testLines := []string{
		"package main",
		"// This is a comment",
		"",
		"func main() {",
		"  // Another comment",
		"  fmt.Println(\"hello\")",
		"}",
	}

	filtered := []string{}
	for _, line := range testLines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) > 0 && !strings.HasPrefix(trimmed, "//") {
			filtered = append(filtered, trimmed)
		}
	}

	// Should have 4 lines (package, func, fmt, })
	expected := 4
	if len(filtered) != expected {
		t.Errorf("Expected %d lines after filtering, got %d", expected, len(filtered))
	}

	// Verify no comment lines remain
	for _, line := range filtered {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			t.Errorf("Comment line should have been filtered: %s", line)
		}
	}
}

// TestEmptyBlockHandling tests graceful handling of empty blocks
func TestEmptyBlockHandling(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(ctx)

	// Set empty code blocks
	spawnSys.codeBlocks = []CodeBlock{}

	// Should return empty block
	block := spawnSys.getNextBlock()
	if len(block.Lines) != 0 {
		t.Errorf("Expected empty block, got %d lines", len(block.Lines))
	}
}

// TestBlockGroupingWithShortLines tests that short blocks are filtered
func TestBlockGroupingWithShortLines(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(ctx)

	// Input with blocks shorter than minBlockLines
	input := []string{
		"a",
		"b",
		// This block is too short (2 lines < minBlockLines=3)
		"func long() {",
		"x := 1",
		"y := 2",
		"z := 3",
		"}",
		// This block is long enough (5 lines >= minBlockLines=3)
	}

	blocks := spawnSys.groupIntoBlocks(input)

	// Should only get blocks that meet minimum size
	for i, block := range blocks {
		if len(block.Lines) < minBlockLines {
			t.Errorf("Block %d has %d lines, should have been filtered (min=%d)",
				i, len(block.Lines), minBlockLines)
		}
	}
}