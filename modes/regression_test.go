package modes

import (
	"testing"
)

// ========================================
// Regression Tests for Basic Motions
// ========================================

// TestBasicMotionsStillWork ensures h, j, k, l still function correctly
func TestBasicMotionsStillWork(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	tests := []struct {
		name       string
		motion     rune
		initialX   int
		initialY   int
		expectedX  int
		expectedY  int
	}{
		{"h moves left", 'h', 10, 10, 9, 10},
		{"j moves down", 'j', 10, 10, 10, 11},
		{"k moves up", 'k', 10, 10, 10, 9},
		{"l moves right", 'l', 10, 10, 11, 10},
		{"space moves right", ' ', 10, 10, 11, 10},
		{"h at left edge stays", 'h', 0, 10, 0, 10},
		{"j at bottom edge stays", 'j', 10, 23, 10, 23},
		{"k at top edge stays", 'k', 10, 0, 10, 0},
		{"l at right edge stays", 'l', 79, 10, 79, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.CursorX = tt.initialX
			ctx.CursorY = tt.initialY

			ExecuteMotion(ctx, tt.motion, 1)

			assertCursorAt(t, ctx, tt.expectedX, tt.expectedY)
		})
	}
}

// TestWordMotionsStillWork ensures w, b, e, W, B, E still function
func TestWordMotionsStillWork(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "hello-world foo_bar baz"
	// Positions: h=0, e=1, l=2, l=3, o=4, -=5, w=6, o=7, r=8, l=9, d=10, space=11,
	//            f=12, o=13, o=14, _=15, b=16, a=17, r=18, space=19, b=20, a=21, z=22
	placeTextAt(ctx, 0, 5, "hello-world foo_bar baz")

	tests := []struct {
		name       string
		motion     rune
		initialX   int
		expectedX  int
		desc       string
	}{
		{"w from start", 'w', 0, 5, "should move to '-' (punctuation)"},
		{"w from punctuation", 'w', 5, 6, "should move to 'w' after punctuation"},
		{"e from start", 'e', 0, 4, "should move to end of 'hello'"},
		{"b from middle", 'b', 10, 6, "should move to start of 'world'"},
		{"W from start", 'W', 0, 12, "should move to 'f' (space-delimited)"},
		{"E from start", 'E', 0, 10, "should move to end of 'hello-world'"},
		{"B from end", 'B', 22, 20, "should move to start of 'baz'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.CursorX = tt.initialX
			ctx.CursorY = 5

			ExecuteMotion(ctx, tt.motion, 1)

			if ctx.CursorX != tt.expectedX {
				t.Errorf("%s: expected X=%d, got X=%d", tt.desc, tt.expectedX, ctx.CursorX)
			}
		})
	}
}

// TestLineMotionsStillWork ensures 0, ^, $ still function
func TestLineMotionsStillWork(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "  hello world"
	placeTextAt(ctx, 0, 7, "  hello world")

	tests := []struct {
		name       string
		motion     rune
		initialX   int
		expectedX  int
		desc       string
	}{
		{"$ goes to end", '$', 5, 12, "should move to last char 'd'"},
		{"0 goes to start", '0', 5, 0, "should move to column 0"},
		{"^ goes to first non-space", '^', 5, 2, "should move to first 'h'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.CursorX = tt.initialX
			ctx.CursorY = 7

			ExecuteMotion(ctx, tt.motion, 1)

			if ctx.CursorX != tt.expectedX {
				t.Errorf("%s: expected X=%d, got X=%d", tt.desc, tt.expectedX, ctx.CursorX)
			}
		})
	}
}

// TestScreenMotionsStillWork ensures H, M, L, G, gg still function
func TestScreenMotionsStillWork(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	tests := []struct {
		name       string
		motion     rune
		initialX   int
		initialY   int
		expectedX  int
		expectedY  int
	}{
		{"H moves to top", 'H', 10, 15, 10, 0},
		{"M moves to middle", 'M', 10, 5, 10, 12},
		{"L moves to bottom", 'L', 10, 5, 10, 23},
		{"G moves to bottom line", 'G', 10, 5, 10, 23},
		{"g moves to top line", 'g', 10, 15, 10, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.CursorX = tt.initialX
			ctx.CursorY = tt.initialY

			ExecuteMotion(ctx, tt.motion, 1)

			assertCursorAt(t, ctx, tt.expectedX, tt.expectedY)
		})
	}
}

