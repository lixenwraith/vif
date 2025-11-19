package modes

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// Helper function to create a test context
func createTestContext() *engine.GameContext {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	screen.SetSize(80, 24)

	ctx := engine.NewGameContext(screen)
	ctx.GameWidth = 80
	ctx.GameHeight = 20
	ctx.CursorX = 0
	ctx.CursorY = 0
	return ctx
}

// Helper function to place a character at a position
func placeChar(ctx *engine.GameContext, x, y int, r rune) {
	entity := ctx.World.CreateEntity()
	ctx.World.AddComponent(entity, components.PositionComponent{X: x, Y: y})
	ctx.World.AddComponent(entity, components.CharacterComponent{Rune: r, Style: tcell.StyleDefault})
	ctx.World.AddComponent(entity, components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceGreen,
		Level: components.LevelBright,
	})
}

// Test H/M/L motions (screen position jumps)
func TestHMLMotions(t *testing.T) {
	ctx := createTestContext()
	ctx.CursorX = 10
	ctx.CursorY = 10

	// Test H - jump to top
	ExecuteMotion(ctx, 'H', 1)
	if ctx.CursorY != 0 {
		t.Errorf("H motion failed: expected Y=0, got Y=%d", ctx.CursorY)
	}
	if ctx.CursorX != 10 {
		t.Errorf("H motion changed X: expected X=10, got X=%d", ctx.CursorX)
	}

	// Test M - jump to middle
	ctx.CursorY = 0
	ExecuteMotion(ctx, 'M', 1)
	expectedMiddle := ctx.GameHeight / 2
	if ctx.CursorY != expectedMiddle {
		t.Errorf("M motion failed: expected Y=%d, got Y=%d", expectedMiddle, ctx.CursorY)
	}

	// Test L - jump to bottom
	ctx.CursorY = 0
	ExecuteMotion(ctx, 'L', 1)
	expectedBottom := ctx.GameHeight - 1
	if ctx.CursorY != expectedBottom {
		t.Errorf("L motion failed: expected Y=%d, got Y=%d", expectedBottom, ctx.CursorY)
	}
}

// Test vim-style word motions w/e/b
func TestVimWordMotions(t *testing.T) {
	ctx := createTestContext()

	// Setup: "foo_bar, baz.qux" at y=0
	text := "foo_bar, baz.qux"
	for i, r := range text {
		if r != ' ' {
			placeChar(ctx, i, 0, r)
		}
	}

	// Test 'w' - should jump to start of next word
	ctx.CursorX = 0 // at 'f' in "foo_bar"
	ctx.CursorY = 0
	ExecuteMotion(ctx, 'w', 1)
	// Should jump to ',' (punctuation is a separate word in vim)
	if ctx.CursorX != 7 {
		t.Errorf("w motion failed: expected X=7 (at ','), got X=%d", ctx.CursorX)
	}

	// Test 'w' again - should jump to 'b' in "baz"
	ExecuteMotion(ctx, 'w', 1)
	if ctx.CursorX != 9 {
		t.Errorf("w motion failed: expected X=9 (at 'b'), got X=%d", ctx.CursorX)
	}

	// Test 'e' - should jump to end of word
	ctx.CursorX = 0 // at 'f' in "foo_bar"
	ExecuteMotion(ctx, 'e', 1)
	if ctx.CursorX != 6 {
		t.Errorf("e motion failed: expected X=6 (at 'r'), got X=%d", ctx.CursorX)
	}

	// Test 'b' - should jump to start of previous word
	ctx.CursorX = 9 // at 'b' in "baz"
	ExecuteMotion(ctx, 'b', 1)
	// Should jump to ',' at position 7 (comma is a separate punctuation word)
	if ctx.CursorX != 7 {
		t.Errorf("b motion failed: expected X=7 (at ','), got X=%d", ctx.CursorX)
	}
}

// Test WORD motions W/E/B (space-delimited)
func TestWORDMotions(t *testing.T) {
	ctx := createTestContext()

	// Setup: "foo_bar, baz.qux" at y=0
	text := "foo_bar, baz.qux"
	for i, r := range text {
		if r != ' ' {
			placeChar(ctx, i, 0, r)
		}
	}

	// Test 'W' - should jump to next space-delimited WORD
	ctx.CursorX = 0 // at 'f' in "foo_bar,"
	ctx.CursorY = 0
	ExecuteMotion(ctx, 'W', 1)
	// Should jump to 'b' in "baz.qux" (skipping entire "foo_bar,")
	if ctx.CursorX != 9 {
		t.Errorf("W motion failed: expected X=9 (at 'b'), got X=%d", ctx.CursorX)
	}

	// Test 'E' - should jump to end of WORD
	ctx.CursorX = 0 // at 'f' in "foo_bar,"
	ExecuteMotion(ctx, 'E', 1)
	if ctx.CursorX != 7 {
		t.Errorf("E motion failed: expected X=7 (at ','), got X=%d", ctx.CursorX)
	}

	// Test 'B' - should jump to start of previous WORD
	ctx.CursorX = 9 // at 'b' in "baz.qux"
	ExecuteMotion(ctx, 'B', 1)
	if ctx.CursorX != 0 {
		t.Errorf("B motion failed: expected X=0 (at 'f'), got X=%d", ctx.CursorX)
	}
}

// Test ^ motion (first non-whitespace)
func TestCaretMotion(t *testing.T) {
	ctx := createTestContext()

	// Setup: "   hello" at y=0 (3 spaces before 'h')
	placeChar(ctx, 3, 0, 'h')
	placeChar(ctx, 4, 0, 'e')
	placeChar(ctx, 5, 0, 'l')
	placeChar(ctx, 6, 0, 'l')
	placeChar(ctx, 7, 0, 'o')

	// Test ^ - should jump to first non-whitespace
	ctx.CursorX = 10
	ctx.CursorY = 0
	ExecuteMotion(ctx, '^', 1)
	if ctx.CursorX != 3 {
		t.Errorf("^ motion failed: expected X=3, got X=%d", ctx.CursorX)
	}

	// Test on empty line - should go to position 0
	ctx.CursorX = 10
	ctx.CursorY = 1 // Empty line
	ExecuteMotion(ctx, '^', 1)
	if ctx.CursorX != 0 {
		t.Errorf("^ motion on empty line: expected X=0, got X=%d", ctx.CursorX)
	}
}

// Test {/} paragraph motions
func TestParagraphMotions(t *testing.T) {
	ctx := createTestContext()

	// Setup: Text on lines 2, 3, 5, 6, 8 (empty: 0, 1, 4, 7, 9+)
	placeChar(ctx, 0, 2, 'a')
	placeChar(ctx, 0, 3, 'b')
	placeChar(ctx, 0, 5, 'c')
	placeChar(ctx, 0, 6, 'd')
	placeChar(ctx, 0, 8, 'e')

	// Test } - jump to next empty line
	ctx.CursorX = 0
	ctx.CursorY = 2
	ExecuteMotion(ctx, '}', 1)
	if ctx.CursorY != 4 {
		t.Errorf("} motion failed: expected Y=4, got Y=%d", ctx.CursorY)
	}

	// Test } again
	ExecuteMotion(ctx, '}', 1)
	if ctx.CursorY != 7 {
		t.Errorf("} motion failed: expected Y=7, got Y=%d", ctx.CursorY)
	}

	// Test { - jump to previous empty line
	ctx.CursorY = 8
	ExecuteMotion(ctx, '{', 1)
	if ctx.CursorY != 7 {
		t.Errorf("{ motion failed: expected Y=7, got Y=%d", ctx.CursorY)
	}

	// Test { again
	ExecuteMotion(ctx, '{', 1)
	if ctx.CursorY != 4 {
		t.Errorf("{ motion failed: expected Y=4, got Y=%d", ctx.CursorY)
	}
}

