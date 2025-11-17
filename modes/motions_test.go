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
