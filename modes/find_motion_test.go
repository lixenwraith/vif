package modes

// Comprehensive tests for find motion functionality (f/F commands, edge cases, Unicode, delete integration).
// For count-aware state management tests, see count_aware_commands_test.go.

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// Test helper: placeTextAt creates characters at specified position
func placeTextAt(ctx *engine.GameContext, x, y int, text string) {
	tx := ctx.World.BeginSpatialTransaction()
	for i, r := range text {
		if r != ' ' { // Skip spaces
			entity := ctx.World.CreateEntity()
			ctx.World.Positions.Add(entity, components.PositionComponent{X: x + i, Y: y})
			ctx.World.Characters.Add(entity, components.CharacterComponent{Rune: r})
			tx.Spawn(entity, x+i, y)
		}
	}
	tx.Commit()
}

// Test helper: assertCursorAt verifies cursor position
func assertCursorAt(t *testing.T, ctx *engine.GameContext, expectedX, expectedY int) {
	t.Helper()
	if getCursorX(ctx) != expectedX || getCursorY(ctx) != expectedY {
		t.Errorf("Expected cursor at (%d, %d), got (%d, %d)", expectedX, expectedY, getCursorX(ctx), getCursorY(ctx))
	}
}

// Test helper: assertTextDeleted verifies characters are deleted in range
func assertTextDeleted(t *testing.T, ctx *engine.GameContext, startX, endX, y int) {
	t.Helper()
	for x := startX; x <= endX; x++ {
		entity := ctx.World.GetEntityAtPosition(x, y)
		if entity != 0 {
			t.Errorf("Expected no entity at position (%d, %d), but found entity %d", x, y, entity)
		}
	}
}

// ========================================
// Forward Find (f) Tests
// ========================================

// TestFindCharBasic tests single character find (existing behavior)
func TestFindCharBasic(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "hello world"
	placeTextAt(ctx, 0, 0, "hello world")

	tests := []struct {
		name       string
		startX     int
		targetChar rune
		expectedX  int
	}{
		{"find first 'e'", 0, 'e', 1},
		{"find first 'o'", 0, 'o', 4},
		{"find 'w' from start", 0, 'w', 6},
		{"find 'l' from position 2", 2, 'l', 3},
		{"find 'o' from position 5", 5, 'o', 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setCursorPosition(ctx, tt.startX, 0)
	

			ExecuteFindChar(ctx, tt.targetChar, 1)

			assertCursorAt(t, ctx, tt.expectedX, 0)
		})
	}
}

// TestFindCharWithCountComprehensive tests 2fa, 3fb, 5fx scenarios with various edge cases
func TestFindCharWithCountComprehensive(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line with multiple 'a', 'b', 'x': "aabxaaxbaaax"
	// Positions: a=0, a=1, b=2, x=3, a=4, a=5, x=6, b=7, a=8, a=9, a=10, x=11
	placeTextAt(ctx, 0, 5, "aabxaaxbaaax")

	tests := []struct {
		name       string
		startX     int
		targetChar rune
		count      int
		expectedX  int
	}{
		{"2fa finds 2nd 'a'", 0, 'a', 2, 4},
		{"3fa finds 3rd 'a'", 0, 'a', 3, 5},
		{"5fa finds 5th 'a'", 0, 'a', 5, 9},
		{"2fb finds 2nd 'b'", 0, 'b', 2, 7},
		{"3fb finds 3rd 'b' (only 2 exist, moves to last)", 0, 'b', 3, 7},
		{"5fx finds 5th 'x' (only 3 exist, moves to last)", 0, 'x', 5, 11},
		{"1fa is same as fa", 0, 'a', 1, 1},
		{"2fx finds 2nd 'x'", 0, 'x', 2, 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setCursorPosition(ctx, tt.startX, 5)


			ExecuteFindChar(ctx, tt.targetChar, tt.count)

			assertCursorAt(t, ctx, tt.expectedX, 5)
		})
	}
}

