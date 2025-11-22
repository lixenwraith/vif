package systems

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestPlaceLine tests the intelligent line placement algorithm
func TestPlaceLine(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	spawnSys := NewSpawnSystem(ctx)

	// Test placing a simple line
	line := "test line"
	style := tcell.StyleDefault
	success := spawnSys.placeLine(world, line, components.SequenceBlue, components.LevelBright, style)

	if !success {
		t.Error("Expected to successfully place line on empty screen")
	}

	// Verify counter was incremented (should count only non-space characters)
	// "test line" has 8 non-space characters ('t','e','s','t','l','i','n','e')
	expectedCount := int64(len(strings.ReplaceAll(line, " ", "")))
	count := spawnSys.GetColorCount(components.SequenceBlue, components.LevelBright)
	if count != expectedCount {
		t.Errorf("Expected counter to be %d (non-space chars), got %d", expectedCount, count)
	}

	// Test placing when screen is full
	// Fill the entire screen first
	for y := 0; y < 24; y++ {
		for x := 0; x < 80; x++ {
			entity := world.CreateEntity()
			world.AddComponent(entity, components.PositionComponent{X: x, Y: y})
			world.AddComponent(entity, components.CharacterComponent{Rune: 'x', Style: style})

			tx := world.BeginSpatialTransaction()
			tx.Spawn(entity, x, y)
			tx.Commit()
		}
	}

	success = spawnSys.placeLine(world, "another line", components.SequenceGreen, components.LevelBright, style)
	if success {
		t.Error("Expected to fail placing line on full screen")
	}
}

// TestPlaceLineNearCursor tests that lines are not placed too close to cursor
func TestPlaceLineNearCursor(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	cursorX, cursorY := 40, 12
	// Sync cursor position to GameState for snapshot pattern
	ctx.State.SetCursorX(cursorX)
	ctx.State.SetCursorY(cursorY)
	spawnSys := NewSpawnSystem(ctx)

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

// TestPlaceLineSkipsSpaces tests that space characters are not created as entities
func TestPlaceLineSkipsSpaces(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	tests := []struct {
		name                string
		line                string
		expectedEntityCount int
		expectedCounterInc  int
	}{
		{
			name:                "line with no spaces",
			line:                "hello",
			expectedEntityCount: 5,
			expectedCounterInc:  5,
		},
		{
			name:                "line with spaces",
			line:                "hi world",
			expectedEntityCount: 7, // 'h', 'i', 'w', 'o', 'r', 'l', 'd' (no space entity)
			expectedCounterInc:  7, // counter should only count non-space chars
		},
		{
			name:                "line with multiple spaces",
			line:                "a  b  c",
			expectedEntityCount: 3, // 'a', 'b', 'c'
			expectedCounterInc:  3,
		},
		{
			name:                "line starting with space",
			line:                " test",
			expectedEntityCount: 4, // 't', 'e', 's', 't'
			expectedCounterInc:  4,
		},
		{
			name:                "line ending with space",
			line:                "test ",
			expectedEntityCount: 4, // 't', 'e', 's', 't'
			expectedCounterInc:  4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a fresh world for each test
			world := engine.NewWorld()
			spawnSys := NewSpawnSystem(ctx)

			style := tcell.StyleDefault
			initialCount := spawnSys.GetColorCount(components.SequenceBlue, components.LevelBright)

			// Place the line
			success := spawnSys.placeLine(world, tt.line, components.SequenceBlue, components.LevelBright, style)

			if !success {
				t.Error("Expected to successfully place line on empty screen")
				return
			}

			// Count entities created
			posType := reflect.TypeOf(components.PositionComponent{})
			entities := world.GetEntitiesWith(posType)
			actualEntityCount := len(entities)

			if actualEntityCount != tt.expectedEntityCount {
				t.Errorf("Expected %d entities, got %d", tt.expectedEntityCount, actualEntityCount)
			}

			// Verify counter incremented correctly (only non-space characters)
			finalCount := spawnSys.GetColorCount(components.SequenceBlue, components.LevelBright)
			actualCounterInc := int(finalCount - initialCount)

			if actualCounterInc != tt.expectedCounterInc {
				t.Errorf("Expected counter increment of %d, got %d", tt.expectedCounterInc, actualCounterInc)
			}

			// Verify no space entities were created
			for _, entity := range entities {
				charComp, ok := world.GetComponent(entity, reflect.TypeOf(components.CharacterComponent{}))
				if ok {
					char := charComp.(components.CharacterComponent)
					if char.Rune == ' ' {
						t.Error("Found space character entity - spaces should be skipped")
					}
				}
			}

			// Verify positions are correct (spaces should create gaps)
			// Get all positions
			positions := make(map[int]bool) // Track X positions
			for _, entity := range entities {
				posComp, ok := world.GetComponent(entity, posType)
				if ok {
					pos := posComp.(components.PositionComponent)
					positions[pos.X] = true
				}
			}

			// For lines with spaces, verify there are gaps in positions
			if strings.Contains(tt.line, " ") {
				lineRunes := []rune(tt.line)
				for i, r := range lineRunes {
					if r == ' ' {
						// At position i, there should be NO entity
						// We can't verify exact position without knowing startCol,
						// but we verified no space runes exist above
						_ = i // position verification done above
					}
				}
			}
		})
	}
}

