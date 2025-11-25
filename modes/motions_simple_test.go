package modes

import (
	"testing"
)

func TestSpaceMotion(t *testing.T) {
	ctx := createTestContext()
	setCursorPosition(ctx, 5, 5)

	// Test space - should move right like 'l'
	ExecuteMotion(ctx, ' ', 1)
	if getCursorX(ctx) != 6 {
		t.Errorf("SPACE motion failed: expected X=6, got X=%d", getCursorX(ctx))
	}

	// Test space with count
	setCursorPosition(ctx, 5, getCursorY(ctx))
	ExecuteMotion(ctx, ' ', 3)
	if getCursorX(ctx) != 8 {
		t.Errorf("SPACE motion with count failed: expected X=8, got X=%d", getCursorX(ctx))
	}

	// Test space at right edge - should stay at edge
	setCursorPosition(ctx, ctx.GameWidth-1, getCursorY(ctx))
	ExecuteMotion(ctx, ' ', 1)
	if getCursorX(ctx) != ctx.GameWidth-1 {
		t.Errorf("SPACE motion at edge: expected X=%d, got X=%d", ctx.GameWidth-1, getCursorX(ctx))
	}
}

func TestSimpleMotionCountHandling(t *testing.T) {
	ctx := createTestContext()

	// Test 5h from middle
	setCursorPosition(ctx, 10, 5)
	ExecuteMotion(ctx, 'h', 5)
	if getCursorX(ctx) != 5 {
		t.Errorf("5h motion: expected X=5, got X=%d", getCursorX(ctx))
	}

	// Test 3j
	setCursorPosition(ctx, getCursorX(ctx), 5)
	ExecuteMotion(ctx, 'j', 3)
	if getCursorY(ctx) != 8 {
		t.Errorf("3j motion: expected Y=8, got Y=%d", getCursorY(ctx))
	}

	// Test 4k
	setCursorPosition(ctx, getCursorX(ctx), 10)
	ExecuteMotion(ctx, 'k', 4)
	if getCursorY(ctx) != 6 {
		t.Errorf("4k motion: expected Y=6, got Y=%d", getCursorY(ctx))
	}

	// Test 7l
	setCursorPosition(ctx, 3, getCursorY(ctx))
	ExecuteMotion(ctx, 'l', 7)
	if getCursorX(ctx) != 10 {
		t.Errorf("7l motion: expected X=10, got X=%d", getCursorX(ctx))
	}

	// Test 5 space (should work like l)
	setCursorPosition(ctx, 3, getCursorY(ctx))
	ExecuteMotion(ctx, ' ', 5)
	if getCursorX(ctx) != 8 {
		t.Errorf("5 space motion: expected X=8, got X=%d", getCursorX(ctx))
	}
}