// Test SPACE motion in normal mode
func TestSpaceMotion(t *testing.T) {
	ctx := createTestContext()
	ctx.CursorX = 5
	ctx.CursorY = 5

	// Test space - should move right like 'l'
	ExecuteMotion(ctx, ' ', 1)
	if ctx.CursorX != 6 {
		t.Errorf("SPACE motion failed: expected X=6, got X=%d", ctx.CursorX)
	}

	// Test space with count
	ctx.CursorX = 5
	ExecuteMotion(ctx, ' ', 3)
	if ctx.CursorX != 8 {
		t.Errorf("SPACE motion with count failed: expected X=8, got X=%d", ctx.CursorX)
	}

	// Test space at right edge - should stay at edge
	ctx.CursorX = ctx.GameWidth - 1
	ExecuteMotion(ctx, ' ', 1)
	if ctx.CursorX != ctx.GameWidth-1 {
		t.Errorf("SPACE motion at edge: expected X=%d, got X=%d", ctx.GameWidth-1, ctx.CursorX)
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

// Test motion with count
func TestMotionWithCount(t *testing.T) {
	ctx := createTestContext()

	// Test H/M/L with count (count should be ignored for these)
	ctx.CursorX = 10
	ctx.CursorY = 10
	ExecuteMotion(ctx, 'H', 5) // Count doesn't affect H
	if ctx.CursorY != 0 {
		t.Errorf("H with count: expected Y=0, got Y=%d", ctx.CursorY)
	}

	// Test paragraph motion with count
	// Clear previous context
	ctx = createTestContext()
	placeChar(ctx, 0, 2, 'a')
	placeChar(ctx, 0, 5, 'b')
	placeChar(ctx, 0, 8, 'c')

	ctx.CursorY = 2
	ExecuteMotion(ctx, '}', 2) // Jump forward to empty line twice
	// Lines with chars: 2, 5, 8. Empty lines: 0, 1, 3, 4, 6, 7, 9+
	// First call from y=2 finds y=3 (empty), second call from y=3 finds y=4 (empty)
	if ctx.CursorY != 4 {
		t.Errorf("} with count=2: expected Y=4, got Y=%d", ctx.CursorY)
	}
}

// Test edge cases
func TestMotionEdgeCases(t *testing.T) {
	ctx := createTestContext()

	// Test motions on empty screen
	ctx.CursorX = 10
	ctx.CursorY = 10

	// w/e/b on empty line should stay in place
	ExecuteMotion(ctx, 'w', 1)
	if ctx.CursorX != 10 {
		t.Errorf("w on empty line: should stay at X=10, got X=%d", ctx.CursorX)
	}

	ExecuteMotion(ctx, 'e', 1)
	if ctx.CursorX != 10 {
		t.Errorf("e on empty line: should stay at X=10, got X=%d", ctx.CursorX)
	}

	ExecuteMotion(ctx, 'b', 1)
	if ctx.CursorX != 10 {
		t.Errorf("b on empty line: should stay at X=10, got X=%d", ctx.CursorX)
	}

	// Same for WORD motions
	ExecuteMotion(ctx, 'W', 1)
	if ctx.CursorX != 10 {
		t.Errorf("W on empty line: should stay at X=10, got X=%d", ctx.CursorX)
	}
}

// Test vim word motions starting from space
func TestVimWordMotionsFromSpace(t *testing.T) {
	ctx := createTestContext()

	// Setup: "foo   bar" at y=0 (3 spaces between foo and bar)
	// Positions: f(0)o(1)o(2) (3)(4)(5) b(6)a(7)r(8)
	placeChar(ctx, 0, 0, 'f')
	placeChar(ctx, 1, 0, 'o')
	placeChar(ctx, 2, 0, 'o')
	placeChar(ctx, 6, 0, 'b')
	placeChar(ctx, 7, 0, 'a')
	placeChar(ctx, 8, 0, 'r')

	// Test 'w' starting from space - should jump to next word
	ctx.CursorX = 3 // On first space after "foo"
	ctx.CursorY = 0
	ExecuteMotion(ctx, 'w', 1)
	if ctx.CursorX != 6 {
		t.Errorf("w from space: expected X=6 (at 'b'), got X=%d", ctx.CursorX)
	}

	// Test 'w' starting from middle of spaces
	ctx.CursorX = 4 // On second space
	ExecuteMotion(ctx, 'w', 1)
	if ctx.CursorX != 6 {
		t.Errorf("w from middle space: expected X=6 (at 'b'), got X=%d", ctx.CursorX)
	}

	// Test 'b' starting from space - should jump to previous word start
	ctx.CursorX = 4 // On space
	ExecuteMotion(ctx, 'b', 1)
	if ctx.CursorX != 0 {
		t.Errorf("b from space: expected X=0 (at 'f'), got X=%d", ctx.CursorX)
	}
}

// Test vim word motions with punctuation transitions
func TestVimWordMotionsPunctuationTransitions(t *testing.T) {
	ctx := createTestContext()

	// Setup: "word...next" at y=0
	// Positions: w(0)o(1)r(2)d(3).(4).(5).(6)n(7)e(8)x(9)t(10)
	text := "word...next"
	for i, r := range text {
		placeChar(ctx, i, 0, r)
	}

	// Test 'w' from word to punctuation
	ctx.CursorX = 0 // at 'w'
	ctx.CursorY = 0
	ExecuteMotion(ctx, 'w', 1)
	if ctx.CursorX != 4 {
		t.Errorf("w word->punct: expected X=4 (at first '.'), got X=%d", ctx.CursorX)
	}

	// Test 'w' from punctuation to punctuation (should skip as one group)
	ctx.CursorX = 4 // at first '.'
	ExecuteMotion(ctx, 'w', 1)
	if ctx.CursorX != 7 {
		t.Errorf("w punct->word: expected X=7 (at 'n'), got X=%d", ctx.CursorX)
	}

	// Test 'e' from word through punctuation
	ctx.CursorX = 0 // at 'w'
	ExecuteMotion(ctx, 'e', 1)
	if ctx.CursorX != 3 {
		t.Errorf("e word end: expected X=3 (at 'd'), got X=%d", ctx.CursorX)
	}

	// Test 'e' from end of word (should jump to end of next word)
	ExecuteMotion(ctx, 'e', 1)
	if ctx.CursorX != 6 {
		t.Errorf("e from word end: expected X=6 (at last '.'), got X=%d", ctx.CursorX)
	}

	// Test 'b' from word to punctuation
	ctx.CursorX = 7 // at 'n'
	ExecuteMotion(ctx, 'b', 1)
	if ctx.CursorX != 4 {
		t.Errorf("b word->punct: expected X=4 (at first '.'), got X=%d", ctx.CursorX)
	}

	// Test 'b' from punctuation to word
	ExecuteMotion(ctx, 'b', 1)
	if ctx.CursorX != 0 {
		t.Errorf("b punct->word: expected X=0 (at 'w'), got X=%d", ctx.CursorX)
	}
}

// Test vim word motions at screen boundaries
func TestVimWordMotionsBoundaries(t *testing.T) {
	ctx := createTestContext()

	// Setup: "word" at the very end of screen width
	endPos := ctx.GameWidth - 4
	placeChar(ctx, endPos, 0, 'w')
	placeChar(ctx, endPos+1, 0, 'o')
	placeChar(ctx, endPos+2, 0, 'r')
	placeChar(ctx, endPos+3, 0, 'd')

	// Test 'w' near right edge - should not move past boundary
	ctx.CursorX = endPos
	ctx.CursorY = 0
	startX := ctx.CursorX
	ExecuteMotion(ctx, 'w', 1)
	// Since we're at the last word, should stay in place
	if ctx.CursorX != startX {
		t.Errorf("w at right edge: expected X=%d, got X=%d", startX, ctx.CursorX)
	}

	// Test 'e' at right edge
	ctx.CursorX = endPos
	ExecuteMotion(ctx, 'e', 1)
	if ctx.CursorX != endPos+3 {
		t.Errorf("e to word end: expected X=%d, got X=%d", endPos+3, ctx.CursorX)
	}

	// Setup: "word" at the very beginning
	ctx = createTestContext()
	placeChar(ctx, 0, 0, 'w')
	placeChar(ctx, 1, 0, 'o')
	placeChar(ctx, 2, 0, 'r')
	placeChar(ctx, 3, 0, 'd')

	// Test 'b' at left edge - should stay in place
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, 'b', 1)
	if ctx.CursorX != 0 {
		t.Errorf("b at left edge: expected X=0, got X=%d", ctx.CursorX)
	}
}

// Test multiple consecutive word motions
func TestVimWordMotionsConsecutive(t *testing.T) {
	ctx := createTestContext()

	// Setup: "one two,three" at y=0
	// Positions: o(0)n(1)e(2) (3) t(4)w(5)o(6),(7)t(8)h(9)r(10)e(11)e(12)
	text := "one two,three"
	for i, r := range text {
		if r != ' ' {
			placeChar(ctx, i, 0, r)
		}
	}

	// Test multiple 'w' commands
	ctx.CursorX = 0
	ctx.CursorY = 0

	ExecuteMotion(ctx, 'w', 1) // Should go to position 4 ('t' in "two")
	if ctx.CursorX != 4 {
		t.Errorf("First w: expected X=4, got X=%d", ctx.CursorX)
	}

	ExecuteMotion(ctx, 'w', 1) // Should go to position 7 (',')
	if ctx.CursorX != 7 {
		t.Errorf("Second w: expected X=7, got X=%d", ctx.CursorX)
	}

	ExecuteMotion(ctx, 'w', 1) // Should go to position 8 ('t' in "three")
	if ctx.CursorX != 8 {
		t.Errorf("Third w: expected X=8, got X=%d", ctx.CursorX)
	}

	// Test multiple 'b' commands (reverse)
	ExecuteMotion(ctx, 'b', 1) // Should go back to 7 (',')
	if ctx.CursorX != 7 {
		t.Errorf("First b: expected X=7, got X=%d", ctx.CursorX)
	}

	ExecuteMotion(ctx, 'b', 1) // Should go back to 4 ('t' in "two")
	if ctx.CursorX != 4 {
		t.Errorf("Second b: expected X=4, got X=%d", ctx.CursorX)
	}

	ExecuteMotion(ctx, 'b', 1) // Should go back to 0 ('o' in "one")
	if ctx.CursorX != 0 {
		t.Errorf("Third b: expected X=0, got X=%d", ctx.CursorX)
	}
}

// Test WORD motions from different starting positions
func TestWORDMotionsFromSpace(t *testing.T) {
	ctx := createTestContext()

	// Setup: "foo   bar" at y=0 (3 spaces between)
	// Positions: f(0)o(1)o(2) (3)(4)(5) b(6)a(7)r(8)
	placeChar(ctx, 0, 0, 'f')
	placeChar(ctx, 1, 0, 'o')
	placeChar(ctx, 2, 0, 'o')
	placeChar(ctx, 6, 0, 'b')
	placeChar(ctx, 7, 0, 'a')
	placeChar(ctx, 8, 0, 'r')

	// Test 'W' starting from space - should jump to next WORD
	ctx.CursorX = 3 // On first space after "foo"
	ctx.CursorY = 0
	ExecuteMotion(ctx, 'W', 1)
	if ctx.CursorX != 6 {
		t.Errorf("W from space: expected X=6 (at 'b'), got X=%d", ctx.CursorX)
	}

	// Test 'W' starting from middle of spaces
	ctx.CursorX = 4 // On second space
	ExecuteMotion(ctx, 'W', 1)
	if ctx.CursorX != 6 {
		t.Errorf("W from middle space: expected X=6 (at 'b'), got X=%d", ctx.CursorX)
	}

	// Test 'B' starting from space - should jump to previous WORD start
	ctx.CursorX = 4 // On space
	ExecuteMotion(ctx, 'B', 1)
	if ctx.CursorX != 0 {
		t.Errorf("B from space: expected X=0 (at 'f'), got X=%d", ctx.CursorX)
	}
}

// Test WORD motions with various punctuation (should all be treated as one WORD)
func TestWORDMotionsWithPunctuation(t *testing.T) {
	ctx := createTestContext()

	// Setup: "foo_bar, baz...qux" at y=0
	// Unlike word motions, WORDs treat punctuation as part of the WORD
	text := "foo_bar, baz...qux"
	for i, r := range text {
		if r != ' ' {
			placeChar(ctx, i, 0, r)
		}
	}

	// Test 'W' - should skip entire "foo_bar," and land on 'b' in "baz...qux"
	ctx.CursorX = 0 // at 'f'
	ctx.CursorY = 0
	ExecuteMotion(ctx, 'W', 1)
	if ctx.CursorX != 9 {
		t.Errorf("W with punctuation: expected X=9 (at 'b'), got X=%d", ctx.CursorX)
	}

	// Test 'E' - should find end of "foo_bar," (the comma)
	ctx.CursorX = 0
	ExecuteMotion(ctx, 'E', 1)
	if ctx.CursorX != 7 {
		t.Errorf("E with punctuation: expected X=7 (at ','), got X=%d", ctx.CursorX)
	}

	// Test 'E' from middle of WORD
	ctx.CursorX = 2 // at second 'o' in "foo_bar,"
	ExecuteMotion(ctx, 'E', 1)
	if ctx.CursorX != 7 {
		t.Errorf("E from middle: expected X=7 (at ','), got X=%d", ctx.CursorX)
	}
}

// Test WORD motions at boundaries
func TestWORDMotionsBoundaries(t *testing.T) {
	ctx := createTestContext()

	// Setup: "word" at the very end of screen
	endPos := ctx.GameWidth - 4
	placeChar(ctx, endPos, 0, 'w')
	placeChar(ctx, endPos+1, 0, 'o')
	placeChar(ctx, endPos+2, 0, 'r')
	placeChar(ctx, endPos+3, 0, 'd')

	// Test 'W' near right edge - should stay in place
	ctx.CursorX = endPos
	ctx.CursorY = 0
	startX := ctx.CursorX
	ExecuteMotion(ctx, 'W', 1)
	if ctx.CursorX != startX {
		t.Errorf("W at right edge: expected X=%d, got X=%d", startX, ctx.CursorX)
	}

	// Test 'E' at right edge - should move to last char
	ctx.CursorX = endPos
	ExecuteMotion(ctx, 'E', 1)
	if ctx.CursorX != endPos+3 {
		t.Errorf("E at right edge: expected X=%d, got X=%d", endPos+3, ctx.CursorX)
	}

	// Setup: "word" at the very beginning
	ctx = createTestContext()
	placeChar(ctx, 0, 0, 'w')
	placeChar(ctx, 1, 0, 'o')
	placeChar(ctx, 2, 0, 'r')
	placeChar(ctx, 3, 0, 'd')

	// Test 'B' at left edge - should stay in place
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, 'B', 1)
	if ctx.CursorX != 0 {
		t.Errorf("B at left edge: expected X=0, got X=%d", ctx.CursorX)
	}
}

