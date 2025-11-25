package modes

import (
	"testing"
)

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
	setCursorPosition(ctx, 0, getCursorY(ctx)) // at 'f' in "foo_bar"
	setCursorPosition(ctx, getCursorX(ctx), 0)
	ExecuteMotion(ctx, 'w', 1)
	// Should jump to ',' (punctuation is a separate word in vim)
	if getCursorX(ctx) != 7 {
		t.Errorf("w motion failed: expected X=7 (at ','), got X=%d", getCursorX(ctx))
	}

	// Test 'w' again - should jump to 'b' in "baz"
	ExecuteMotion(ctx, 'w', 1)
	if getCursorX(ctx) != 9 {
		t.Errorf("w motion failed: expected X=9 (at 'b'), got X=%d", getCursorX(ctx))
	}

	// Test 'e' - should jump to end of word
	setCursorPosition(ctx, 0, getCursorY(ctx)) // at 'f' in "foo_bar"
	ExecuteMotion(ctx, 'e', 1)
	if getCursorX(ctx) != 6 {
		t.Errorf("e motion failed: expected X=6 (at 'r'), got X=%d", getCursorX(ctx))
	}

	// Test 'b' - should jump to start of previous word
	setCursorPosition(ctx, 9, getCursorY(ctx)) // at 'b' in "baz"
	ExecuteMotion(ctx, 'b', 1)
	// Should jump to ',' at position 7 (comma is a separate punctuation word)
	if getCursorX(ctx) != 7 {
		t.Errorf("b motion failed: expected X=7 (at ','), got X=%d", getCursorX(ctx))
	}
}

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
	setCursorPosition(ctx, 3, getCursorY(ctx)) // On first space after "foo"
	setCursorPosition(ctx, getCursorX(ctx), 0)
	ExecuteMotion(ctx, 'w', 1)
	if getCursorX(ctx) != 6 {
		t.Errorf("w from space: expected X=6 (at 'b'), got X=%d", getCursorX(ctx))
	}

	// Test 'w' starting from middle of spaces
	setCursorPosition(ctx, 4, getCursorY(ctx)) // On second space
	ExecuteMotion(ctx, 'w', 1)
	if getCursorX(ctx) != 6 {
		t.Errorf("w from middle space: expected X=6 (at 'b'), got X=%d", getCursorX(ctx))
	}

	// Test 'b' starting from space - should jump to previous word start
	setCursorPosition(ctx, 4, getCursorY(ctx)) // On space
	ExecuteMotion(ctx, 'b', 1)
	if getCursorX(ctx) != 0 {
		t.Errorf("b from space: expected X=0 (at 'f'), got X=%d", getCursorX(ctx))
	}
}

func TestVimWordMotionsPunctuationTransitions(t *testing.T) {
	ctx := createTestContext()

	// Setup: "word...next" at y=0
	// Positions: w(0)o(1)r(2)d(3).(4).(5).(6)n(7)e(8)x(9)t(10)
	text := "word...next"
	for i, r := range text {
		placeChar(ctx, i, 0, r)
	}

	// Test 'w' from word to punctuation
	setCursorPosition(ctx, 0, getCursorY(ctx)) // at 'w'
	setCursorPosition(ctx, getCursorX(ctx), 0)
	ExecuteMotion(ctx, 'w', 1)
	if getCursorX(ctx) != 4 {
		t.Errorf("w word->punct: expected X=4 (at first '.'), got X=%d", getCursorX(ctx))
	}

	// Test 'w' from punctuation to punctuation (should skip as one group)
	setCursorPosition(ctx, 4, getCursorY(ctx)) // at first '.'
	ExecuteMotion(ctx, 'w', 1)
	if getCursorX(ctx) != 7 {
		t.Errorf("w punct->word: expected X=7 (at 'n'), got X=%d", getCursorX(ctx))
	}

	// Test 'e' from word through punctuation
	setCursorPosition(ctx, 0, getCursorY(ctx)) // at 'w'
	ExecuteMotion(ctx, 'e', 1)
	if getCursorX(ctx) != 3 {
		t.Errorf("e word end: expected X=3 (at 'd'), got X=%d", getCursorX(ctx))
	}

	// Test 'e' from end of word (should jump to end of next word)
	ExecuteMotion(ctx, 'e', 1)
	if getCursorX(ctx) != 6 {
		t.Errorf("e from word end: expected X=6 (at last '.'), got X=%d", getCursorX(ctx))
	}

	// Test 'b' from word to punctuation
	setCursorPosition(ctx, 7, getCursorY(ctx)) // at 'n'
	ExecuteMotion(ctx, 'b', 1)
	if getCursorX(ctx) != 4 {
		t.Errorf("b word->punct: expected X=4 (at first '.'), got X=%d", getCursorX(ctx))
	}

	// Test 'b' from punctuation to word
	ExecuteMotion(ctx, 'b', 1)
	if getCursorX(ctx) != 0 {
		t.Errorf("b punct->word: expected X=0 (at 'w'), got X=%d", getCursorX(ctx))
	}
}

