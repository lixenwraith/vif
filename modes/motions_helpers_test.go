package modes

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// Helper function to create a simple test context for helper function tests
func createSimpleTestContext() *engine.GameContext {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	screen.SetSize(80, 24)

	ctx := engine.NewGameContext(screen)
	ctx.GameWidth = 80
	ctx.GameHeight = 20
	setCursorPosition(ctx, 0, 0)
	return ctx
}

// Helper function to place a character without full sequence setup
func placeSimpleChar(ctx *engine.GameContext, x, y int, r rune) {
	entity := ctx.World.CreateEntity()
	ctx.World.Positions.Add(entity, components.PositionComponent{X: x, Y: y})
	ctx.World.Characters.Add(entity, components.CharacterComponent{Rune: r, Style: tcell.StyleDefault})
}

// TestGetCharacterTypeAt tests character type classification
func TestGetCharacterTypeAt(t *testing.T) {
	ctx := createSimpleTestContext()

	// Test CharTypeSpace (empty position)
	charType := getCharacterTypeAt(ctx, 0, 0)
	if charType != CharTypeSpace {
		t.Errorf("Expected CharTypeSpace (0) for empty position, got %d", charType)
	}

	// Test CharTypeWord (alphanumeric)
	placeSimpleChar(ctx, 0, 0, 'a')
	charType = getCharacterTypeAt(ctx, 0, 0)
	if charType != CharTypeWord {
		t.Errorf("Expected CharTypeWord (1) for 'a', got %d", charType)
	}

	placeSimpleChar(ctx, 1, 0, 'Z')
	charType = getCharacterTypeAt(ctx, 1, 0)
	if charType != CharTypeWord {
		t.Errorf("Expected CharTypeWord (1) for 'Z', got %d", charType)
	}

	placeSimpleChar(ctx, 2, 0, '5')
	charType = getCharacterTypeAt(ctx, 2, 0)
	if charType != CharTypeWord {
		t.Errorf("Expected CharTypeWord (1) for '5', got %d", charType)
	}

	placeSimpleChar(ctx, 3, 0, '_')
	charType = getCharacterTypeAt(ctx, 3, 0)
	if charType != CharTypeWord {
		t.Errorf("Expected CharTypeWord (1) for '_', got %d", charType)
	}

	// Test CharTypePunctuation
	placeSimpleChar(ctx, 4, 0, '.')
	charType = getCharacterTypeAt(ctx, 4, 0)
	if charType != CharTypePunctuation {
		t.Errorf("Expected CharTypePunctuation (2) for '.', got %d", charType)
	}

	placeSimpleChar(ctx, 5, 0, ',')
	charType = getCharacterTypeAt(ctx, 5, 0)
	if charType != CharTypePunctuation {
		t.Errorf("Expected CharTypePunctuation (2) for ',', got %d", charType)
	}

	placeSimpleChar(ctx, 6, 0, '!')
	charType = getCharacterTypeAt(ctx, 6, 0)
	if charType != CharTypePunctuation {
		t.Errorf("Expected CharTypePunctuation (2) for '!', got %d", charType)
	}

	placeSimpleChar(ctx, 7, 0, '{')
	charType = getCharacterTypeAt(ctx, 7, 0)
	if charType != CharTypePunctuation {
		t.Errorf("Expected CharTypePunctuation (2) for '{', got %d", charType)
	}

	placeSimpleChar(ctx, 8, 0, '@')
	charType = getCharacterTypeAt(ctx, 8, 0)
	if charType != CharTypePunctuation {
		t.Errorf("Expected CharTypePunctuation (2) for '@', got %d", charType)
	}
}

// TestGetCharacterTypeAt_EdgeCases tests edge cases for character type classification
func TestGetCharacterTypeAt_EdgeCases(t *testing.T) {
	ctx := createSimpleTestContext()

	// Test at boundaries
	charType := getCharacterTypeAt(ctx, 0, 0)
	if charType != CharTypeSpace {
		t.Errorf("Expected CharTypeSpace at (0,0), got %d", charType)
	}

	charType = getCharacterTypeAt(ctx, ctx.GameWidth-1, ctx.GameHeight-1)
	if charType != CharTypeSpace {
		t.Errorf("Expected CharTypeSpace at max position, got %d", charType)
	}

	// Test with character at boundary
	placeSimpleChar(ctx, ctx.GameWidth-1, ctx.GameHeight-1, 'x')
	charType = getCharacterTypeAt(ctx, ctx.GameWidth-1, ctx.GameHeight-1)
	if charType != CharTypeWord {
		t.Errorf("Expected CharTypeWord at max position with character, got %d", charType)
	}
}