// Test multiple consecutive WORD motions
func TestWORDMotionsConsecutive(t *testing.T) {
	ctx := createTestContext()

	// Setup: "one.two three,four" at y=0
	text := "one.two three,four"
	for i, r := range text {
		if r != ' ' {
			placeChar(ctx, i, 0, r)
		}
	}

	// Test multiple 'W' commands
	ctx.CursorX = 0
	ctx.CursorY = 0

	ExecuteMotion(ctx, 'W', 1) // Should go to 't' in "three,four"
	if ctx.CursorX != 8 {
		t.Errorf("First W: expected X=8, got X=%d", ctx.CursorX)
	}

	// Test 'W' at end - should stay
	ExecuteMotion(ctx, 'W', 1)
	if ctx.CursorX != 8 {
		t.Errorf("W at end: expected X=8, got X=%d", ctx.CursorX)
	}

	// Test 'B' back
	ExecuteMotion(ctx, 'B', 1)
	if ctx.CursorX != 0 {
		t.Errorf("B back: expected X=0, got X=%d", ctx.CursorX)
	}
}

// Test motion count handling - ensure each iteration starts from updated position
func TestMotionCountHandling(t *testing.T) {
	ctx := createTestContext()

	// Setup: "one two three four five" at y=0
	// Positions: o(0)n(1)e(2) (3) t(4)w(5)o(6) (7) t(8)h(9)r(10)e(11)e(12) (13) f(14)o(15)u(16)r(17) (18) f(19)i(20)v(21)e(22)
	text := "one two three four five"
	for i, r := range text {
		if r != ' ' {
			placeChar(ctx, i, 0, r)
		}
	}

	// Test 3w - should move 3 words forward
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, 'w', 3)
	// First w: 0 -> 4 ('t' in "two")
	// Second w: 4 -> 8 ('t' in "three")
	// Third w: 8 -> 14 ('f' in "four")
	if ctx.CursorX != 14 {
		t.Errorf("3w motion: expected X=14 (at 'f' in 'four'), got X=%d", ctx.CursorX)
	}

	// Test 2b - should move 2 words backward
	ExecuteMotion(ctx, 'b', 2)
	// First b: 14 -> 8 ('t' in "three")
	// Second b: 8 -> 4 ('t' in "two")
	if ctx.CursorX != 4 {
		t.Errorf("2b motion: expected X=4 (at 't' in 'two'), got X=%d", ctx.CursorX)
	}

	// Test 2e - should move to end of 2nd word ahead
	ctx.CursorX = 0
	ExecuteMotion(ctx, 'e', 2)
	// First e: 0 -> 2 (end of "one")
	// Second e: 2 -> 6 (end of "two")
	if ctx.CursorX != 6 {
		t.Errorf("2e motion: expected X=6 (at 'o' in 'two'), got X=%d", ctx.CursorX)
	}

	// Test 2W - WORD forward with count
	ctx.CursorX = 0
	ExecuteMotion(ctx, 'W', 2)
	// First W: 0 -> 4 ('t' in "two")
	// Second W: 4 -> 8 ('t' in "three")
	if ctx.CursorX != 8 {
		t.Errorf("2W motion: expected X=8 (at 't' in 'three'), got X=%d", ctx.CursorX)
	}

	// Test 2E - WORD end with count
	ctx.CursorX = 0
	ExecuteMotion(ctx, 'E', 2)
	// First E: 0 -> 2 (end of "one")
	// Second E: 2 -> 6 (end of "two")
	if ctx.CursorX != 6 {
		t.Errorf("2E motion: expected X=6 (at 'o' in 'two'), got X=%d", ctx.CursorX)
	}

	// Test 2B - WORD backward with count
	ctx.CursorX = 14 // at 'f' in "four"
	ExecuteMotion(ctx, 'B', 2)
	// First B: 14 -> 8 ('t' in "three")
	// Second B: 8 -> 4 ('t' in "two")
	if ctx.CursorX != 4 {
		t.Errorf("2B motion: expected X=4 (at 't' in 'two'), got X=%d", ctx.CursorX)
	}
}

