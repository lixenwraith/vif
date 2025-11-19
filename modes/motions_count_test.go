package modes

import (
	"testing"
)



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