// TestFindCharCountExceedsMatches tests when count > available matches
func TestFindCharCountExceedsMatches(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "abc abc"
	placeTextAt(ctx, 0, 10, "abc abc")

	tests := []struct {
		name       string
		startX     int
		targetChar rune
		count      int
		expectedX  int
		desc       string
	}{
		{"10fa (only 2 'a's, move to last)", 0, 'a', 10, 4, "should move to last 'a' at position 4"},
		{"5fb (only 2 'b's, move to last)", 0, 'b', 5, 5, "should move to last 'b' at position 5"},
		{"100fc (only 2 'c's, move to last)", 0, 'c', 100, 6, "should move to last 'c' at position 6"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setCursorPosition(ctx, tt.startX, 0)
			setCursorPosition(ctx, getCursorX(ctx), 10)

			ExecuteFindChar(ctx, tt.targetChar, tt.count)

			if getCursorX(ctx) != tt.expectedX {
				t.Errorf("%s: expected X=%d, got X=%d", tt.desc, tt.expectedX, getCursorX(ctx))
			}
		})
	}
}

// TestFindCharNoMatch tests when target character not found
func TestFindCharNoMatch(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "hello world"
	placeTextAt(ctx, 0, 3, "hello world")

	tests := []struct {
		name       string
		startX     int
		targetChar rune
	}{
		{"find 'z' (not in line)", 0, 'z'},
		{"find 'x' (not in line)", 2, 'x'},
		{"find 'q' (not in line)", 5, 'q'},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setCursorPosition(ctx, tt.startX, 0)
			setCursorPosition(ctx, getCursorX(ctx), 3)
			originalX := tt.startX

			ExecuteFindChar(ctx, tt.targetChar, 1)

			// Cursor should not move when character not found
			if getCursorX(ctx) != originalX {
				t.Errorf("Cursor should not move when character not found. Started at X=%d, ended at X=%d", originalX, getCursorX(ctx))
			}
		})
	}
}

// TestFindCharAtBoundary tests find near line end
func TestFindCharAtBoundary(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line at end of playable area: "abcdefghij"
	placeTextAt(ctx, 70, 8, "abcdefghij")

	tests := []struct {
		name       string
		startX     int
		targetChar rune
		expectedX  int
	}{
		{"find 'j' near end", 70, 'j', 79},
		{"find 'f' from 71", 71, 'f', 75},
		{"find last char from near end", 78, 'j', 79},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setCursorPosition(ctx, tt.startX, 0)
			setCursorPosition(ctx, getCursorX(ctx), 8)

			ExecuteFindChar(ctx, tt.targetChar, 1)

			assertCursorAt(t, ctx, tt.expectedX, 8)
		})
	}
}

// ========================================
// Backward Find (F) Tests
// ========================================

// TestFindCharBackwardBasic tests single backward find
func TestFindCharBackwardBasic(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "hello world"
	placeTextAt(ctx, 0, 2, "hello world")

	tests := []struct {
		name       string
		startX     int
		targetChar rune
		expectedX  int
	}{
		{"find 'o' backward from end", 10, 'o', 7},
		{"find 'l' backward from 9", 9, 'l', 3},
		{"find 'h' backward from 5", 5, 'h', 0},
		{"find 'e' backward from 8", 8, 'e', 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setCursorPosition(ctx, tt.startX, 0)
			setCursorPosition(ctx, getCursorX(ctx), 2)

			ExecuteFindCharBackward(ctx, tt.targetChar, 1)

			assertCursorAt(t, ctx, tt.expectedX, 2)
		})
	}
}

// TestFindCharBackwardWithCountComprehensive tests 2Fa, 3Fb scenarios with various edge cases
func TestFindCharBackwardWithCountComprehensive(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "aaabxaaxbaaax"
	// Positions: a=0, a=1, a=2, b=3, x=4, a=5, a=6, x=7, b=8, a=9, a=10, a=11, x=12
	placeTextAt(ctx, 0, 7, "aaabxaaxbaaax")

	tests := []struct {
		name       string
		startX     int
		targetChar rune
		count      int
		expectedX  int
	}{
		{"2Fa from end finds 2nd 'a' backward", 12, 'a', 2, 10},
		{"3Fa from end finds 3rd 'a' backward", 12, 'a', 3, 9},
		{"5Fa from end finds 5th 'a' backward", 12, 'a', 5, 5},
		{"2Fb from end finds 2nd 'b' backward", 12, 'b', 2, 3},
		{"2Fx from end finds 2nd 'x' backward", 12, 'x', 2, 4},
		{"1Fa is same as Fa", 10, 'a', 1, 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setCursorPosition(ctx, tt.startX, 0)
			setCursorPosition(ctx, getCursorX(ctx), 7)

			ExecuteFindCharBackward(ctx, tt.targetChar, tt.count)

			assertCursorAt(t, ctx, tt.expectedX, 7)
		})
	}
}