// Test motion count boundary conditions - large counts should stop at boundary
func TestMotionCountBoundaryHandling(t *testing.T) {
	ctx := createTestContext()

	// Setup: "one two three" at y=0
	text := "one two three"
	for i, r := range text {
		if r != ' ' {
			placeChar(ctx, i, 0, r)
		}
	}

	// Test 100w - should stop at last word, not loop unnecessarily
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, 'w', 100)
	// Should stop at 't' in "three" (position 8)
	if ctx.CursorX != 8 {
		t.Errorf("100w motion: expected X=8 (at 't' in 'three'), got X=%d", ctx.CursorX)
	}

	// Test 100b - should stop at first word
	ExecuteMotion(ctx, 'b', 100)
	// Should stop at 'o' in "one" (position 0)
	if ctx.CursorX != 0 {
		t.Errorf("100b motion: expected X=0 (at 'o' in 'one'), got X=%d", ctx.CursorX)
	}

	// Test 100h - should stop at left edge
	ctx.CursorX = 5
	ExecuteMotion(ctx, 'h', 100)
	if ctx.CursorX != 0 {
		t.Errorf("100h motion: expected X=0 (left edge), got X=%d", ctx.CursorX)
	}

	// Test 100l - should stop at right edge
	ctx.CursorX = 5
	ExecuteMotion(ctx, 'l', 100)
	if ctx.CursorX != ctx.GameWidth-1 {
		t.Errorf("100l motion: expected X=%d (right edge), got X=%d", ctx.GameWidth-1, ctx.CursorX)
	}

	// Test 100j - should stop at bottom
	ctx.CursorY = 5
	ExecuteMotion(ctx, 'j', 100)
	if ctx.CursorY != ctx.GameHeight-1 {
		t.Errorf("100j motion: expected Y=%d (bottom), got Y=%d", ctx.GameHeight-1, ctx.CursorY)
	}

	// Test 100k - should stop at top
	ctx.CursorY = 5
	ExecuteMotion(ctx, 'k', 100)
	if ctx.CursorY != 0 {
		t.Errorf("100k motion: expected Y=0 (top), got Y=%d", ctx.CursorY)
	}

	// Test 100 space - should stop at right edge
	ctx.CursorX = 5
	ExecuteMotion(ctx, ' ', 100)
	if ctx.CursorX != ctx.GameWidth-1 {
		t.Errorf("100 space motion: expected X=%d (right edge), got X=%d", ctx.GameWidth-1, ctx.CursorX)
	}
}

// Test paragraph motion count handling
func TestParagraphMotionCountHandling(t *testing.T) {
	ctx := createTestContext()

	// Setup: Text on lines 2, 5, 8, 11 (empty: 0, 1, 3, 4, 6, 7, 9, 10, 12+)
	placeChar(ctx, 0, 2, 'a')
	placeChar(ctx, 0, 5, 'b')
	placeChar(ctx, 0, 8, 'c')
	placeChar(ctx, 0, 11, 'd')

	// Test 2} - jump forward 2 empty lines
	ctx.CursorY = 2
	ExecuteMotion(ctx, '}', 2)
	// First }: 2 -> 3 (first empty line)
	// Second }: 3 -> 4 (second empty line)
	if ctx.CursorY != 4 {
		t.Errorf("2} motion: expected Y=4, got Y=%d", ctx.CursorY)
	}

	// Test 3} - jump forward 3 empty lines
	ctx.CursorY = 2
	ExecuteMotion(ctx, '}', 3)
	// First }: 2 -> 3
	// Second }: 3 -> 4
	// Third }: 4 -> 6
	if ctx.CursorY != 6 {
		t.Errorf("3} motion: expected Y=6, got Y=%d", ctx.CursorY)
	}

	// Test 2{ - jump backward 2 empty lines
	ctx.CursorY = 11
	ExecuteMotion(ctx, '{', 2)
	// First {: 11 -> 10
	// Second {: 10 -> 9
	if ctx.CursorY != 9 {
		t.Errorf("2{ motion: expected Y=9, got Y=%d", ctx.CursorY)
	}

	// Test 100{ - should stop at top
	ctx.CursorY = 5
	ExecuteMotion(ctx, '{', 100)
	if ctx.CursorY != 0 {
		t.Errorf("100{ motion: expected Y=0 (top), got Y=%d", ctx.CursorY)
	}

	// Test 100} - should stop when can't find more empty lines
	ctx.CursorY = 2
	ExecuteMotion(ctx, '}', 100)
	// Should stop when it can't find any more empty lines
	if ctx.CursorY < 12 {
		t.Errorf("100} motion: expected Y>=12 (stopped at boundary), got Y=%d", ctx.CursorY)
	}
}

// Test motion count with mixed word and punctuation
func TestMotionCountMixedContent(t *testing.T) {
	ctx := createTestContext()

	// Setup: "foo, bar, baz, qux" at y=0
	// Positions: f(0)o(1)o(2),(3) (4) b(5)a(6)r(7),(8) (9) b(10)a(11)z(12),(13) (14) q(15)u(16)x(17)
	text := "foo, bar, baz, qux"
	for i, r := range text {
		if r != ' ' {
			placeChar(ctx, i, 0, r)
		}
	}

	// Test 4w - move through words and punctuation
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, 'w', 4)
	// First w: 0 -> 3 (',')
	// Second w: 3 -> 5 ('b')
	// Third w: 5 -> 8 (',')
	// Fourth w: 8 -> 10 ('b')
	if ctx.CursorX != 10 {
		t.Errorf("4w with punctuation: expected X=10 (at 'b' in 'baz'), got X=%d", ctx.CursorX)
	}

	// Test 6w - should reach end
	ctx.CursorX = 0
	ExecuteMotion(ctx, 'w', 6)
	// First w: 0 -> 3 (',')
	// Second w: 3 -> 5 ('b')
	// Third w: 5 -> 8 (',')
	// Fourth w: 8 -> 10 ('b')
	// Fifth w: 10 -> 13 (',')
	// Sixth w: 13 -> 15 ('q')
	if ctx.CursorX != 15 {
		t.Errorf("6w with punctuation: expected X=15 (at 'q' in 'qux'), got X=%d", ctx.CursorX)
	}

	// Test 10w - should stop at last word
	ctx.CursorX = 0
	ExecuteMotion(ctx, 'w', 10)
	// Should stop at 'q' in "qux" (can't move further)
	if ctx.CursorX != 15 {
		t.Errorf("10w with punctuation: expected X=15 (stopped at 'q'), got X=%d", ctx.CursorX)
	}
}

// Test count with simple motions (h, j, k, l, space)
func TestSimpleMotionCountHandling(t *testing.T) {
	ctx := createTestContext()

	// Test 5h from middle
	ctx.CursorX = 10
	ctx.CursorY = 5
	ExecuteMotion(ctx, 'h', 5)
	if ctx.CursorX != 5 {
		t.Errorf("5h motion: expected X=5, got X=%d", ctx.CursorX)
	}

	// Test 3j
	ctx.CursorY = 5
	ExecuteMotion(ctx, 'j', 3)
	if ctx.CursorY != 8 {
		t.Errorf("3j motion: expected Y=8, got Y=%d", ctx.CursorY)
	}

	// Test 4k
	ctx.CursorY = 10
	ExecuteMotion(ctx, 'k', 4)
	if ctx.CursorY != 6 {
		t.Errorf("4k motion: expected Y=6, got Y=%d", ctx.CursorY)
	}

	// Test 7l
	ctx.CursorX = 3
	ExecuteMotion(ctx, 'l', 7)
	if ctx.CursorX != 10 {
		t.Errorf("7l motion: expected X=10, got X=%d", ctx.CursorX)
	}

	// Test 5 space (should work like l)
	ctx.CursorX = 3
	ExecuteMotion(ctx, ' ', 5)
	if ctx.CursorX != 8 {
		t.Errorf("5 space motion: expected X=8, got X=%d", ctx.CursorX)
	}
}

// Test count of zero should default to 1
func TestMotionCountZeroDefaultsToOne(t *testing.T) {
	ctx := createTestContext()

	// Setup: "one two three" at y=0
	text := "one two three"
	for i, r := range text {
		if r != ' ' {
			placeChar(ctx, i, 0, r)
		}
	}

	// Test 0w (count=0) should behave like 1w
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, 'w', 0)
	if ctx.CursorX != 4 {
		t.Errorf("0w motion (should default to 1): expected X=4, got X=%d", ctx.CursorX)
	}

	// Test 0h should behave like 1h
	ctx.CursorX = 5
	ExecuteMotion(ctx, 'h', 0)
	if ctx.CursorX != 4 {
		t.Errorf("0h motion (should default to 1): expected X=4, got X=%d", ctx.CursorX)
	}
}

