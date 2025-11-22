package systems

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestGroupIntoBlocks tests logical block grouping
func TestGroupIntoBlocks(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(ctx)

	tests := []struct {
		name        string
		input       []string
		minBlocks   int
		maxBlocks   int
		checkBraces bool
	}{
		{
			name: "function with braces",
			input: []string{
				"func example() {",
				"x := 1",
				"y := 2",
				"return x + y",
				"}",
			},
			minBlocks:   1,
			maxBlocks:   1,
			checkBraces: true,
		},
		{
			name: "const block",
			input: []string{
				"const (",
				"MaxSize = 100",
				"MinSize = 10",
				")",
			},
			minBlocks:   1,
			maxBlocks:   1,
			checkBraces: false,
		},
		{
			name: "multiple functions",
			input: []string{
				"func first() {",
				"a := 1",
				"b := 2",
				"}",
				"func second() {",
				"c := 3",
				"d := 4",
				"}",
			},
			minBlocks:   1,
			maxBlocks:   2,
			checkBraces: true,
		},
		{
			name: "indentation change",
			input: []string{
				"type Foo struct {",
				"Field1 int",
				"Field2 string",
				"}",
				"var x = 1",
				"var y = 2",
				"var z = 3",
			},
			minBlocks:   1,
			maxBlocks:   2,
			checkBraces: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := spawnSys.groupIntoBlocks(tt.input)

			if len(blocks) < tt.minBlocks || len(blocks) > tt.maxBlocks {
				t.Errorf("Expected %d-%d blocks, got %d", tt.minBlocks, tt.maxBlocks, len(blocks))
			}

			// Verify all blocks meet minimum size
			for i, block := range blocks {
				if len(block.Lines) < minBlockLines {
					t.Errorf("Block %d has %d lines, expected at least %d", i, len(block.Lines), minBlockLines)
				}
				if len(block.Lines) > maxBlockLines {
					t.Errorf("Block %d has %d lines, expected at most %d", i, len(block.Lines), maxBlockLines)
				}
			}

			// Check braces if required
			if tt.checkBraces && len(blocks) > 0 {
				hasBraces := false
				for _, block := range blocks {
					if block.HasBraces {
						hasBraces = true
						break
					}
				}
				if !hasBraces {
					t.Error("Expected at least one block with braces")
				}
			}
		})
	}
}

// TestGetIndentLevel tests indent calculation
func TestGetIndentLevel(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(ctx)

	tests := []struct {
		line     string
		expected int
	}{
		{"no indent", 0},
		{"  two spaces", 2},
		{"    four spaces", 4},
		{"\tone tab", 4},
		{"\t\ttwo tabs", 8},
		{"  \tmixed", 6},
	}

	for _, tt := range tests {
		result := spawnSys.getIndentLevel(tt.line)
		if result != tt.expected {
			t.Errorf("getIndentLevel(%q) = %d, expected %d", tt.line, result, tt.expected)
		}
	}
}

// TestBlockSpawning tests that blocks are spawned as units
func TestBlockSpawning(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(ctx)

	// Create test code blocks with proper state
	testBlocks := []CodeBlock{
		{
			Lines:       []string{"line1", "line2", "line3"},
			IndentLevel: 0,
			HasBraces:   false,
		},
		{
			Lines:       []string{"func foo() {", "return 1", "}"},
			IndentLevel: 0,
			HasBraces:   true,
		},
	}
	spawnSys.contentMutex.Lock()
	spawnSys.codeBlocks = testBlocks
	spawnSys.contentMutex.Unlock()

	spawnSys.totalBlocks.Store(int32(len(testBlocks)))
	spawnSys.nextBlockIndex.Store(0)
	spawnSys.blocksConsumed.Store(0)

	// Get first block
	block1 := spawnSys.getNextBlock()
	if len(block1.Lines) != 3 {
		t.Errorf("Expected first block to have 3 lines, got %d", len(block1.Lines))
	}
	idx1 := spawnSys.nextBlockIndex.Load()
	if idx1 != 1 {
		t.Errorf("Expected nextBlockIndex to be 1, got %d", idx1)
	}

	// Get second block
	block2 := spawnSys.getNextBlock()
	if len(block2.Lines) != 3 {
		t.Errorf("Expected second block to have 3 lines, got %d", len(block2.Lines))
	}
	if !block2.HasBraces {
		t.Error("Expected second block to have braces")
	}

	// After getting second block, it wraps around and swaps to new content
	// The new content comes from ContentManager (default content since no files exist)
	// So we just verify that we can continue getting blocks without errors
	block3 := spawnSys.getNextBlock()
	if len(block3.Lines) == 0 {
		t.Error("Should get valid content block after wraparound")
	}
}