// TestValidatePosition tests position validation
func TestValidatePosition(t *testing.T) {
	ctx := createSimpleTestContext()

	tests := []struct {
		name        string
		inputX      int
		inputY      int
		expectedX   int
		expectedY   int
		description string
	}{
		{
			name:        "valid position",
			inputX:      10,
			inputY:      5,
			expectedX:   10,
			expectedY:   5,
			description: "position within bounds should remain unchanged",
		},
		{
			name:        "X too small",
			inputX:      -5,
			inputY:      5,
			expectedX:   0,
			expectedY:   5,
			description: "negative X should be clamped to 0",
		},
		{
			name:        "X too large",
			inputX:      100,
			inputY:      5,
			expectedX:   79,
			expectedY:   5,
			description: "X beyond width should be clamped to GameWidth-1",
		},
		{
			name:        "Y too small",
			inputX:      10,
			inputY:      -3,
			expectedX:   10,
			expectedY:   0,
			description: "negative Y should be clamped to 0",
		},
		{
			name:        "Y too large",
			inputX:      10,
			inputY:      50,
			expectedX:   10,
			expectedY:   19,
			description: "Y beyond height should be clamped to GameHeight-1",
		},
		{
			name:        "both too small",
			inputX:      -10,
			inputY:      -10,
			expectedX:   0,
			expectedY:   0,
			description: "both negative should be clamped to (0,0)",
		},
		{
			name:        "both too large",
			inputX:      200,
			inputY:      100,
			expectedX:   79,
			expectedY:   19,
			description: "both beyond bounds should be clamped to max",
		},
		{
			name:        "at minimum boundary",
			inputX:      0,
			inputY:      0,
			expectedX:   0,
			expectedY:   0,
			description: "position at (0,0) should remain unchanged",
		},
		{
			name:        "at maximum boundary",
			inputX:      79,
			inputY:      19,
			expectedX:   79,
			expectedY:   19,
			description: "position at max should remain unchanged",
		},
		{
			name:        "one off maximum",
			inputX:      80,
			inputY:      20,
			expectedX:   79,
			expectedY:   19,
			description: "position one past max should be clamped",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validX, validY := validatePosition(ctx, tt.inputX, tt.inputY)

			if validX != tt.expectedX || validY != tt.expectedY {
				t.Errorf("%s: validatePosition(%d, %d) = (%d, %d), expected (%d, %d)",
					tt.description, tt.inputX, tt.inputY, validX, validY, tt.expectedX, tt.expectedY)
			}
		})
	}
}

// TestValidatePosition_WithMotion tests that validatePosition works correctly with actual motion functions
func TestValidatePosition_WithMotion(t *testing.T) {
	ctx := createSimpleTestContext()

	// Setup a line of text at y=0
	text := "hello world"
	for i, r := range text {
		if r != ' ' {
			placeSimpleChar(ctx, i, 0, r)
		}
	}

	// Test that cursor position is validated after motion
	setCursorPosition(ctx, 78, 0)

	// Move right - should be clamped to GameWidth-1
	ExecuteMotion(ctx, 'l', 5)
	if getCursorX(ctx) != 79 {
		t.Errorf("Expected X to be clamped to 79, got %d", getCursorX(ctx))
	}

	// Move down from near bottom
	setCursorPosition(ctx, getCursorX(ctx), 18)
	ExecuteMotion(ctx, 'j', 5)
	if getCursorY(ctx) != 19 {
		t.Errorf("Expected Y to be clamped to 19, got %d", getCursorY(ctx))
	}

	// Move left from left edge
	setCursorPosition(ctx, 2, getCursorY(ctx))
	ExecuteMotion(ctx, 'h', 5)
	if getCursorX(ctx) != 0 {
		t.Errorf("Expected X to be clamped to 0, got %d", getCursorX(ctx))
	}

	// Move up from near top
	setCursorPosition(ctx, getCursorX(ctx), 2)
	ExecuteMotion(ctx, 'k', 5)
	if getCursorY(ctx) != 0 {
		t.Errorf("Expected Y to be clamped to 0, got %d", getCursorY(ctx))
	}
}

// TestCharTypeEnum tests that the enum values are as specified
func TestCharTypeEnum(t *testing.T) {
	if CharTypeSpace != 0 {
		t.Errorf("CharTypeSpace should be 0, got %d", CharTypeSpace)
	}

	if CharTypeWord != 1 {
		t.Errorf("CharTypeWord should be 1, got %d", CharTypeWord)
	}

	if CharTypePunctuation != 2 {
		t.Errorf("CharTypePunctuation should be 2, got %d", CharTypePunctuation)
	}
}