// Test WORD end motion edge cases
func TestWORDEndEdgeCases(t *testing.T) {
	ctx := createTestContext()

	// Setup: "foo bar" at y=0
	text := "foo bar"
	for i, r := range text {
		if r != ' ' {
			placeChar(ctx, i, 0, r)
		}
	}

	// Test 'E' from start of WORD
	ctx.CursorX = 0 // at 'f'
	ctx.CursorY = 0
	ExecuteMotion(ctx, 'E', 1)
	if ctx.CursorX != 2 {
		t.Errorf("E from WORD start: expected X=2 (at second 'o'), got X=%d", ctx.CursorX)
	}

	// Test 'E' again - should jump to end of "bar"
	ExecuteMotion(ctx, 'E', 1)
	if ctx.CursorX != 6 {
		t.Errorf("E to next WORD end: expected X=6 (at 'r'), got X=%d", ctx.CursorX)
	}

	// Test 'E' from end of WORD - should stay if no more WORDs
	ExecuteMotion(ctx, 'E', 1)
	if ctx.CursorX != 6 {
		t.Errorf("E at last WORD: expected X=6, got X=%d", ctx.CursorX)
	}
}

// Test repeated 'w' presses on "hello world test"
func TestRepeatedWPresses(t *testing.T) {
	ctx := createTestContext()

	// Setup: "hello world test" at y=0
	// Positions: h(0)e(1)l(2)l(3)o(4) (5) w(6)o(7)r(8)l(9)d(10) (11) t(12)e(13)s(14)t(15)
	text := "hello world test"
	for i, r := range text {
		if r != ' ' {
			placeChar(ctx, i, 0, r)
		}
	}

	// Start at 'h' (position 0)
	ctx.CursorX = 0
	ctx.CursorY = 0

	// First 'w' press - should move to 'w' in "world"
	ExecuteMotion(ctx, 'w', 1)
	if ctx.CursorX != 6 {
		t.Errorf("First w press: expected X=6 (at 'w' in 'world'), got X=%d", ctx.CursorX)
	}

	// Second 'w' press - should move to 't' in "test"
	ExecuteMotion(ctx, 'w', 1)
	if ctx.CursorX != 12 {
		t.Errorf("Second w press: expected X=12 (at 't' in 'test'), got X=%d", ctx.CursorX)
	}

	// Third 'w' press - should stay at 't' (no more words)
	startX := ctx.CursorX
	ExecuteMotion(ctx, 'w', 1)
	if ctx.CursorX != startX {
		t.Errorf("Third w press at end: expected X=%d (stay in place), got X=%d", startX, ctx.CursorX)
	}
}

// Test word motions with mixed content: "foo_bar, baz.qux! end"
func TestWordMotionsMixedContent(t *testing.T) {
	ctx := createTestContext()

	// Setup: "foo_bar, baz.qux! end" at y=0
	// Positions: f(0)o(1)o(2)_(3)b(4)a(5)r(6),(7) (8) b(9)a(10)z(11).(12)q(13)u(14)x(15)!(16) (17) e(18)n(19)d(20)
	text := "foo_bar, baz.qux! end"
	for i, r := range text {
		if r != ' ' {
			placeChar(ctx, i, 0, r)
		}
	}

	// Test 'w' from start - punctuation is treated as separate word
	ctx.CursorX = 0 // at 'f'
	ctx.CursorY = 0
	ExecuteMotion(ctx, 'w', 1)
	// Should jump to ',' (punctuation is a separate word in vim)
	if ctx.CursorX != 7 {
		t.Errorf("w to punctuation: expected X=7 (at ','), got X=%d", ctx.CursorX)
	}

	// Test 'w' from comma - should go to 'b' in "baz"
	ExecuteMotion(ctx, 'w', 1)
	if ctx.CursorX != 9 {
		t.Errorf("w from comma: expected X=9 (at 'b'), got X=%d", ctx.CursorX)
	}

	// Test 'w' from 'b' - should go to '.' (period)
	ExecuteMotion(ctx, 'w', 1)
	if ctx.CursorX != 12 {
		t.Errorf("w from baz: expected X=12 (at '.'), got X=%d", ctx.CursorX)
	}

	// Test 'w' from '.' - should go to 'q' (start of "qux")
	ExecuteMotion(ctx, 'w', 1)
	if ctx.CursorX != 13 {
		t.Errorf("w from period: expected X=13 (at 'q'), got X=%d", ctx.CursorX)
	}

	// Test 'w' from 'q' - should go to '!' (next punctuation after "qux")
	ExecuteMotion(ctx, 'w', 1)
	if ctx.CursorX != 16 {
		t.Errorf("w from q: expected X=16 (at '!'), got X=%d", ctx.CursorX)
	}

	// Test 'w' from '!' - should go to 'e' in "end"
	ExecuteMotion(ctx, 'w', 1)
	if ctx.CursorX != 18 {
		t.Errorf("w from exclamation: expected X=18 (at 'e'), got X=%d", ctx.CursorX)
	}

	// Test 'e' motion on mixed content
	ctx.CursorX = 0 // at 'f'
	ExecuteMotion(ctx, 'e', 1)
	if ctx.CursorX != 6 {
		t.Errorf("e from start: expected X=6 (at 'r'), got X=%d", ctx.CursorX)
	}

	// Test 'e' again - should go to comma (end of punctuation group)
	ExecuteMotion(ctx, 'e', 1)
	if ctx.CursorX != 7 {
		t.Errorf("e to comma: expected X=7 (at ','), got X=%d", ctx.CursorX)
	}

	// Test 'b' motion backward through mixed content
	ctx.CursorX = 18 // at 'e' in "end"
	ExecuteMotion(ctx, 'b', 1)
	if ctx.CursorX != 16 {
		t.Errorf("b from end: expected X=16 (at '!'), got X=%d", ctx.CursorX)
	}

	ExecuteMotion(ctx, 'b', 1)
	if ctx.CursorX != 13 {
		t.Errorf("b from !: expected X=13 (at 'q'), got X=%d", ctx.CursorX)
	}

	ExecuteMotion(ctx, 'b', 1)
	if ctx.CursorX != 12 {
		t.Errorf("b from q: expected X=12 (at '.'), got X=%d", ctx.CursorX)
	}
}

// Test WORD motions on mixed content: "foo_bar, baz.qux! end"
func TestWORDMotionsMixedContent(t *testing.T) {
	ctx := createTestContext()

	// Setup: "foo_bar, baz.qux! end" at y=0
	// Positions: f(0)o(1)o(2)_(3)b(4)a(5)r(6),(7) (8) b(9)a(10)z(11).(12)q(13)u(14)x(15)!(16) (17) e(18)n(19)d(20)
	text := "foo_bar, baz.qux! end"
	for i, r := range text {
		if r != ' ' {
			placeChar(ctx, i, 0, r)
		}
	}

	// Test 'W' from start - "foo_bar," is treated as one WORD
	ctx.CursorX = 0 // at 'f'
	ctx.CursorY = 0
	ExecuteMotion(ctx, 'W', 1)
	// Should jump to 'b' in "baz.qux!" (entire "foo_bar," is one WORD)
	if ctx.CursorX != 9 {
		t.Errorf("W to next WORD: expected X=9 (at 'b'), got X=%d", ctx.CursorX)
	}

	// Test 'W' again - should go to 'e' in "end"
	ExecuteMotion(ctx, 'W', 1)
	if ctx.CursorX != 18 {
		t.Errorf("W to end: expected X=18 (at 'e'), got X=%d", ctx.CursorX)
	}

	// Test 'E' motion - should find end of WORD "foo_bar," (the comma)
	ctx.CursorX = 0
	ExecuteMotion(ctx, 'E', 1)
	if ctx.CursorX != 7 {
		t.Errorf("E from start: expected X=7 (at ','), got X=%d", ctx.CursorX)
	}

	// Test 'E' again - should go to '!' at end of "baz.qux!"
	ExecuteMotion(ctx, 'E', 1)
	if ctx.CursorX != 16 {
		t.Errorf("E to next WORD end: expected X=16 (at '!'), got X=%d", ctx.CursorX)
	}

	// Test 'B' backward - should go back to 'b' in "baz.qux!"
	ctx.CursorX = 18 // at 'e' in "end"
	ExecuteMotion(ctx, 'B', 1)
	if ctx.CursorX != 9 {
		t.Errorf("B from end: expected X=9 (at 'b'), got X=%d", ctx.CursorX)
	}

	// Test 'B' again - should go back to 'f' in "foo_bar,"
	ExecuteMotion(ctx, 'B', 1)
	if ctx.CursorX != 0 {
		t.Errorf("B to start: expected X=0 (at 'f'), got X=%d", ctx.CursorX)
	}
}