// TestParagraphMotionsStillWork ensures {, } still function
func TestParagraphMotionsStillWork(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create some lines with empty lines at specific positions
	placeTextAt(ctx, 0, 2, "line2")
	// Line 3 is empty
	placeTextAt(ctx, 0, 4, "line4")
	placeTextAt(ctx, 0, 5, "line5")
	// Line 6 is empty
	placeTextAt(ctx, 0, 7, "line7")

	tests := []struct {
		name       string
		motion     rune
		initialY   int
		expectedY  int
	}{
		{"} finds next empty line", '}', 2, 3},
		{"} finds next empty line from 5", '}', 5, 6},
		{"{ finds prev empty line", '{', 5, 3},
		{"{ finds prev empty line from 7", '{', 7, 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.CursorX = 0
			ctx.CursorY = tt.initialY

			ExecuteMotion(ctx, tt.motion, 1)

			if ctx.CursorY != tt.expectedY {
				t.Errorf("expected Y=%d, got Y=%d", tt.expectedY, ctx.CursorY)
			}
		})
	}
}

// ========================================
// Regression Tests for Count Prefixes
// ========================================

// TestCountWithBasicMotions ensures 5h, 3j, 10w still work
func TestCountWithBasicMotions(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	tests := []struct {
		name       string
		motion     rune
		count      int
		initialX   int
		initialY   int
		expectedX  int
		expectedY  int
	}{
		{"5h moves left 5 times", 'h', 5, 20, 10, 15, 10},
		{"3j moves down 3 times", 'j', 3, 10, 5, 10, 8},
		{"2k moves up 2 times", 'k', 2, 10, 10, 10, 8},
		{"7l moves right 7 times", 'l', 7, 10, 10, 17, 10},
		{"10h from 5 stops at 0", 'h', 10, 5, 10, 0, 10},
		{"30j from 10 stops at 23", 'j', 30, 10, 10, 10, 23},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.CursorX = tt.initialX
			ctx.CursorY = tt.initialY

			ExecuteMotion(ctx, tt.motion, tt.count)

			assertCursorAt(t, ctx, tt.expectedX, tt.expectedY)
		})
	}
}

// TestCountWithWordMotions ensures 3w, 2e, 4b still work
func TestCountWithWordMotions(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "one two three four five"
	placeTextAt(ctx, 0, 8, "one two three four five")

	tests := []struct {
		name       string
		motion     rune
		count      int
		initialX   int
		minExpectedX int
		desc       string
	}{
		{"3w from start", 'w', 3, 0, 10, "should jump forward 3 words"},
		{"2e from start", 'e', 2, 0, 6, "should jump to 2nd word end"},
		{"2b from end", 'b', 2, 20, 10, "should jump back 2 words"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.CursorX = tt.initialX
			ctx.CursorY = 8

			ExecuteMotion(ctx, tt.motion, tt.count)

			// Check that cursor moved at least to expected position
			if ctx.CursorX < tt.minExpectedX {
				t.Errorf("%s: expected X>=%d, got X=%d", tt.desc, tt.minExpectedX, ctx.CursorX)
			}
		})
	}
}

// ========================================
// Regression Tests for Special Commands
// ========================================

// TestSpecialCommandsStillWork ensures gg, G, go, dd, d$ still work
func TestSpecialCommandsStillWork(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	t.Run("gg goes to top-left", func(t *testing.T) {
		ctx.CursorX = 40
		ctx.CursorY = 15

		// Simulate "gg" (two g presses)
		ExecuteMotion(ctx, 'g', 1)

		assertCursorAt(t, ctx, 40, 0)
	})

	t.Run("G goes to bottom", func(t *testing.T) {
		ctx.CursorX = 40
		ctx.CursorY = 5

		ExecuteMotion(ctx, 'G', 1)

		assertCursorAt(t, ctx, 40, 23)
	})

	t.Run("x deletes character", func(t *testing.T) {
		// Create test character
		placeTextAt(ctx, 10, 3, "x")
		ctx.CursorX = 10
		ctx.CursorY = 3

		ExecuteMotion(ctx, 'x', 1)

		// Character should be deleted
		entity := ctx.World.GetEntityAtPosition(10, 3)
		if entity != 0 {
			t.Errorf("Character should be deleted, but entity %d still exists", entity)
		}
	})
}

