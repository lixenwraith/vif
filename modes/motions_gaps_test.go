package modes

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
)

// TestWordMotionsWithFileContent tests word motions with file-based content placement
// This simulates how the game actually places characters from files - with position GAPS
func TestWordMotionsWithFileContent(t *testing.T) {
	ctx := createTestContext()

	// Simulate placing "package md5" from file content
	// The spawn system places individual characters at specific positions with gaps
	// "package" at positions 0-6, gap at 7, "md5" at positions 8-10
	// This is different from iterating "package md5" and skipping spaces

	// Place "package": p(0) a(1) c(2) k(3) a(4) g(5) e(6)
	placeChar(ctx, 0, 0, 'p')
	placeChar(ctx, 1, 0, 'a')
	placeChar(ctx, 2, 0, 'c')
	placeChar(ctx, 3, 0, 'k')
	placeChar(ctx, 4, 0, 'a')
	placeChar(ctx, 5, 0, 'g')
	placeChar(ctx, 6, 0, 'e')

	// Position 7 is a GAP (no entity, simulating space between words in file)

	// Place "md5": m(8) d(9) 5(10)
	placeChar(ctx, 8, 0, 'm')
	placeChar(ctx, 9, 0, 'd')
	placeChar(ctx, 10, 0, '5')

	// Test 'w' from position 0 - should jump over the gap to 'm' at position 8
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, 'w', 1)
	if ctx.CursorX != 8 {
		t.Errorf("w from start of 'package': expected X=8 (at 'm' in 'md5'), got X=%d", ctx.CursorX)
	}

	// Test 'b' from position 10 - should jump back to 'm' at position 8
	ctx.CursorX = 10
	ExecuteMotion(ctx, 'b', 1)
	if ctx.CursorX != 8 {
		t.Errorf("b from '5': expected X=8 (at 'm'), got X=%d", ctx.CursorX)
	}

	// Test 'b' again from position 8 - should jump back to 'p' at position 0
	ExecuteMotion(ctx, 'b', 1)
	if ctx.CursorX != 0 {
		t.Errorf("b from 'm': expected X=0 (at 'p' in 'package'), got X=%d", ctx.CursorX)
	}

	// Test 'e' from position 0 - should find end of "package" at position 6
	ctx.CursorX = 0
	ExecuteMotion(ctx, 'e', 1)
	if ctx.CursorX != 6 {
		t.Errorf("e from 'p': expected X=6 (at 'e' in 'package'), got X=%d", ctx.CursorX)
	}

	// Test 'e' again - should jump over gap to end of "md5" at position 10
	ExecuteMotion(ctx, 'e', 1)
	if ctx.CursorX != 10 {
		t.Errorf("e from end of 'package': expected X=10 (at '5' in 'md5'), got X=%d", ctx.CursorX)
	}
}

// TestMultipleWPressesWithGaps tests that multiple 'w' presses work correctly with position gaps
func TestMultipleWPressesWithGaps(t *testing.T) {
	ctx := createTestContext()

	// Simulate file content: "import crypto" with gaps
	// "import" at 0-5, gap at 6-8, "crypto" at 9-14

	// Place "import": i(0) m(1) p(2) o(3) r(4) t(5)
	placeChar(ctx, 0, 0, 'i')
	placeChar(ctx, 1, 0, 'm')
	placeChar(ctx, 2, 0, 'p')
	placeChar(ctx, 3, 0, 'o')
	placeChar(ctx, 4, 0, 'r')
	placeChar(ctx, 5, 0, 't')

	// Positions 6-8 are GAPS (simulating multiple spaces in file)

	// Place "crypto": c(9) r(10) y(11) p(12) t(13) o(14)
	placeChar(ctx, 9, 0, 'c')
	placeChar(ctx, 10, 0, 'r')
	placeChar(ctx, 11, 0, 'y')
	placeChar(ctx, 12, 0, 'p')
	placeChar(ctx, 13, 0, 't')
	placeChar(ctx, 14, 0, 'o')

	// Start at 'i' (position 0)
	ctx.CursorX = 0
	ctx.CursorY = 0

	// First 'w' press - should move to 'c' at position 9 (skipping the gap)
	ExecuteMotion(ctx, 'w', 1)
	if ctx.CursorX != 9 {
		t.Errorf("First w press: expected X=9 (at 'c' in 'crypto'), got X=%d", ctx.CursorX)
	}

	// Second 'w' press - should stay at 9 (no more words)
	prevX := ctx.CursorX
	ExecuteMotion(ctx, 'w', 1)
	if ctx.CursorX != prevX {
		t.Errorf("Second w press at end: expected X=%d (stay in place), got X=%d", prevX, ctx.CursorX)
	}

	// Test multiple 'w' presses from start with count
	ctx.CursorX = 0
	ExecuteMotion(ctx, 'w', 2)
	// First w: 0 -> 9, second w: stays at 9
	if ctx.CursorX != 9 {
		t.Errorf("2w from start: expected X=9 (at 'c'), got X=%d", ctx.CursorX)
	}

	// Verify we can navigate back through the gap
	ExecuteMotion(ctx, 'b', 1)
	if ctx.CursorX != 0 {
		t.Errorf("b from 'crypto': expected X=0 (at 'i' in 'import'), got X=%d", ctx.CursorX)
	}
}