// Test edge cases for word motions
func TestWordMotionsEdgeCases(t *testing.T) {
	ctx := createTestContext()

	// Test 1: Multiple consecutive spaces
	// Setup: "foo    bar" at y=0 (4 spaces between)
	// Positions: f(0)o(1)o(2) (3)(4)(5)(6) b(7)a(8)r(9)
	placeChar(ctx, 0, 0, 'f')
	placeChar(ctx, 1, 0, 'o')
	placeChar(ctx, 2, 0, 'o')
	placeChar(ctx, 7, 0, 'b')
	placeChar(ctx, 8, 0, 'a')
	placeChar(ctx, 9, 0, 'r')

	ctx.CursorX = 0
	ctx.CursorY = 0

	// 'w' should skip all spaces and land on 'b'
	ExecuteMotion(ctx, 'w', 1)
	if ctx.CursorX != 7 {
		t.Errorf("w over multiple spaces: expected X=7 (at 'b'), got X=%d", ctx.CursorX)
	}

	// 'b' should skip all spaces and land back on 'f'
	ExecuteMotion(ctx, 'b', 1)
	if ctx.CursorX != 0 {
		t.Errorf("b over multiple spaces: expected X=0 (at 'f'), got X=%d", ctx.CursorX)
	}

	// Test 2: Beginning of line - 'b' should stay at position 0
	ctx.CursorX = 0
	ExecuteMotion(ctx, 'b', 1)
	if ctx.CursorX != 0 {
		t.Errorf("b at beginning: expected X=0 (stay), got X=%d", ctx.CursorX)
	}

	// Test 3: End of line - 'w' should stay in place
	ctx.CursorX = 7 // at 'b' in "bar" (last word)
	startX := ctx.CursorX
	ExecuteMotion(ctx, 'w', 1)
	if ctx.CursorX != startX {
		t.Errorf("w at end: expected X=%d (stay), got X=%d", startX, ctx.CursorX)
	}

	// Test 4: Single character words/punctuation
	ctx = createTestContext()
	// Setup: "a b c" at y=0
	placeChar(ctx, 0, 0, 'a')
	placeChar(ctx, 2, 0, 'b')
	placeChar(ctx, 4, 0, 'c')

	ctx.CursorX = 0
	ctx.CursorY = 0

	ExecuteMotion(ctx, 'w', 1)
	if ctx.CursorX != 2 {
		t.Errorf("w over single chars: expected X=2 (at 'b'), got X=%d", ctx.CursorX)
	}

	ExecuteMotion(ctx, 'w', 1)
	if ctx.CursorX != 4 {
		t.Errorf("w to last single char: expected X=4 (at 'c'), got X=%d", ctx.CursorX)
	}

	// Test 5: Empty positions between characters (same as multiple spaces)
	ctx = createTestContext()
	// Setup: "x     y" (5 spaces)
	placeChar(ctx, 0, 0, 'x')
	placeChar(ctx, 6, 0, 'y')

	ctx.CursorX = 0
	ctx.CursorY = 0

	ExecuteMotion(ctx, 'w', 1)
	if ctx.CursorX != 6 {
		t.Errorf("w over many empty positions: expected X=6 (at 'y'), got X=%d", ctx.CursorX)
	}

	// Test 6: Starting from middle of multiple spaces
	ctx = createTestContext()
	// Setup: "foo    bar"
	placeChar(ctx, 0, 0, 'f')
	placeChar(ctx, 1, 0, 'o')
	placeChar(ctx, 2, 0, 'o')
	placeChar(ctx, 7, 0, 'b')
	placeChar(ctx, 8, 0, 'a')
	placeChar(ctx, 9, 0, 'r')

	ctx.CursorX = 4 // in the middle of spaces
	ctx.CursorY = 0

	ExecuteMotion(ctx, 'w', 1)
	if ctx.CursorX != 7 {
		t.Errorf("w from middle of spaces: expected X=7 (at 'b'), got X=%d", ctx.CursorX)
	}

	// Test 'b' from middle of spaces
	ctx.CursorX = 5 // in the middle of spaces
	ExecuteMotion(ctx, 'b', 1)
	if ctx.CursorX != 0 {
		t.Errorf("b from middle of spaces: expected X=0 (at 'f'), got X=%d", ctx.CursorX)
	}

	// Test 7: Consecutive punctuation marks
	ctx = createTestContext()
	// Setup: "word...more"
	text := "word...more"
	for i, r := range text {
		placeChar(ctx, i, 0, r)
	}

	ctx.CursorX = 0
	ctx.CursorY = 0

	ExecuteMotion(ctx, 'w', 1) // Should go to first '.'
	if ctx.CursorX != 4 {
		t.Errorf("w to consecutive punct: expected X=4 (at first '.'), got X=%d", ctx.CursorX)
	}

	ExecuteMotion(ctx, 'w', 1) // Should go to 'm' in "more"
	if ctx.CursorX != 7 {
		t.Errorf("w past consecutive punct: expected X=7 (at 'm'), got X=%d", ctx.CursorX)
	}
}

// Test WORD motions edge cases
func TestWORDMotionsEdgeCasesComprehensive(t *testing.T) {
	ctx := createTestContext()

	// Test 1: Multiple consecutive spaces with WORD motions
	// Setup: "foo    bar" at y=0 (4 spaces between)
	placeChar(ctx, 0, 0, 'f')
	placeChar(ctx, 1, 0, 'o')
	placeChar(ctx, 2, 0, 'o')
	placeChar(ctx, 7, 0, 'b')
	placeChar(ctx, 8, 0, 'a')
	placeChar(ctx, 9, 0, 'r')

	ctx.CursorX = 0
	ctx.CursorY = 0

	// 'W' should skip all spaces and land on 'b'
	ExecuteMotion(ctx, 'W', 1)
	if ctx.CursorX != 7 {
		t.Errorf("W over multiple spaces: expected X=7 (at 'b'), got X=%d", ctx.CursorX)
	}

	// 'B' should skip all spaces and land back on 'f'
	ExecuteMotion(ctx, 'B', 1)
	if ctx.CursorX != 0 {
		t.Errorf("B over multiple spaces: expected X=0 (at 'f'), got X=%d", ctx.CursorX)
	}

	// Test 2: Beginning of line - 'B' should stay at position 0
	ctx.CursorX = 0
	ExecuteMotion(ctx, 'B', 1)
	if ctx.CursorX != 0 {
		t.Errorf("B at beginning: expected X=0 (stay), got X=%d", ctx.CursorX)
	}

	// Test 3: End of line - 'W' should stay in place
	ctx.CursorX = 7 // at 'b' in "bar" (last WORD)
	startX := ctx.CursorX
	ExecuteMotion(ctx, 'W', 1)
	if ctx.CursorX != startX {
		t.Errorf("W at end: expected X=%d (stay), got X=%d", startX, ctx.CursorX)
	}

	// Test 4: Complex punctuation treated as single WORD
	ctx = createTestContext()
	// Setup: "a...b...c...d"
	text := "a...b...c...d"
	for i, r := range text {
		placeChar(ctx, i, 0, r)
	}

	ctx.CursorX = 0
	ctx.CursorY = 0

	// 'W' should treat all characters up to next space as one WORD
	// Since there are no spaces, should stay at 0 (already at the only WORD)
	startX = ctx.CursorX
	ExecuteMotion(ctx, 'W', 1)
	if ctx.CursorX != startX {
		t.Errorf("W on continuous non-spaces: expected X=%d (stay), got X=%d", startX, ctx.CursorX)
	}

	// Test 5: Starting from middle of multiple spaces with WORD
	ctx = createTestContext()
	// Setup: "foo    bar"
	placeChar(ctx, 0, 0, 'f')
	placeChar(ctx, 1, 0, 'o')
	placeChar(ctx, 2, 0, 'o')
	placeChar(ctx, 7, 0, 'b')
	placeChar(ctx, 8, 0, 'a')
	placeChar(ctx, 9, 0, 'r')

	ctx.CursorX = 4 // in the middle of spaces
	ctx.CursorY = 0

	ExecuteMotion(ctx, 'W', 1)
	if ctx.CursorX != 7 {
		t.Errorf("W from middle of spaces: expected X=7 (at 'b'), got X=%d", ctx.CursorX)
	}

	// Test 'B' from middle of spaces
	ctx.CursorX = 5 // in the middle of spaces
	ExecuteMotion(ctx, 'B', 1)
	if ctx.CursorX != 0 {
		t.Errorf("B from middle of spaces: expected X=0 (at 'f'), got X=%d", ctx.CursorX)
	}
}