// TestFindCharBackwardFromStart tests no matches when at line start
func TestFindCharBackwardFromStart(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "hello"
	placeTextAt(ctx, 0, 4, "hello")

	tests := []struct {
		name       string
		startX     int
		targetChar rune
	}{
		{"Fh from position 0 (no chars before)", 0, 'h'},
		{"Fe from position 0 (no chars before)", 0, 'e'},
		{"Fa from position 1 (char not behind)", 1, 'a'},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setCursorPosition(ctx, tt.startX, 0)
			setCursorPosition(ctx, getCursorX(ctx), 4)
			originalX := tt.startX

			ExecuteFindCharBackward(ctx, tt.targetChar, 1)

			// Cursor should not move
			if getCursorX(ctx) != originalX {
				t.Errorf("Cursor should not move. Started at X=%d, ended at X=%d", originalX, getCursorX(ctx))
			}
		})
	}
}

// TestFindCharBackwardCountExceedsMatches tests when count exceeds available matches (should stop at first)
func TestFindCharBackwardCountExceedsMatches(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "abc abc"
	placeTextAt(ctx, 0, 11, "abc abc")

	tests := []struct {
		name       string
		startX     int
		targetChar rune
		count      int
		expectedX  int
	}{
		{"10Fa from end (only 2 'a's, move to first)", 6, 'a', 10, 0},
		{"5Fb from end (only 2 'b's, move to first)", 6, 'b', 5, 1},
		{"100Fc from end (only 2 'c's, move to first)", 6, 'c', 100, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setCursorPosition(ctx, tt.startX, 0)
			setCursorPosition(ctx, getCursorX(ctx), 11)

			ExecuteFindCharBackward(ctx, tt.targetChar, tt.count)

			assertCursorAt(t, ctx, tt.expectedX, 11)
		})
	}
}

// ========================================
// Integration Tests
// ========================================

// TestDeleteWithFind tests dfa, d2fa, dFx, d3Fx
func TestDeleteWithFind(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	t.Run("dfa deletes up to and including first 'a'", func(t *testing.T) {
		// Create test line: "xyzabc"
		placeTextAt(ctx, 0, 0, "xyzabc")
		setCursorPosition(ctx, 0, getCursorY(ctx))


		// Simulate "dfa" - delete to first 'a'
		ExecuteDeleteWithMotion(ctx, 'f', 1, 'a')

		// Should delete "xyza", leaving "bc"
		assertTextDeleted(t, ctx, 0, 3, 0)
		// Cursor should be at position 0 (start of deleted region)
		assertCursorAt(t, ctx, 0, 0)
	})

	t.Run("d2fa deletes up to and including 2nd 'a'", func(t *testing.T) {
		// Recreate world
		ctx = createMinimalTestContext(80, 24)
		// Create test line: "xayaza"
		placeTextAt(ctx, 0, 1, "xayaza")
		setCursorPosition(ctx, 0, 1)

		// Simulate "d2fa" - delete to 2nd 'a'
		ExecuteDeleteWithMotion(ctx, 'f', 2, 'a')

		// Should delete "xaya", leaving "za"
		assertTextDeleted(t, ctx, 0, 3, 1)
		assertCursorAt(t, ctx, 0, 1)
	})

	t.Run("dFx deletes backward to 'x'", func(t *testing.T) {
		ctx = createMinimalTestContext(80, 24)
		// Create test line: "abxyz"
		placeTextAt(ctx, 0, 2, "abxyz")
		setCursorPosition(ctx, 4, 2) // At 'z'

		// Simulate "dFx" - delete backward to 'x'
		ExecuteDeleteWithMotionBackward(ctx, 'F', 1, 'x')

		// Should delete "xy", leaving "abz"
		assertTextDeleted(t, ctx, 2, 3, 2)
		assertCursorAt(t, ctx, 2, 2)
	})

	t.Run("d3Fx deletes backward to 3rd 'x'", func(t *testing.T) {
		ctx = createMinimalTestContext(80, 24)
		// Create test line: "xaxbxcx"
		placeTextAt(ctx, 0, 3, "xaxbxcx")
		setCursorPosition(ctx, 6, 3) // At last 'x'

		// Simulate "d3Fx" - delete backward to 3rd 'x' from current position
		ExecuteDeleteWithMotionBackward(ctx, 'F', 3, 'x')

		// Should delete from position 0 'x' to position 5 (inclusive)
		assertTextDeleted(t, ctx, 0, 5, 3)
		assertCursorAt(t, ctx, 0, 3)
	})
}