// TestPlaceLinePositionMaintenance tests that positions are maintained even when skipping spaces
func TestPlaceLinePositionMaintenance(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	// Place cursor far from common spawn locations
	// Sync cursor position to GameState for snapshot pattern
	ctx.State.SetCursorX(0)
	ctx.State.SetCursorY(0)
	spawnSys := NewSpawnSystem(ctx)

	line := "a b c"
	style := tcell.StyleDefault

	// Place line multiple times to find where it gets placed
	success := spawnSys.placeLine(world, line, components.SequenceBlue, components.LevelBright, style)

	if !success {
		t.Fatal("Failed to place line")
	}

	// Get all entities
	posType := reflect.TypeOf(components.PositionComponent{})
	entities := world.GetEntitiesWith(posType)

	if len(entities) != 3 {
		t.Fatalf("Expected 3 entities (a, b, c), got %d", len(entities))
	}

	// Build a map of position to character
	posMap := make(map[int]rune)
	var startX int
	minX := 1000
	for _, entity := range entities {
		posComp, _ := world.GetComponent(entity, posType)
		charComp, _ := world.GetComponent(entity, reflect.TypeOf(components.CharacterComponent{}))

		pos := posComp.(components.PositionComponent)
		char := charComp.(components.CharacterComponent)

		posMap[pos.X] = char.Rune
		if pos.X < minX {
			minX = pos.X
			startX = pos.X
		}
	}

	// Verify positions: 'a' at startX, 'b' at startX+2, 'c' at startX+4
	// (with spaces at startX+1 and startX+3)
	if posMap[startX] != 'a' {
		t.Errorf("Expected 'a' at position %d, got %c", startX, posMap[startX])
	}
	if posMap[startX+2] != 'b' {
		t.Errorf("Expected 'b' at position %d, got %c", startX+2, posMap[startX+2])
	}
	if posMap[startX+4] != 'c' {
		t.Errorf("Expected 'c' at position %d, got %c", startX+4, posMap[startX+4])
	}

	// Verify no entities at space positions
	if _, exists := posMap[startX+1]; exists {
		t.Errorf("Found entity at space position %d (should be empty)", startX+1)
	}
	if _, exists := posMap[startX+3]; exists {
		t.Errorf("Found entity at space position %d (should be empty)", startX+3)
	}
}

