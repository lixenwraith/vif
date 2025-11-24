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