// TestWordMotionsWithLargeGaps tests word motions with very large gaps between words
func TestWordMotionsWithLargeGaps(t *testing.T) {
	ctx := createTestContext()

	// Place words with large gaps between them
	// "foo" at 0-2, large gap, "bar" at 20-22, large gap, "baz" at 50-52

	placeChar(ctx, 0, 0, 'f')
	placeChar(ctx, 1, 0, 'o')
	placeChar(ctx, 2, 0, 'o')

	// Gap from 3-19

	placeChar(ctx, 20, 0, 'b')
	placeChar(ctx, 21, 0, 'a')
	placeChar(ctx, 22, 0, 'r')

	// Gap from 23-49

	placeChar(ctx, 50, 0, 'b')
	placeChar(ctx, 51, 0, 'a')
	placeChar(ctx, 52, 0, 'z')

	// Test 'w' across large gaps
	ctx.CursorX = 0
	ctx.CursorY = 0

	ExecuteMotion(ctx, 'w', 1)
	if ctx.CursorX != 20 {
		t.Errorf("w over large gap: expected X=20 (at 'b' in 'bar'), got X=%d", ctx.CursorX)
	}

	ExecuteMotion(ctx, 'w', 1)
	if ctx.CursorX != 50 {
		t.Errorf("w over second large gap: expected X=50 (at 'b' in 'baz'), got X=%d", ctx.CursorX)
	}

	// Test 'b' back across large gaps
	ExecuteMotion(ctx, 'b', 1)
	if ctx.CursorX != 20 {
		t.Errorf("b over large gap: expected X=20 (at 'b' in 'bar'), got X=%d", ctx.CursorX)
	}

	ExecuteMotion(ctx, 'b', 1)
	if ctx.CursorX != 0 {
		t.Errorf("b over second large gap: expected X=0 (at 'f' in 'foo'), got X=%d", ctx.CursorX)
	}

	// Test 'e' across large gaps
	ctx.CursorX = 0
	ExecuteMotion(ctx, 'e', 1)
	if ctx.CursorX != 2 {
		t.Errorf("e in first word: expected X=2 (at 'o' in 'foo'), got X=%d", ctx.CursorX)
	}

	ExecuteMotion(ctx, 'e', 1)
	if ctx.CursorX != 22 {
		t.Errorf("e over large gap: expected X=22 (at 'r' in 'bar'), got X=%d", ctx.CursorX)
	}

	ExecuteMotion(ctx, 'e', 1)
	if ctx.CursorX != 52 {
		t.Errorf("e over second large gap: expected X=52 (at 'z' in 'baz'), got X=%d", ctx.CursorX)
	}
}

// TestWordMotionsStartingFromGap tests motions when cursor starts in a gap position
func TestWordMotionsStartingFromGap(t *testing.T) {
	ctx := createTestContext()

	// Place "hello" at 0-4, gap at 5-9, "world" at 10-14
	placeChar(ctx, 0, 0, 'h')
	placeChar(ctx, 1, 0, 'e')
	placeChar(ctx, 2, 0, 'l')
	placeChar(ctx, 3, 0, 'l')
	placeChar(ctx, 4, 0, 'o')

	placeChar(ctx, 10, 0, 'w')
	placeChar(ctx, 11, 0, 'o')
	placeChar(ctx, 12, 0, 'r')
	placeChar(ctx, 13, 0, 'l')
	placeChar(ctx, 14, 0, 'd')

	// Start cursor in the middle of a gap (position 7)
	ctx.CursorX = 7
	ctx.CursorY = 0

	// 'w' from gap should find next word
	ExecuteMotion(ctx, 'w', 1)
	if ctx.CursorX != 10 {
		t.Errorf("w from gap: expected X=10 (at 'w' in 'world'), got X=%d", ctx.CursorX)
	}

	// 'b' from gap should find previous word
	ctx.CursorX = 7
	ExecuteMotion(ctx, 'b', 1)
	if ctx.CursorX != 0 {
		t.Errorf("b from gap: expected X=0 (at 'h' in 'hello'), got X=%d", ctx.CursorX)
	}

	// 'e' from gap should find end of next word
	ctx.CursorX = 7
	ExecuteMotion(ctx, 'e', 1)
	if ctx.CursorX != 14 {
		t.Errorf("e from gap: expected X=14 (at 'd' in 'world'), got X=%d", ctx.CursorX)
	}
}

