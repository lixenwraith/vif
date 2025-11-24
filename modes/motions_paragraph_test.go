package modes

import (
	"testing"
)

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