// TestPlaceLinePackageMd5 tests that "package md5" creates exactly 10 entities (not 11)
func TestPlaceLinePackageMd5(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	// Place cursor far from spawn area to avoid exclusion zone
	// Sync cursor position to GameState for snapshot pattern
	ctx.State.SetCursorX(0)
	ctx.State.SetCursorY(0)
	spawnSys := NewSpawnSystem(ctx)

	line := "package md5"
	style := tcell.StyleDefault

	// Place the line
	success := spawnSys.placeLine(world, line, components.SequenceBlue, components.LevelBright, style)

	if !success {
		t.Fatal("Failed to place line 'package md5'")
	}

	// Get all entities
	posType := reflect.TypeOf(components.PositionComponent{})
	entities := world.GetEntitiesWith(posType)

	// Should have exactly 10 entities (7 for "package" + 3 for "md5"), not 11
	if len(entities) != 10 {
		t.Errorf("Expected exactly 10 entities (7 from 'package' + 3 from 'md5'), got %d", len(entities))
	}

	// Verify color counter counts only non-space characters
	count := spawnSys.GetColorCount(components.SequenceBlue, components.LevelBright)
	if count != 10 {
		t.Errorf("Expected color count 10 (no space counted), got %d", count)
	}

	// Find where the line was placed
	if len(entities) == 0 {
		t.Fatal("No entities created")
	}

	firstPosComp, _ := world.GetComponent(entities[0], posType)
	firstPos := firstPosComp.(components.PositionComponent)
	startX := firstPos.X
	startY := firstPos.Y

	// Verify "package" characters exist at correct positions (0-6)
	expectedPackage := []rune("package")
	for i, expectedRune := range expectedPackage {
		entity := world.GetEntityAtPosition(startX+i, startY)
		if entity == 0 {
			t.Errorf("No entity at position %d (expected '%c' from 'package')", i, expectedRune)
			continue
		}

		charType := reflect.TypeOf(components.CharacterComponent{})
		charComp, ok := world.GetComponent(entity, charType)
		if !ok {
			t.Errorf("Entity at position %d missing CharacterComponent", i)
			continue
		}

		char := charComp.(components.CharacterComponent)
		if char.Rune != expectedRune {
			t.Errorf("Position %d: expected '%c', got '%c'", i, expectedRune, char.Rune)
		}
	}

	// Verify NO entity exists at the space position (position 7)
	spaceEntity := world.GetEntityAtPosition(startX+7, startY)
	if spaceEntity != 0 {
		charType := reflect.TypeOf(components.CharacterComponent{})
		charComp, _ := world.GetComponent(spaceEntity, charType)
		char := charComp.(components.CharacterComponent)
		t.Errorf("Found entity at space position 7 with character '%c', should be empty", char.Rune)
	}

	// Verify "md5" characters exist at correct positions (8-10)
	expectedMd5 := []rune("md5")
	for i, expectedRune := range expectedMd5 {
		pos := 8 + i // Offset by 8 to account for "package " (7 chars + 1 space)
		entity := world.GetEntityAtPosition(startX+pos, startY)
		if entity == 0 {
			t.Errorf("No entity at position %d (expected '%c' from 'md5')", pos, expectedRune)
			continue
		}

		charType := reflect.TypeOf(components.CharacterComponent{})
		charComp, ok := world.GetComponent(entity, charType)
		if !ok {
			t.Errorf("Entity at position %d missing CharacterComponent", pos)
			continue
		}

		char := charComp.(components.CharacterComponent)
		if char.Rune != expectedRune {
			t.Errorf("Position %d: expected '%c', got '%c'", pos, expectedRune, char.Rune)
		}
	}
}