// TestWORDMotionsWithFileContentGaps tests WORD motions with file-based gaps
func TestWORDMotionsWithFileContentGaps(t *testing.T) {
	ctx := createTestContext()

	// Simulate "func(x,y)" at 0-8, gap at 9-11, "return" at 12-17
	// WORDs should treat "func(x,y)" as single WORD and "return" as another

	placeChar(ctx, 0, 0, 'f')
	placeChar(ctx, 1, 0, 'u')
	placeChar(ctx, 2, 0, 'n')
	placeChar(ctx, 3, 0, 'c')
	placeChar(ctx, 4, 0, '(')
	placeChar(ctx, 5, 0, 'x')
	placeChar(ctx, 6, 0, ',')
	placeChar(ctx, 7, 0, 'y')
	placeChar(ctx, 8, 0, ')')

	// Gap at 9-11

	placeChar(ctx, 12, 0, 'r')
	placeChar(ctx, 13, 0, 'e')
	placeChar(ctx, 14, 0, 't')
	placeChar(ctx, 15, 0, 'u')
	placeChar(ctx, 16, 0, 'r')
	placeChar(ctx, 17, 0, 'n')

	// Test 'W' - should treat "func(x,y)" as one WORD and jump to "return"
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, 'W', 1)
	if ctx.CursorX != 12 {
		t.Errorf("W over gap: expected X=12 (at 'r' in 'return'), got X=%d", ctx.CursorX)
	}

	// Test 'B' back
	ExecuteMotion(ctx, 'B', 1)
	if ctx.CursorX != 0 {
		t.Errorf("B over gap: expected X=0 (at 'f' in 'func'), got X=%d", ctx.CursorX)
	}

	// Test 'E' - should find end of "func(x,y)" at ')'
	ctx.CursorX = 0
	ExecuteMotion(ctx, 'E', 1)
	if ctx.CursorX != 8 {
		t.Errorf("E in first WORD: expected X=8 (at ')'), got X=%d", ctx.CursorX)
	}

	// Test 'E' again - should jump over gap to end of "return"
	ExecuteMotion(ctx, 'E', 1)
	if ctx.CursorX != 17 {
		t.Errorf("E over gap: expected X=17 (at 'n' in 'return'), got X=%d", ctx.CursorX)
	}
}

func TestGetCharAtSpaceHandling(t *testing.T) {
	ctx := createTestContext()

	// Test 1: Empty position (no entity) - should return 0
	result := getCharAt(ctx, 5, 5)
	if result != 0 {
		t.Errorf("getCharAt at empty position: expected 0, got %v (%q)", result, result)
	}

	// Test 2: Regular character entity - should return the rune
	placeChar(ctx, 10, 10, 'a')
	result = getCharAt(ctx, 10, 10)
	if result != 'a' {
		t.Errorf("getCharAt with regular char: expected 'a', got %v (%q)", result, result)
	}

	// Test 3: Space character entity - should return 0 (defensive handling)
	// Create an entity with a space character directly (edge case - shouldn't normally exist)
	entity := ctx.World.CreateEntity()
	ctx.World.Positions.Add(entity, components.PositionComponent{X: 15, Y: 15})
	ctx.World.Characters.Add(entity, components.CharacterComponent{Rune: ' ', Style: tcell.StyleDefault})
	ctx.World.Sequences.Add(entity, components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceGreen,
		Level: components.LevelBright,
	})

	result = getCharAt(ctx, 15, 15)
	if result != 0 {
		t.Errorf("getCharAt with space char entity: expected 0 (defensive handling), got %v (%q)", result, result)
	}

	// Test 4: Multiple positions with different characters
	placeChar(ctx, 0, 0, 'x')
	placeChar(ctx, 1, 0, 'y')
	placeChar(ctx, 2, 0, 'z')

	if getCharAt(ctx, 0, 0) != 'x' {
		t.Errorf("getCharAt at (0,0): expected 'x', got %q", getCharAt(ctx, 0, 0))
	}
	if getCharAt(ctx, 1, 0) != 'y' {
		t.Errorf("getCharAt at (1,0): expected 'y', got %q", getCharAt(ctx, 1, 0))
	}
	if getCharAt(ctx, 2, 0) != 'z' {
		t.Errorf("getCharAt at (2,0): expected 'z', got %q", getCharAt(ctx, 2, 0))
	}

	// Test 5: Position between characters should return 0
	result = getCharAt(ctx, 0, 1) // Different row, no char
	if result != 0 {
		t.Errorf("getCharAt at empty row: expected 0, got %v (%q)", result, result)
	}

	// Test 6: Punctuation and special characters should work normally
	placeChar(ctx, 20, 5, '.')
	placeChar(ctx, 21, 5, ',')
	placeChar(ctx, 22, 5, '!')

	if getCharAt(ctx, 20, 5) != '.' {
		t.Errorf("getCharAt with '.': expected '.', got %q", getCharAt(ctx, 20, 5))
	}
	if getCharAt(ctx, 21, 5) != ',' {
		t.Errorf("getCharAt with ',': expected ',', got %q", getCharAt(ctx, 21, 5))
	}
	if getCharAt(ctx, 22, 5) != '!' {
		t.Errorf("getCharAt with '!': expected '!', got %q", getCharAt(ctx, 22, 5))
	}
}