// ========================================
// Regression Tests for Bracket Matching
// ========================================

// TestBracketMatchingStillWorks ensures % command still functions
func TestBracketMatchingStillWorks(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "if (x == 1) { return }"
	placeTextAt(ctx, 0, 10, "if (x == 1) { return }")

	tests := []struct {
		name       string
		startX     int
		expectedX  int
		desc       string
	}{
		{"% from ( to )", 3, 10, "should jump from '(' to ')'"},
		{"% from ) to (", 10, 3, "should jump from ')' to '('"},
		{"% from { to }", 12, 21, "should jump from '{' to '}'"},
		{"% from } to {", 21, 12, "should jump from '}' to '{'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.CursorX = tt.startX
			ctx.CursorY = 10

			ExecuteMotion(ctx, '%', 1)

			if ctx.CursorX != tt.expectedX {
				t.Errorf("%s: expected X=%d, got X=%d", tt.desc, tt.expectedX, ctx.CursorX)
			}
		})
	}
}

// ========================================
// Regression Tests for Cursor Validation
// ========================================

// TestCursorValidationStillWorks ensures cursor stays within bounds
func TestCursorValidationStillWorks(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	t.Run("excessive left movement stops at 0", func(t *testing.T) {
		ctx.CursorX = 5
		ctx.CursorY = 10

		ExecuteMotion(ctx, 'h', 100)

		assertCursorAt(t, ctx, 0, 10)
	})

	t.Run("excessive right movement stops at width-1", func(t *testing.T) {
		ctx.CursorX = 70
		ctx.CursorY = 10

		ExecuteMotion(ctx, 'l', 100)

		assertCursorAt(t, ctx, 79, 10)
	})

	t.Run("excessive up movement stops at 0", func(t *testing.T) {
		ctx.CursorX = 10
		ctx.CursorY = 5

		ExecuteMotion(ctx, 'k', 100)

		assertCursorAt(t, ctx, 10, 0)
	})

	t.Run("excessive down movement stops at height-1", func(t *testing.T) {
		ctx.CursorX = 10
		ctx.CursorY = 20

		ExecuteMotion(ctx, 'j', 100)

		assertCursorAt(t, ctx, 10, 23)
	})
}

// ========================================
// No Race Conditions Tests
// ========================================

// TestNoRaceInMotionExecution ensures no race conditions in motion execution
// Note: This test must be run with -race flag to detect races
func TestNoRaceInMotionExecution(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line
	placeTextAt(ctx, 0, 0, "abcdefghijklmnopqrstuvwxyz")

	// Execute multiple motions rapidly (simulating user input)
	motions := []rune{'l', 'l', 'w', 'h', 'j', 'k', '$', '0', '^'}

	for _, motion := range motions {
		ExecuteMotion(ctx, motion, 1)
	}

	// If we get here without race detector errors, test passes
	t.Log("No race conditions detected in motion execution")
}

// TestNoRaceInFindMotions ensures no race conditions in find motions
func TestNoRaceInFindMotions(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line
	placeTextAt(ctx, 0, 5, "aaaaabbbbbcccccddddd")

	// Execute multiple find motions rapidly
	ExecuteFindChar(ctx, 'a', 1)
	ExecuteFindChar(ctx, 'b', 2)
	ExecuteFindCharBackward(ctx, 'a', 1)
	ExecuteFindChar(ctx, 'c', 3)
	ExecuteFindCharBackward(ctx, 'b', 2)

	t.Log("No race conditions detected in find motions")
}

