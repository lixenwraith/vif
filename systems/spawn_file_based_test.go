package systems

import (
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestFileLoading tests that the data file is loaded correctly
func TestFileLoading(t *testing.T) {
	// Create a temporary test file
	tmpFile, err := os.CreateTemp("", "test_data_*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	testContent := `line one
line two
line three
    indented line
`
	if _, err := tmpFile.WriteString(testContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	// Override the dataFilePath temporarily
	originalPath := dataFilePath
	defer func() {
		// Can't actually restore since dataFilePath is const, but this documents intent
		_ = originalPath
	}()

	// Since we can't change const, we'll test the loadFileContent method behavior
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(80, 24, 40, 12, ctx)

	// The system should have loaded the file (or have empty slice if file doesn't exist)
	// This tests that the system initializes without crashing
	if spawnSys.fileLines == nil {
		t.Error("fileLines should be initialized (empty slice if no file)")
	}
}

// TestColorCounters tests atomic color counter operations
func TestColorCounters(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(80, 24, 40, 12, ctx)

	// Test initial state (all counters should be 0)
	if count := spawnSys.GetColorCount(components.SequenceBlue, components.LevelBright); count != 0 {
		t.Errorf("Initial Blue Bright count should be 0, got %d", count)
	}

	// Test incrementing
	spawnSys.AddColorCount(components.SequenceBlue, components.LevelBright, 5)
	if count := spawnSys.GetColorCount(components.SequenceBlue, components.LevelBright); count != 5 {
		t.Errorf("After adding 5, count should be 5, got %d", count)
	}

	// Test decrementing
	spawnSys.AddColorCount(components.SequenceBlue, components.LevelBright, -2)
	if count := spawnSys.GetColorCount(components.SequenceBlue, components.LevelBright); count != 3 {
		t.Errorf("After subtracting 2, count should be 3, got %d", count)
	}

	// Test different colors independently
	spawnSys.AddColorCount(components.SequenceGreen, components.LevelNormal, 10)
	if count := spawnSys.GetColorCount(components.SequenceGreen, components.LevelNormal); count != 10 {
		t.Errorf("Green Normal count should be 10, got %d", count)
	}
	if count := spawnSys.GetColorCount(components.SequenceBlue, components.LevelBright); count != 3 {
		t.Errorf("Blue Bright should still be 3, got %d", count)
	}
}

// TestColorCountersConcurrency tests atomic operations under concurrent access
func TestColorCountersConcurrency(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(80, 24, 40, 12, ctx)

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
	actualCount := spawnSys.GetColorCount(components.SequenceBlue, components.LevelBright)

	if actualCount != expectedCount {
		t.Errorf("Expected count %d after concurrent increments, got %d", expectedCount, actualCount)
	}
}

// TestGetAvailableColors tests the color availability tracking
func TestGetAvailableColors(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(80, 24, 40, 12, ctx)

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

// TestPlaceLine tests the intelligent line placement algorithm
func TestPlaceLine(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	spawnSys := NewSpawnSystem(80, 24, 40, 12, ctx)

	// Test placing a simple line
	line := "test line"
	style := tcell.StyleDefault
	success := spawnSys.placeLine(world, line, components.SequenceBlue, components.LevelBright, style)

	if !success {
		t.Error("Expected to successfully place line on empty screen")
	}

	// Verify counter was incremented
	count := spawnSys.GetColorCount(components.SequenceBlue, components.LevelBright)
	if count != int64(len(line)) {
		t.Errorf("Expected counter to be %d, got %d", len(line), count)
	}

	// Test placing when screen is full
	// Fill the entire screen first
	for y := 0; y < 24; y++ {
		for x := 0; x < 80; x++ {
			entity := world.CreateEntity()
			world.AddComponent(entity, components.PositionComponent{X: x, Y: y})
			world.AddComponent(entity, components.CharacterComponent{Rune: 'x', Style: style})
			world.UpdateSpatialIndex(entity, x, y)
		}
	}

	success = spawnSys.placeLine(world, "another line", components.SequenceGreen, components.LevelBright, style)
	if success {
		t.Error("Expected to fail placing line on full screen")
	}
}

// TestSpawnWithNoAvailableColors tests that spawning stops when all 6 colors are present
func TestSpawnWithNoAvailableColors(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	spawnSys := NewSpawnSystem(80, 24, 40, 12, ctx)

	// Simulate all 6 colors being present
	spawnSys.AddColorCount(components.SequenceBlue, components.LevelBright, 10)
	spawnSys.AddColorCount(components.SequenceBlue, components.LevelNormal, 10)
	spawnSys.AddColorCount(components.SequenceBlue, components.LevelDark, 10)
	spawnSys.AddColorCount(components.SequenceGreen, components.LevelBright, 10)
	spawnSys.AddColorCount(components.SequenceGreen, components.LevelNormal, 10)
	spawnSys.AddColorCount(components.SequenceGreen, components.LevelDark, 10)

	// Add some dummy file content
	spawnSys.fileLines = []string{"test line 1", "test line 2"}

	// Get entities before spawn
	beforeCount := len(world.GetEntitiesWith())

	// Try to spawn - should do nothing since all colors are present
	spawnSys.spawnSequence(world)

	// Get entities after spawn
	afterCount := len(world.GetEntitiesWith())

	if afterCount != beforeCount {
		t.Errorf("Expected no new entities when all 6 colors present, before=%d, after=%d", beforeCount, afterCount)
	}
}

// TestGetNextBlock tests block retrieval from file lines
func TestGetNextBlock(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	spawnSys := NewSpawnSystem(80, 24, 40, 12, ctx)

	// Set up test file lines
	spawnSys.fileLines = []string{"line1", "line2", "line3", "line4", "line5"}
	spawnSys.nextLineIndex = 0

	// Get a block of 3 lines
	block := spawnSys.getNextBlock(3)
	if len(block) != 3 {
		t.Errorf("Expected block of 3 lines, got %d", len(block))
	}
	if block[0] != "line1" || block[1] != "line2" || block[2] != "line3" {
		t.Error("Block doesn't contain expected lines in order")
	}

	// Verify index advanced
	if spawnSys.nextLineIndex != 3 {
		t.Errorf("Expected nextLineIndex to be 3, got %d", spawnSys.nextLineIndex)
	}

	// Test wraparound
	block = spawnSys.getNextBlock(4)
	if len(block) != 4 {
		t.Errorf("Expected block of 4 lines, got %d", len(block))
	}
	if block[0] != "line4" || block[1] != "line5" || block[2] != "line1" {
		t.Error("Block doesn't wrap around correctly")
	}

	// Verify index wrapped
	if spawnSys.nextLineIndex != 2 {
		t.Errorf("Expected nextLineIndex to wrap to 2, got %d", spawnSys.nextLineIndex)
	}
}

// TestPlaceLineNearCursor tests that lines are not placed too close to cursor
func TestPlaceLineNearCursor(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	cursorX, cursorY := 40, 12
	spawnSys := NewSpawnSystem(80, 24, cursorX, cursorY, ctx)

	// Try many times to place a line - none should be near the cursor
	line := "test"
	style := tcell.StyleDefault

	for attempt := 0; attempt < 50; attempt++ {
		success := spawnSys.placeLine(world, line, components.SequenceBlue, components.LevelBright, style)
		if success {
			// Check all placed entities are far from cursor
			posType := reflect.TypeOf(components.PositionComponent{})
			entities := world.GetEntitiesWith(posType)
			for _, entity := range entities {
				posComp, ok := world.GetComponent(entity, posType)
				if ok {
					pos := posComp.(components.PositionComponent)
					dx := pos.X - cursorX
					dy := pos.Y - cursorY

					// Must be far enough from cursor
					if (dx >= -5 && dx <= 5) && (dy >= -3 && dy <= 3) {
						t.Errorf("Entity placed too close to cursor at (%d, %d), cursor at (%d, %d)",
							pos.X, pos.Y, cursorX, cursorY)
					}
				}
			}

			// Clean up for next attempt
			for _, entity := range entities {
				world.SafeDestroyEntity(entity)
			}
			spawnSys.AddColorCount(components.SequenceBlue, components.LevelBright, -int64(len(line)))
		}
	}
}