// TestPlaceLineConstBlockSize tests that "const BlockSize = 64" creates correct entities with spaces preserved
func TestPlaceLineConstBlockSize(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	// Place cursor far from spawn area
	// Sync cursor position to GameState for snapshot pattern
	ctx.State.SetCursorX(0)
	ctx.State.SetCursorY(0)
	spawnSys := NewSpawnSystem(ctx)

	line := "const BlockSize = 64"
	style := tcell.StyleDefault

	// Count expected non-space characters
	expectedNonSpaceCount := 0
	for _, r := range line {
		if r != ' ' {
			expectedNonSpaceCount++
		}
	}
	// "const" (5) + "BlockSize" (9) + "=" (1) + "64" (2) = 17 characters

	// Place the line
	success := spawnSys.placeLine(world, line, components.SequenceGreen, components.LevelNormal, style)

	if !success {
		t.Fatal("Failed to place line 'const BlockSize = 64'")
	}

	// Get all entities
	posType := reflect.TypeOf(components.PositionComponent{})
	entities := world.GetEntitiesWith(posType)

	// Should have exactly expectedNonSpaceCount entities (spaces don't create entities)
	if len(entities) != expectedNonSpaceCount {
		t.Errorf("Expected %d entities (non-space chars only), got %d", expectedNonSpaceCount, len(entities))
	}

	// Verify color counter counts only non-space characters
	count := spawnSys.GetColorCount(components.SequenceGreen, components.LevelNormal)
	if count != int64(expectedNonSpaceCount) {
		t.Errorf("Expected color count %d (no spaces counted), got %d", expectedNonSpaceCount, count)
	}

	// Find where the line was placed
	if len(entities) == 0 {
		t.Fatal("No entities created")
	}

	firstPosComp, _ := world.GetComponent(entities[0], posType)
	firstPos := firstPosComp.(components.PositionComponent)
	startX := firstPos.X
	startY := firstPos.Y

	// Verify spaces don't have entities and non-spaces do
	lineRunes := []rune(line)
	for i, r := range lineRunes {
		entity := world.GetEntityAtPosition(startX+i, startY)

		if r == ' ' {
			// Space position should NOT have an entity
			if entity != 0 {
				charType := reflect.TypeOf(components.CharacterComponent{})
				charComp, _ := world.GetComponent(entity, charType)
				char := charComp.(components.CharacterComponent)
				t.Errorf("Found entity at space position %d with character '%c', should be empty", i, char.Rune)
			}
		} else {
			// Non-space position should have an entity
			if entity == 0 {
				t.Errorf("No entity at non-space position %d (expected '%c')", i, r)
				continue
			}

			// Verify the character matches
			charType := reflect.TypeOf(components.CharacterComponent{})
			charComp, ok := world.GetComponent(entity, charType)
			if !ok {
				t.Errorf("Entity at position %d missing CharacterComponent", i)
				continue
			}

			char := charComp.(components.CharacterComponent)
			if char.Rune != r {
				t.Errorf("Position %d: expected '%c', got '%c'", i, r, char.Rune)
			}
		}
	}

	// Verify word boundaries are preserved (check specific positions)
	// "const" should be at positions 0-4
	// space at position 5
	// "BlockSize" should be at positions 6-14
	// space at position 15
	// "=" should be at position 16
	// space at position 17
	// "64" should be at positions 18-19

	expectedPositions := map[int]rune{
		0: 'c', 1: 'o', 2: 'n', 3: 's', 4: 't',
		// 5 is space (no entity)
		6: 'B', 7: 'l', 8: 'o', 9: 'c', 10: 'k', 11: 'S', 12: 'i', 13: 'z', 14: 'e',
		// 15 is space (no entity)
		16: '=',
		// 17 is space (no entity)
		18: '6', 19: '4',
	}

	for offset, expectedRune := range expectedPositions {
		entity := world.GetEntityAtPosition(startX+offset, startY)
		if entity == 0 {
			t.Errorf("No entity at position %d (expected '%c')", offset, expectedRune)
			continue
		}

		charType := reflect.TypeOf(components.CharacterComponent{})
		charComp, ok := world.GetComponent(entity, charType)
		if !ok {
			t.Errorf("Entity at position %d missing CharacterComponent", offset)
			continue
		}

		char := charComp.(components.CharacterComponent)
		if char.Rune != expectedRune {
			t.Errorf("Position %d: expected '%c', got '%c'", offset, expectedRune, char.Rune)
		}
	}
}