// Test getCharAt with defensive space handling
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
	ctx.World.AddComponent(entity, components.PositionComponent{X: 15, Y: 15})
	ctx.World.AddComponent(entity, components.CharacterComponent{Rune: ' ', Style: tcell.StyleDefault})
	ctx.World.AddComponent(entity, components.SequenceComponent{
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

// TestWordMotionsWithFileContent tests word motions with file-based content placement
// This simulates how the game actually places characters from files - with position GAPS
// between words, not just skipping space characters in iteration.
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
// This verifies the cursor doesn't get stuck on gap positions (empty spaces)
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
// This simulates sparse content placement across the screen
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
// This simulates cursor being moved to an empty position (which can happen in the game)
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
// Verifies that WORD motions (space-delimited) also work correctly with position gaps
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

// Test % motion with simple parentheses
func TestBracketMatchingParentheses(t *testing.T) {
	ctx := createTestContext()

	// Setup: "(hello)" at y=0
	// Positions: ((0) h(1) e(2) l(3) l(4) o(5) )(6)
	placeChar(ctx, 0, 0, '(')
	placeChar(ctx, 1, 0, 'h')
	placeChar(ctx, 2, 0, 'e')
	placeChar(ctx, 3, 0, 'l')
	placeChar(ctx, 4, 0, 'l')
	placeChar(ctx, 5, 0, 'o')
	placeChar(ctx, 6, 0, ')')

	// Test % from opening parenthesis
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 6 || ctx.CursorY != 0 {
		t.Errorf("%% from '(': expected (6, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from closing parenthesis (should go back)
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 0 || ctx.CursorY != 0 {
		t.Errorf("%% from ')': expected (0, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}
}

// Test % motion with curly braces
func TestBracketMatchingCurlyBraces(t *testing.T) {
	ctx := createTestContext()

	// Setup: "{code}" at y=0
	placeChar(ctx, 0, 0, '{')
	placeChar(ctx, 1, 0, 'c')
	placeChar(ctx, 2, 0, 'o')
	placeChar(ctx, 3, 0, 'd')
	placeChar(ctx, 4, 0, 'e')
	placeChar(ctx, 5, 0, '}')

	// Test % from opening brace
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 5 || ctx.CursorY != 0 {
		t.Errorf("%% from '{': expected (5, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from closing brace
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 0 || ctx.CursorY != 0 {
		t.Errorf("%% from '}': expected (0, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}
}

// Test % motion with square brackets
func TestBracketMatchingSquareBrackets(t *testing.T) {
	ctx := createTestContext()

	// Setup: "[array]" at y=0
	placeChar(ctx, 0, 0, '[')
	placeChar(ctx, 1, 0, 'a')
	placeChar(ctx, 2, 0, 'r')
	placeChar(ctx, 3, 0, 'r')
	placeChar(ctx, 4, 0, 'a')
	placeChar(ctx, 5, 0, 'y')
	placeChar(ctx, 6, 0, ']')

	// Test % from opening bracket
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 6 || ctx.CursorY != 0 {
		t.Errorf("%% from '[': expected (6, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from closing bracket
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 0 || ctx.CursorY != 0 {
		t.Errorf("%% from ']': expected (0, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}
}

// Test % motion with nested brackets
func TestBracketMatchingNested(t *testing.T) {
	ctx := createTestContext()

	// Setup: "((a))" at y=0
	// Positions: ((0) ((1) a(2) )(3) )(4)
	placeChar(ctx, 0, 0, '(')
	placeChar(ctx, 1, 0, '(')
	placeChar(ctx, 2, 0, 'a')
	placeChar(ctx, 3, 0, ')')
	placeChar(ctx, 4, 0, ')')

	// Test % from first opening parenthesis - should match last closing
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 4 || ctx.CursorY != 0 {
		t.Errorf("%% from outer '(': expected (4, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from second opening parenthesis - should match first closing
	ctx.CursorX = 1
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 3 || ctx.CursorY != 0 {
		t.Errorf("%% from inner '(': expected (3, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from first closing parenthesis - should match second opening
	ctx.CursorX = 3
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 1 || ctx.CursorY != 0 {
		t.Errorf("%% from first ')': expected (1, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from last closing parenthesis - should match first opening
	ctx.CursorX = 4
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 0 || ctx.CursorY != 0 {
		t.Errorf("%% from outer ')': expected (0, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}
}

// Test % motion with multi-line brackets
func TestBracketMatchingMultiLine(t *testing.T) {
	ctx := createTestContext()

	// Setup: Opening brace at (0,0), closing brace at (0,3)
	// Line 0: {
	// Line 1: code
	// Line 2: here
	// Line 3: }
	placeChar(ctx, 0, 0, '{')
	placeChar(ctx, 0, 1, 'c')
	placeChar(ctx, 1, 1, 'o')
	placeChar(ctx, 2, 1, 'd')
	placeChar(ctx, 3, 1, 'e')
	placeChar(ctx, 0, 2, 'h')
	placeChar(ctx, 1, 2, 'e')
	placeChar(ctx, 2, 2, 'r')
	placeChar(ctx, 3, 2, 'e')
	placeChar(ctx, 0, 3, '}')

	// Test % from opening brace on line 0
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 0 || ctx.CursorY != 3 {
		t.Errorf("%% from '{' at line 0: expected (0, 3), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from closing brace on line 3
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 0 || ctx.CursorY != 0 {
		t.Errorf("%% from '}' at line 3: expected (0, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}
}

// Test % motion with mixed bracket types (should only match same type)
func TestBracketMatchingMixedTypes(t *testing.T) {
	ctx := createTestContext()

	// Setup: "[{()}]" at y=0
	// Positions: [(0) {(1) ((2) )(3) }(4) ](5)
	placeChar(ctx, 0, 0, '[')
	placeChar(ctx, 1, 0, '{')
	placeChar(ctx, 2, 0, '(')
	placeChar(ctx, 3, 0, ')')
	placeChar(ctx, 4, 0, '}')
	placeChar(ctx, 5, 0, ']')

	// Test % from '[' - should match ']'
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 5 || ctx.CursorY != 0 {
		t.Errorf("%% from '[': expected (5, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from '{' - should match '}'
	ctx.CursorX = 1
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 4 || ctx.CursorY != 0 {
		t.Errorf("%% from '{': expected (4, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from '(' - should match ')'
	ctx.CursorX = 2
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 3 || ctx.CursorY != 0 {
		t.Errorf("%% from '(': expected (3, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}
}

// Test % motion when cursor is not on a bracket (should stay in place)
func TestBracketMatchingNonBracket(t *testing.T) {
	ctx := createTestContext()

	// Setup: "hello" at y=0
	placeChar(ctx, 0, 0, 'h')
	placeChar(ctx, 1, 0, 'e')
	placeChar(ctx, 2, 0, 'l')
	placeChar(ctx, 3, 0, 'l')
	placeChar(ctx, 4, 0, 'o')

	// Test % from non-bracket character - should stay in place
	ctx.CursorX = 2
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 2 || ctx.CursorY != 0 {
		t.Errorf("%% from non-bracket: expected (2, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}
}

// Test % motion when no matching bracket exists (should stay in place)
func TestBracketMatchingNoMatch(t *testing.T) {
	ctx := createTestContext()

	// Setup: "(" at (0,0) with no closing parenthesis
	placeChar(ctx, 0, 0, '(')
	placeChar(ctx, 1, 0, 'a')
	placeChar(ctx, 2, 0, 'b')
	placeChar(ctx, 3, 0, 'c')

	// Test % from opening parenthesis with no match - should stay in place
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 0 || ctx.CursorY != 0 {
		t.Errorf("%% from unmatched '(': expected (0, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Setup: ")" at (5,0) with no opening parenthesis
	ctx = createTestContext()
	placeChar(ctx, 5, 0, ')')
	placeChar(ctx, 6, 0, 'x')

	ctx.CursorX = 5
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 5 || ctx.CursorY != 0 {
		t.Errorf("%% from unmatched ')': expected (5, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}
}

// Test % motion with deeply nested brackets
func TestBracketMatchingDeeplyNested(t *testing.T) {
	ctx := createTestContext()

	// Setup: "((((a))))" at y=0
	// Positions: ((0) ((1) ((2) ((3) a(4) )(5) )(6) )(7) )(8)
	text := "((((a))))"
	for i, r := range text {
		placeChar(ctx, i, 0, r)
	}

	// Test % from outermost opening bracket
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 8 || ctx.CursorY != 0 {
		t.Errorf("%% from outermost '(': expected (8, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from innermost opening bracket
	ctx.CursorX = 3
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 5 || ctx.CursorY != 0 {
		t.Errorf("%% from innermost '(': expected (5, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from innermost closing bracket
	ctx.CursorX = 5
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 3 || ctx.CursorY != 0 {
		t.Errorf("%% from innermost ')': expected (3, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from outermost closing bracket
	ctx.CursorX = 8
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 0 || ctx.CursorY != 0 {
		t.Errorf("%% from outermost ')': expected (0, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}
}

// Test % motion with complex multi-line code structure
func TestBracketMatchingComplexMultiLine(t *testing.T) {
	ctx := createTestContext()

	// Simulate a function definition across multiple lines:
	// Line 0: func test() {
	// Line 1:   if (x > 0) {
	// Line 2:     return x
	// Line 3:   }
	// Line 4: }

	// Line 0: func test() {
	text0 := "func test() {"
	for i, r := range text0 {
		if r != ' ' {
			placeChar(ctx, i, 0, r)
		}
	}

	// Line 1:   if (x > 0) {
	text1 := "  if (x > 0) {"
	for i, r := range text1 {
		if r != ' ' {
			placeChar(ctx, i, 1, r)
		}
	}

	// Line 2:     return x
	text2 := "    return x"
	for i, r := range text2 {
		if r != ' ' {
			placeChar(ctx, i, 2, r)
		}
	}

	// Line 3:   }
	placeChar(ctx, 2, 3, '}')

	// Line 4: }
	placeChar(ctx, 0, 4, '}')

	// Test % from opening brace on line 0 - should match closing brace on line 4
	ctx.CursorX = 12 // Position of '{' in "func test() {"
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 0 || ctx.CursorY != 4 {
		t.Errorf("%% from outer '{' at line 0: expected (0, 4), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from opening parenthesis on line 1
	ctx.CursorX = 5 // Position of '(' in "if (x > 0)"
	ctx.CursorY = 1
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 11 || ctx.CursorY != 1 {
		t.Errorf("%% from '(' at line 1: expected (11, 1), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from opening brace on line 1 - should match closing brace on line 3
	ctx.CursorX = 13 // Position of '{' at end of line 1
	ctx.CursorY = 1
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 2 || ctx.CursorY != 3 {
		t.Errorf("%% from inner '{' at line 1: expected (2, 3), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}
}

// Test % motion with brackets separated by large gaps
func TestBracketMatchingWithGaps(t *testing.T) {
	ctx := createTestContext()

	// Setup: '(' at (0,0), ')' at (50,5) with large gap
	placeChar(ctx, 0, 0, '(')
	placeChar(ctx, 50, 5, ')')

	// Test % from opening parenthesis - should find closing across gap
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 50 || ctx.CursorY != 5 {
		t.Errorf("%% from '(' with gap: expected (50, 5), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from closing parenthesis - should find opening across gap
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 0 || ctx.CursorY != 0 {
		t.Errorf("%% from ')' with gap: expected (0, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}
}

// Test % motion with mismatched bracket types (should not match)
func TestBracketMatchingMismatchedTypes(t *testing.T) {
	ctx := createTestContext()

	// Setup: "( ]" at y=0 - mismatched types
	placeChar(ctx, 0, 0, '(')
	placeChar(ctx, 1, 0, 'x')
	placeChar(ctx, 2, 0, ']')

	// Test % from '(' - should not match ']', should stay in place
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 0 || ctx.CursorY != 0 {
		t.Errorf("%% from '(' with mismatched ']': expected (0, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Setup: "[ }" at y=1 - mismatched types
	placeChar(ctx, 0, 1, '[')
	placeChar(ctx, 1, 1, 'y')
	placeChar(ctx, 2, 1, '}')

	// Test % from '[' - should not match '}', should stay in place
	ctx.CursorX = 0
	ctx.CursorY = 1
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 0 || ctx.CursorY != 1 {
		t.Errorf("%% from '[' with mismatched '}': expected (0, 1), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}
}

// Test % motion with angle brackets
func TestBracketMatchingAngleBrackets(t *testing.T) {
	ctx := createTestContext()

	// Setup: "<template>" at y=0
	placeChar(ctx, 0, 0, '<')
	placeChar(ctx, 1, 0, 't')
	placeChar(ctx, 2, 0, 'e')
	placeChar(ctx, 3, 0, 'm')
	placeChar(ctx, 4, 0, 'p')
	placeChar(ctx, 5, 0, 'l')
	placeChar(ctx, 6, 0, 'a')
	placeChar(ctx, 7, 0, 't')
	placeChar(ctx, 8, 0, 'e')
	placeChar(ctx, 9, 0, '>')

	// Test % from opening angle bracket
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 9 || ctx.CursorY != 0 {
		t.Errorf("%% from '<': expected (9, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from closing angle bracket
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 0 || ctx.CursorY != 0 {
		t.Errorf("%% from '>': expected (0, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}
}

// Test % motion with nested angle brackets
func TestBracketMatchingNestedAngleBrackets(t *testing.T) {
	ctx := createTestContext()

	// Setup: "<<a>>" at y=0
	// Positions: <(0) <(1) a(2) >(3) >(4)
	placeChar(ctx, 0, 0, '<')
	placeChar(ctx, 1, 0, '<')
	placeChar(ctx, 2, 0, 'a')
	placeChar(ctx, 3, 0, '>')
	placeChar(ctx, 4, 0, '>')

	// Test % from first opening angle bracket - should match last closing
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 4 || ctx.CursorY != 0 {
		t.Errorf("%% from outer '<': expected (4, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from second opening angle bracket - should match first closing
	ctx.CursorX = 1
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 3 || ctx.CursorY != 0 {
		t.Errorf("%% from inner '<': expected (3, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from first closing angle bracket - should match second opening
	ctx.CursorX = 3
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 1 || ctx.CursorY != 0 {
		t.Errorf("%% from first '>': expected (1, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from last closing angle bracket - should match first opening
	ctx.CursorX = 4
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 0 || ctx.CursorY != 0 {
		t.Errorf("%% from outer '>': expected (0, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}
}

// Test % motion with mixed bracket types including angle brackets
func TestBracketMatchingMixedTypesWithAngles(t *testing.T) {
	ctx := createTestContext()

	// Setup: "[{<()>}]" at y=0
	// Positions: [(0) {(1) <(2) ((3) )(4) >(5) }(6) ](7)
	placeChar(ctx, 0, 0, '[')
	placeChar(ctx, 1, 0, '{')
	placeChar(ctx, 2, 0, '<')
	placeChar(ctx, 3, 0, '(')
	placeChar(ctx, 4, 0, ')')
	placeChar(ctx, 5, 0, '>')
	placeChar(ctx, 6, 0, '}')
	placeChar(ctx, 7, 0, ']')

	// Test % from '[' - should match ']'
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 7 || ctx.CursorY != 0 {
		t.Errorf("%% from '[': expected (7, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from '{' - should match '}'
	ctx.CursorX = 1
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 6 || ctx.CursorY != 0 {
		t.Errorf("%% from '{': expected (6, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from '<' - should match '>'
	ctx.CursorX = 2
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 5 || ctx.CursorY != 0 {
		t.Errorf("%% from '<': expected (5, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from '(' - should match ')'
	ctx.CursorX = 3
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 4 || ctx.CursorY != 0 {
		t.Errorf("%% from '(': expected (4, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}
}

// Test % motion with unmatched angle brackets
func TestBracketMatchingUnmatchedAngleBrackets(t *testing.T) {
	ctx := createTestContext()

	// Setup: "<" at (0,0) with no closing angle bracket
	placeChar(ctx, 0, 0, '<')
	placeChar(ctx, 1, 0, 'a')
	placeChar(ctx, 2, 0, 'b')
	placeChar(ctx, 3, 0, 'c')

	// Test % from opening angle bracket with no match - should stay in place
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 0 || ctx.CursorY != 0 {
		t.Errorf("%% from unmatched '<': expected (0, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Setup: ">" at (5,0) with no opening angle bracket
	ctx = createTestContext()
	placeChar(ctx, 5, 0, '>')
	placeChar(ctx, 6, 0, 'x')

	ctx.CursorX = 5
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 5 || ctx.CursorY != 0 {
		t.Errorf("%% from unmatched '>': expected (5, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}
}

// Test % motion with angle brackets across multiple lines
func TestBracketMatchingAngleBracketsMultiLine(t *testing.T) {
	ctx := createTestContext()

	// Setup: Opening angle bracket at (0,0), closing angle bracket at (0,3)
	// Line 0: <
	// Line 1: template
	// Line 2: code
	// Line 3: >
	placeChar(ctx, 0, 0, '<')
	placeChar(ctx, 0, 1, 't')
	placeChar(ctx, 1, 1, 'e')
	placeChar(ctx, 2, 1, 'm')
	placeChar(ctx, 3, 1, 'p')
	placeChar(ctx, 0, 2, 'c')
	placeChar(ctx, 1, 2, 'o')
	placeChar(ctx, 2, 2, 'd')
	placeChar(ctx, 3, 2, 'e')
	placeChar(ctx, 0, 3, '>')

	// Test % from opening angle bracket on line 0
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 0 || ctx.CursorY != 3 {
		t.Errorf("%% from '<' at line 0: expected (0, 3), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Test % from closing angle bracket on line 3
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 0 || ctx.CursorY != 0 {
		t.Errorf("%% from '>' at line 3: expected (0, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}
}

// Test % motion with mismatched angle bracket types
func TestBracketMatchingMismatchedAngleBrackets(t *testing.T) {
	ctx := createTestContext()

	// Setup: "< ]" at y=0 - mismatched types
	placeChar(ctx, 0, 0, '<')
	placeChar(ctx, 1, 0, 'x')
	placeChar(ctx, 2, 0, ']')

	// Test % from '<' - should not match ']', should stay in place
	ctx.CursorX = 0
	ctx.CursorY = 0
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 0 || ctx.CursorY != 0 {
		t.Errorf("%% from '<' with mismatched ']': expected (0, 0), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}

	// Setup: "< }" at y=1 - mismatched types
	placeChar(ctx, 0, 1, '<')
	placeChar(ctx, 1, 1, 'y')
	placeChar(ctx, 2, 1, '}')

	// Test % from '<' - should not match '}', should stay in place
	ctx.CursorX = 0
	ctx.CursorY = 1
	ExecuteMotion(ctx, '%', 1)
	if ctx.CursorX != 0 || ctx.CursorY != 1 {
		t.Errorf("%% from '<' with mismatched '}': expected (0, 1), got (%d, %d)", ctx.CursorX, ctx.CursorY)
	}
}