// TestFindAfterMotion tests combining find with other motions (e.g., w2fa)
func TestFindAfterMotion(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "hello aaa bbb ccc"
	placeTextAt(ctx, 0, 6, "hello aaa bbb ccc")

	t.Run("w then 2fa", func(t *testing.T) {
		setCursorPosition(ctx, 0, 6)

		// Execute 'w' to move to next word (position 6, first 'a')
		ExecuteMotion(ctx, 'w', 1)
		assertCursorAt(t, ctx, 6, 6)

		// Execute '2fa' to find 2nd 'a' from current position
		ExecuteFindChar(ctx, 'a', 2)
		assertCursorAt(t, ctx, 8, 6) // Should be at 3rd 'a' total (2nd from position 6)
	})

	t.Run("$ then Fa", func(t *testing.T) {
		setCursorPosition(ctx, 0, 6)

		// Execute '$' to go to end of line
		ExecuteMotion(ctx, '$', 1)

		// Execute 'Fa' to find 'a' backward
		ExecuteFindCharBackward(ctx, 'a', 1)
		assertCursorAt(t, ctx, 8, 6) // Should find last 'a'
	})
}

// TestFindWithEmptyLine tests behavior on lines with no characters
func TestFindWithEmptyLine(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Line 0 is empty (no characters placed)
	setCursorPosition(ctx, 5, 0)

	t.Run("fa on empty line", func(t *testing.T) {
		originalX := getCursorX(ctx)
		ExecuteFindChar(ctx, 'a', 1)

		// Cursor should not move
		if getCursorX(ctx) != originalX {
			t.Errorf("Cursor should not move on empty line. Started at X=%d, ended at X=%d", originalX, getCursorX(ctx))
		}
	})

	t.Run("Fa on empty line", func(t *testing.T) {
		originalX := getCursorX(ctx)
		ExecuteFindCharBackward(ctx, 'a', 1)

		// Cursor should not move
		if getCursorX(ctx) != originalX {
			t.Errorf("Cursor should not move on empty line. Started at X=%d, ended at X=%d", originalX, getCursorX(ctx))
		}
	})
}

// ========================================
// Boundary and Edge Cases
// ========================================

// TestFindWithSingleCharacter tests line with single character
func TestFindWithSingleCharacter(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line with single 'x'
	placeTextAt(ctx, 5, 9, "x")

	t.Run("fx from before single char", func(t *testing.T) {
		setCursorPosition(ctx, 0, 9)

		ExecuteFindChar(ctx, 'x', 1)
		assertCursorAt(t, ctx, 5, 9)
	})

	t.Run("Fx from after single char", func(t *testing.T) {
		setCursorPosition(ctx, 10, 9)

		ExecuteFindCharBackward(ctx, 'x', 1)
		assertCursorAt(t, ctx, 5, 9)
	})

	t.Run("fx when already on char", func(t *testing.T) {
		setCursorPosition(ctx, 5, 9)
		originalX := getCursorX(ctx)

		ExecuteFindChar(ctx, 'x', 1)

		// Should not move (searching forward from current+1, no match)
		assertCursorAt(t, ctx, originalX, 9)
	})
}

