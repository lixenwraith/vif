package modes

import (
	"testing"
)



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