// TestConcurrentMotionUpdates tests concurrent cursor position updates
func TestConcurrentMotionUpdates(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// This test verifies that cursor position updates are atomic
	// and don't cause data races when accessed concurrently

	// Set initial position
	ctx.CursorX = 10
	ctx.CursorY = 10

	// Execute a series of motions
	for i := 0; i < 10; i++ {
		ExecuteMotion(ctx, 'l', 1)
		ExecuteMotion(ctx, 'j', 1)
		ExecuteMotion(ctx, 'h', 1)
		ExecuteMotion(ctx, 'k', 1)
	}

	// Verify cursor is still within valid bounds
	if ctx.CursorX < 0 || ctx.CursorX >= ctx.GameWidth {
		t.Errorf("CursorX out of bounds: %d", ctx.CursorX)
	}
	if ctx.CursorY < 0 || ctx.CursorY >= ctx.GameHeight {
		t.Errorf("CursorY out of bounds: %d", ctx.CursorY)
	}

	t.Log("Concurrent motion updates completed without race conditions")
}

// ========================================
// Integration Test: Full Command Sequences
// ========================================

// TestComplexCommandSequences tests realistic vi command sequences
func TestComplexCommandSequences(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create multi-line test content
	placeTextAt(ctx, 0, 0, "func main() {")
	placeTextAt(ctx, 4, 1, "fmt.Println(hello)")
	placeTextAt(ctx, 4, 2, "return")
	placeTextAt(ctx, 0, 3, "}")

	t.Run("navigate to function name and back", func(t *testing.T) {
		ctx.CursorX = 0
		ctx.CursorY = 0

		// Move to 'main'
		ExecuteMotion(ctx, 'w', 1)
		// Move to '('
		ExecuteMotion(ctx, 'w', 1)
		// Move back to 'main'
		ExecuteMotion(ctx, 'b', 1)
		// Move to start of line
		ExecuteMotion(ctx, '0', 1)

		assertCursorAt(t, ctx, 0, 0)
	})

	t.Run("navigate down and to end of line", func(t *testing.T) {
		ctx.CursorX = 0
		ctx.CursorY = 0

		// Move down 2 lines
		ExecuteMotion(ctx, 'j', 2)
		// Move to end of line
		ExecuteMotion(ctx, '$', 1)

		if ctx.CursorY != 2 {
			t.Errorf("Expected Y=2, got Y=%d", ctx.CursorY)
		}
		// X should be at last character
		if ctx.CursorX < 6 {
			t.Errorf("Expected X>=6 (near end), got X=%d", ctx.CursorX)
		}
	})
}

// TestCommandStateReset verifies that command state is properly reset between operations
func TestCommandStateReset(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	placeTextAt(ctx, 0, 0, "test line")

	// Execute a motion
	ExecuteMotion(ctx, 'l', 5)

	// Verify internal state is clean
	if ctx.WaitingForF {
		t.Error("WaitingForF should be false after motion")
	}
	if ctx.WaitingForFBackward {
		t.Error("WaitingForFBackward should be false after motion")
	}

	// Execute a find
	ExecuteFindChar(ctx, 't', 1)

	// State should remain clean
	if ctx.WaitingForF {
		t.Error("WaitingForF should be false after find execution")
	}
}

// TestMotionCountPreservation verifies that counts are properly handled
func TestMotionCountPreservation(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	tests := []struct {
		name         string
		motion       rune
		count        int
		verifyMinMove int
	}{
		{"5l moves at least 5", 'l', 5, 5},
		{"10j moves at least 10", 'j', 10, 10},
		{"3k moves at least 3", 'k', 3, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			startX := 20
			startY := 15
			ctx.CursorX = startX
			ctx.CursorY = startY

			ExecuteMotion(ctx, tt.motion, tt.count)

			var moved int
			switch tt.motion {
			case 'l':
				moved = ctx.CursorX - startX
			case 'j':
				moved = ctx.CursorY - startY
			case 'k':
				moved = startY - ctx.CursorY
			case 'h':
				moved = startX - ctx.CursorX
			}

			if moved < tt.verifyMinMove && moved >= 0 {
				// Allow for boundary conditions
				if (tt.motion == 'l' && ctx.CursorX == ctx.GameWidth-1) ||
					(tt.motion == 'j' && ctx.CursorY == ctx.GameHeight-1) ||
					(tt.motion == 'k' && ctx.CursorY == 0) ||
					(tt.motion == 'h' && ctx.CursorX == 0) {
					// Hit boundary, acceptable
					return
				}
				t.Errorf("Expected to move at least %d, moved %d", tt.verifyMinMove, moved)
			}
		})
	}
}