func TestVimWordMotionsBoundaries(t *testing.T) {
	ctx := createTestContext()

	// Setup: "word" at the very end of screen width
	endPos := ctx.GameWidth - 4
	placeChar(ctx, endPos, 0, 'w')
	placeChar(ctx, endPos+1, 0, 'o')
	placeChar(ctx, endPos+2, 0, 'r')
	placeChar(ctx, endPos+3, 0, 'd')

	// Test 'w' near right edge - should not move past boundary
	setCursorPosition(ctx, endPos, getCursorY(ctx))
	setCursorPosition(ctx, getCursorX(ctx), 0)
	startX := getCursorX(ctx)
	ExecuteMotion(ctx, 'w', 1)
	// Since we're at the last word, should stay in place
	if getCursorX(ctx) != startX {
		t.Errorf("w at right edge: expected X=%d, got X=%d", startX, getCursorX(ctx))
	}

	// Test 'e' at right edge
	setCursorPosition(ctx, endPos, getCursorY(ctx))
	ExecuteMotion(ctx, 'e', 1)
	if getCursorX(ctx) != endPos+3 {
		t.Errorf("e to word end: expected X=%d, got X=%d", endPos+3, getCursorX(ctx))
	}

	// Setup: "word" at the very beginning
	ctx = createTestContext()
	placeChar(ctx, 0, 0, 'w')
	placeChar(ctx, 1, 0, 'o')
	placeChar(ctx, 2, 0, 'r')
	placeChar(ctx, 3, 0, 'd')

	// Test 'b' at left edge - should stay in place
	setCursorPosition(ctx, 0, 0)
	ExecuteMotion(ctx, 'b', 1)
	if getCursorX(ctx) != 0 {
		t.Errorf("b at left edge: expected X=0, got X=%d", getCursorX(ctx))
	}
}

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
	setCursorPosition(ctx, 0, 0)

	ExecuteMotion(ctx, 'w', 1) // Should go to position 4 ('t' in "two")
	if getCursorX(ctx) != 4 {
		t.Errorf("First w: expected X=4, got X=%d", getCursorX(ctx))
	}

	ExecuteMotion(ctx, 'w', 1) // Should go to position 7 (',')
	if getCursorX(ctx) != 7 {
		t.Errorf("Second w: expected X=7, got X=%d", getCursorX(ctx))
	}

	ExecuteMotion(ctx, 'w', 1) // Should go to position 8 ('t' in "three")
	if getCursorX(ctx) != 8 {
		t.Errorf("Third w: expected X=8, got X=%d", getCursorX(ctx))
	}

	// Test multiple 'b' commands (reverse)
	ExecuteMotion(ctx, 'b', 1) // Should go back to 7 (',')
	if getCursorX(ctx) != 7 {
		t.Errorf("First b: expected X=7, got X=%d", getCursorX(ctx))
	}

	ExecuteMotion(ctx, 'b', 1) // Should go back to 4 ('t' in "two")
	if getCursorX(ctx) != 4 {
		t.Errorf("Second b: expected X=4, got X=%d", getCursorX(ctx))
	}

	ExecuteMotion(ctx, 'b', 1) // Should go back to 0 ('o' in "one")
	if getCursorX(ctx) != 0 {
		t.Errorf("Third b: expected X=0, got X=%d", getCursorX(ctx))
	}
}

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
	setCursorPosition(ctx, 0, 0)

	// First 'w' press - should move to 'w' in "world"
	ExecuteMotion(ctx, 'w', 1)
	if getCursorX(ctx) != 6 {
		t.Errorf("First w press: expected X=6 (at 'w' in 'world'), got X=%d", getCursorX(ctx))
	}

	// Second 'w' press - should move to 't' in "test"
	ExecuteMotion(ctx, 'w', 1)
	if getCursorX(ctx) != 12 {
		t.Errorf("Second w press: expected X=12 (at 't' in 'test'), got X=%d", getCursorX(ctx))
	}

	// Third 'w' press - should stay at 't' (no more words)
	startX := getCursorX(ctx)
	ExecuteMotion(ctx, 'w', 1)
	if getCursorX(ctx) != startX {
		t.Errorf("Third w press at end: expected X=%d (stay in place), got X=%d", startX, getCursorX(ctx))
	}
}

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
	setCursorPosition(ctx, 0, getCursorY(ctx)) // at 'f'
	setCursorPosition(ctx, getCursorX(ctx), 0)
	ExecuteMotion(ctx, 'w', 1)
	// Should jump to ',' (punctuation is a separate word in vim)
	if getCursorX(ctx) != 7 {
		t.Errorf("w to punctuation: expected X=7 (at ','), got X=%d", getCursorX(ctx))
	}

	// Test 'w' from comma - should go to 'b' in "baz"
	ExecuteMotion(ctx, 'w', 1)
	if getCursorX(ctx) != 9 {
		t.Errorf("w from comma: expected X=9 (at 'b'), got X=%d", getCursorX(ctx))
	}

	// Test 'w' from 'b' - should go to '.' (period)
	ExecuteMotion(ctx, 'w', 1)
	if getCursorX(ctx) != 12 {
		t.Errorf("w from baz: expected X=12 (at '.'), got X=%d", getCursorX(ctx))
	}

	// Test 'w' from '.' - should go to 'q' (start of "qux")
	ExecuteMotion(ctx, 'w', 1)
	if getCursorX(ctx) != 13 {
		t.Errorf("w from period: expected X=13 (at 'q'), got X=%d", getCursorX(ctx))
	}

	// Test 'w' from 'q' - should go to '!' (next punctuation after "qux")
	ExecuteMotion(ctx, 'w', 1)
	if getCursorX(ctx) != 16 {
		t.Errorf("w from q: expected X=16 (at '!'), got X=%d", getCursorX(ctx))
	}

	// Test 'w' from '!' - should go to 'e' in "end"
	ExecuteMotion(ctx, 'w', 1)
	if getCursorX(ctx) != 18 {
		t.Errorf("w from exclamation: expected X=18 (at 'e'), got X=%d", getCursorX(ctx))
	}

	// Test 'e' motion on mixed content
	setCursorPosition(ctx, 0, getCursorY(ctx)) // at 'f'
	ExecuteMotion(ctx, 'e', 1)
	if getCursorX(ctx) != 6 {
		t.Errorf("e from start: expected X=6 (at 'r'), got X=%d", getCursorX(ctx))
	}

	// Test 'e' again - should go to comma (end of punctuation group)
	ExecuteMotion(ctx, 'e', 1)
	if getCursorX(ctx) != 7 {
		t.Errorf("e to comma: expected X=7 (at ','), got X=%d", getCursorX(ctx))
	}

	// Test 'b' motion backward through mixed content
	setCursorPosition(ctx, 18, getCursorY(ctx)) // at 'e' in "end"
	ExecuteMotion(ctx, 'b', 1)
	if getCursorX(ctx) != 16 {
		t.Errorf("b from end: expected X=16 (at '!'), got X=%d", getCursorX(ctx))
	}

	ExecuteMotion(ctx, 'b', 1)
	if getCursorX(ctx) != 13 {
		t.Errorf("b from !: expected X=13 (at 'q'), got X=%d", getCursorX(ctx))
	}

	ExecuteMotion(ctx, 'b', 1)
	if getCursorX(ctx) != 12 {
		t.Errorf("b from q: expected X=12 (at '.'), got X=%d", getCursorX(ctx))
	}
}

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

	setCursorPosition(ctx, 0, 0)

	// 'w' should skip all spaces and land on 'b'
	ExecuteMotion(ctx, 'w', 1)
	if getCursorX(ctx) != 7 {
		t.Errorf("w over multiple spaces: expected X=7 (at 'b'), got X=%d", getCursorX(ctx))
	}

	// 'b' should skip all spaces and land back on 'f'
	ExecuteMotion(ctx, 'b', 1)
	if getCursorX(ctx) != 0 {
		t.Errorf("b over multiple spaces: expected X=0 (at 'f'), got X=%d", getCursorX(ctx))
	}

	// Test 2: Beginning of line - 'b' should stay at position 0
	setCursorPosition(ctx, 0, getCursorY(ctx))
	ExecuteMotion(ctx, 'b', 1)
	if getCursorX(ctx) != 0 {
		t.Errorf("b at beginning: expected X=0 (stay), got X=%d", getCursorX(ctx))
	}

	// Test 3: End of line - 'w' should stay in place
	setCursorPosition(ctx, 7, getCursorY(ctx)) // at 'b' in "bar" (last word)
	startX := getCursorX(ctx)
	ExecuteMotion(ctx, 'w', 1)
	if getCursorX(ctx) != startX {
		t.Errorf("w at end: expected X=%d (stay), got X=%d", startX, getCursorX(ctx))
	}

	// Test 4: Single character words/punctuation
	ctx = createTestContext()
	// Setup: "a b c" at y=0
	placeChar(ctx, 0, 0, 'a')
	placeChar(ctx, 2, 0, 'b')
	placeChar(ctx, 4, 0, 'c')

	setCursorPosition(ctx, 0, 0)

	ExecuteMotion(ctx, 'w', 1)
	if getCursorX(ctx) != 2 {
		t.Errorf("w over single chars: expected X=2 (at 'b'), got X=%d", getCursorX(ctx))
	}

	ExecuteMotion(ctx, 'w', 1)
	if getCursorX(ctx) != 4 {
		t.Errorf("w to last single char: expected X=4 (at 'c'), got X=%d", getCursorX(ctx))
	}

	// Test 5: Empty positions between characters (same as multiple spaces)
	ctx = createTestContext()
	// Setup: "x     y" (5 spaces)
	placeChar(ctx, 0, 0, 'x')
	placeChar(ctx, 6, 0, 'y')

	setCursorPosition(ctx, 0, 0)

	ExecuteMotion(ctx, 'w', 1)
	if getCursorX(ctx) != 6 {
		t.Errorf("w over many empty positions: expected X=6 (at 'y'), got X=%d", getCursorX(ctx))
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

	setCursorPosition(ctx, 4, getCursorY(ctx)) // in the middle of spaces
	setCursorPosition(ctx, getCursorX(ctx), 0)

	ExecuteMotion(ctx, 'w', 1)
	if getCursorX(ctx) != 7 {
		t.Errorf("w from middle of spaces: expected X=7 (at 'b'), got X=%d", getCursorX(ctx))
	}

	// Test 'b' from middle of spaces
	setCursorPosition(ctx, 5, getCursorY(ctx)) // in the middle of spaces
	ExecuteMotion(ctx, 'b', 1)
	if getCursorX(ctx) != 0 {
		t.Errorf("b from middle of spaces: expected X=0 (at 'f'), got X=%d", getCursorX(ctx))
	}

	// Test 7: Consecutive punctuation marks
	ctx = createTestContext()
	// Setup: "word...more"
	text := "word...more"
	for i, r := range text {
		placeChar(ctx, i, 0, r)
	}

	setCursorPosition(ctx, 0, 0)

	ExecuteMotion(ctx, 'w', 1) // Should go to first '.'
	if getCursorX(ctx) != 4 {
		t.Errorf("w to consecutive punct: expected X=4 (at first '.'), got X=%d", getCursorX(ctx))
	}

	ExecuteMotion(ctx, 'w', 1) // Should go to 'm' in "more"
	if getCursorX(ctx) != 7 {
		t.Errorf("w past consecutive punct: expected X=7 (at 'm'), got X=%d", getCursorX(ctx))
	}
}