// TestGetCharacterTypeAt_AllWordChars tests all word characters
func TestGetCharacterTypeAt_AllWordChars(t *testing.T) {
	ctx := createSimpleTestContext()

	// Test all lowercase letters
	for r := 'a'; r <= 'z'; r++ {
		placeSimpleChar(ctx, 0, 0, r)
		charType := getCharacterTypeAt(ctx, 0, 0)
		if charType != CharTypeWord {
			t.Errorf("Expected CharTypeWord for '%c', got %d", r, charType)
		}
		// Clear for next test
		ctx.World.DestroyEntity(ctx.World.GetEntityAtPosition(0, 0))
	}

	// Test all uppercase letters
	for r := 'A'; r <= 'Z'; r++ {
		placeSimpleChar(ctx, 0, 0, r)
		charType := getCharacterTypeAt(ctx, 0, 0)
		if charType != CharTypeWord {
			t.Errorf("Expected CharTypeWord for '%c', got %d", r, charType)
		}
		ctx.World.DestroyEntity(ctx.World.GetEntityAtPosition(0, 0))
	}

	// Test all digits
	for r := '0'; r <= '9'; r++ {
		placeSimpleChar(ctx, 0, 0, r)
		charType := getCharacterTypeAt(ctx, 0, 0)
		if charType != CharTypeWord {
			t.Errorf("Expected CharTypeWord for '%c', got %d", r, charType)
		}
		ctx.World.DestroyEntity(ctx.World.GetEntityAtPosition(0, 0))
	}
}

// TestGetCharacterTypeAt_CommonPunctuation tests common punctuation characters
func TestGetCharacterTypeAt_CommonPunctuation(t *testing.T) {
	ctx := createSimpleTestContext()

	punctuation := []rune{'.', ',', ';', ':', '!', '?', '-', '+', '=', '*', '/', '\\', '|', '<', '>', '(', ')', '[', ']', '{', '}', '\'', '"', '`', '~', '@', '#', '$', '%', '^', '&'}

	for _, r := range punctuation {
		placeSimpleChar(ctx, 0, 0, r)
		charType := getCharacterTypeAt(ctx, 0, 0)
		if charType != CharTypePunctuation {
			t.Errorf("Expected CharTypePunctuation for '%c', got %d", r, charType)
		}
		ctx.World.DestroyEntity(ctx.World.GetEntityAtPosition(0, 0))
	}
}

// TestGetCharacterTypeAt_SpaceHandling tests that both empty positions and space entities are detected as spaces
func TestGetCharacterTypeAt_SpaceHandling(t *testing.T) {
	ctx := createSimpleTestContext()

	// Test empty position (no entity) - should return CharTypeSpace
	charType := getCharacterTypeAt(ctx, 0, 0)
	if charType != CharTypeSpace {
		t.Errorf("Expected CharTypeSpace for empty position (no entity), got %d", charType)
	}

	// Test space character entity - should return CharTypeSpace
	// Note: getCharAt currently returns 0 for space entities, but getCharacterTypeAt
	// should handle both 0 and ' ' to be robust to future changes
	placeSimpleChar(ctx, 1, 0, ' ')
	charType = getCharacterTypeAt(ctx, 1, 0)
	if charType != CharTypeSpace {
		t.Errorf("Expected CharTypeSpace for space character entity, got %d", charType)
	}

	// Test that a space entity and empty position are treated the same
	emptyType := getCharacterTypeAt(ctx, 5, 5) // Empty position
	spaceType := getCharacterTypeAt(ctx, 1, 0) // Space entity
	if emptyType != spaceType {
		t.Errorf("Empty position and space entity should have same type, got empty=%d space=%d", emptyType, spaceType)
	}

	// Verify non-space characters are not detected as spaces
	placeSimpleChar(ctx, 2, 0, 'a')
	charType = getCharacterTypeAt(ctx, 2, 0)
	if charType == CharTypeSpace {
		t.Errorf("Expected non-space type for 'a', got CharTypeSpace")
	}
}

// Test isWordChar helper
func TestIsWordChar(t *testing.T) {
	tests := []struct {
		r        rune
		expected bool
	}{
		{'a', true},
		{'Z', true},
		{'5', true},
		{'_', true},
		{' ', false},
		{'.', false},
		{',', false},
		{'(', false},
		{')', false},
	}

	for _, tt := range tests {
		result := isWordChar(tt.r)
		if result != tt.expected {
			t.Errorf("isWordChar(%q) = %v, want %v", tt.r, result, tt.expected)
		}
	}
}

// Test isPunctuation helper
func TestIsPunctuation(t *testing.T) {
	tests := []struct {
		r        rune
		expected bool
	}{
		{'.', true},
		{',', true},
		{'(', true},
		{')', true},
		{'a', false},
		{'Z', false},
		{'_', false},
		{' ', false},
		{0, false}, // Test that 0 (empty position) is not punctuation
	}

	for _, tt := range tests {
		result := isPunctuation(tt.r)
		if result != tt.expected {
			t.Errorf("isPunctuation(%q) = %v, want %v", tt.r, result, tt.expected)
		}
	}
}