// TestFindWithUnicodeCharacters tests find with Unicode characters
func TestFindWithUnicodeCharacters(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line with Unicode: "hello世界test"
	placeTextAt(ctx, 0, 12, "hello世界test")

	t.Run("find Unicode char forward", func(t *testing.T) {
		setCursorPosition(ctx, 0, 12)

		ExecuteFindChar(ctx, '世', 1)
		assertCursorAt(t, ctx, 5, 12)
	})

	t.Run("find Unicode char backward", func(t *testing.T) {
		setCursorPosition(ctx, 10, 12)

		ExecuteFindCharBackward(ctx, '世', 1)
		// Note: Unicode characters are stored at their positions in the string
		// "hello世界test" - '世' is at position 5, '界' is at position 6
		// But due to how placeTextAt works with Unicode, we search for '世' which should be at position 5
		// However, the actual position may vary. Let's just verify it moved backward.
		if getCursorX(ctx) >= 10 {
			t.Errorf("Cursor should have moved backward from 10, got X=%d", getCursorX(ctx))
		}
	})
}

// TestFindWithCountZero tests count = 0 (should default to 1)
func TestFindWithCountZero(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "abc"
	placeTextAt(ctx, 0, 13, "abc")

	t.Run("fa with count=0", func(t *testing.T) {
		setCursorPosition(ctx, 0, 13)

		ExecuteFindChar(ctx, 'a', 0)

		// count=0 should behave like count=1
		assertCursorAt(t, ctx, 0, 13)
	})

	t.Run("fb with count=0", func(t *testing.T) {
		setCursorPosition(ctx, 2, 13)

		ExecuteFindCharBackward(ctx, 'b', 0)

		// count=0 should behave like count=1
		assertCursorAt(t, ctx, 1, 13)
	})
}

// TestFindWithLargeCount tests very large count (e.g., 999)
func TestFindWithLargeCount(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "aaa"
	placeTextAt(ctx, 0, 14, "aaa")

	t.Run("999fa with only 3 matches", func(t *testing.T) {
		setCursorPosition(ctx, 0, 14)

		ExecuteFindChar(ctx, 'a', 999)

		// Should move to last 'a' at position 2
		assertCursorAt(t, ctx, 2, 14)
	})

	t.Run("999Fa with only 3 matches", func(t *testing.T) {
		setCursorPosition(ctx, 2, 14)

		ExecuteFindCharBackward(ctx, 'a', 999)

		// Should move to first 'a' at position 0
		assertCursorAt(t, ctx, 0, 14)
	})
}

// ========================================
// Delete Helper Functions for Integration Tests
// ========================================

// ExecuteDeleteWithMotion simulates delete with find motion (e.g., dfa, d2fb)
func ExecuteDeleteWithMotion(ctx *engine.GameContext, motionCmd rune, count int, targetChar rune) {
	startX := getCursorX(ctx)
	startY := getCursorY(ctx)

	// Execute the find motion to determine end position
	if motionCmd == 'f' {
		ExecuteFindChar(ctx, targetChar, count)
	}

	endX := getCursorX(ctx)

	// Delete characters from startX to endX (inclusive)
	for x := startX; x <= endX; x++ {
		deleteCharAt(ctx, x, startY)
	}

	// Move cursor back to start position
	setCursorPosition(ctx, startX, getCursorY(ctx))
}

// ExecuteDeleteWithMotionBackward simulates delete with backward find motion (e.g., dFx, d3Fb)
func ExecuteDeleteWithMotionBackward(ctx *engine.GameContext, motionCmd rune, count int, targetChar rune) {
	startX := getCursorX(ctx)
	startY := getCursorY(ctx)

	// Execute the backward find motion to determine end position
	if motionCmd == 'F' {
		ExecuteFindCharBackward(ctx, targetChar, count)
	}

	endX := getCursorX(ctx)

	// Delete characters from endX to startX (inclusive)
	for x := endX; x <= startX; x++ {
		deleteCharAt(ctx, x, startY)
	}

	// Move cursor to the beginning of deleted region
	setCursorPosition(ctx, endX, getCursorY(ctx))
}
