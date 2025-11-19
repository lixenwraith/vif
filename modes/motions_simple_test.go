package modes

import (
	"testing"
)



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

