package modes

// Comprehensive tests for till motion functionality (t/T commands) and find/till repeat (;/, commands).

import (
	"testing"
)

// ========================================
// Till Forward (t) Tests
// ========================================

// TestTillCharBasic tests single character till (moves one position before target)
func TestTillCharBasic(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "hello world"
	placeTextAt(ctx, 0, 0, "hello world")

	tests := []struct {
		name       string
		startX     int
		targetChar rune
		expectedX  int
	}{
		{"till first 'e' (moves to position before)", 0, 'e', 0}, // 'e' is at 1, till moves to 0 (too close, stays)
		{"till first 'o' from start", 0, 'o', 3},                 // 'o' is at 4, till moves to 3
		{"till 'w' from start", 0, 'w', 5},                       // 'w' is at 6, till moves to 5
		{"till 'l' from position 2", 2, 'l', 2},                  // 'l' is at 3, till moves to 2 (already there)
		{"till second 'o' from position 5", 5, 'o', 6},           // second 'o' is at 7, till moves to 6
		{"till 'd' from start", 0, 'd', 9},                       // 'd' is at 10, till moves to 9
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setCursorPosition(ctx, tt.startX, getCursorY(ctx))
			setCursorPosition(ctx, getCursorX(ctx), 0)

			ExecuteTillChar(ctx, tt.targetChar, 1)

			assertCursorAt(t, ctx, tt.expectedX, 0)
		})
	}
}

// TestTillCharWithCountComprehensive tests 2ta, 3tb, 5tx scenarios
func TestTillCharWithCountComprehensive(t *testing.T) {
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
		desc       string
	}{
		{"2ta finds 2nd 'a', moves before", 0, 'a', 2, 3, "2nd 'a' at 4, moves to 3"},
		{"3ta finds 3rd 'a', moves before", 0, 'a', 3, 4, "3rd 'a' at 5, moves to 4"},
		{"5ta finds 5th 'a', moves before", 0, 'a', 5, 8, "5th 'a' at 9, moves to 8"},
		{"2tb finds 2nd 'b', moves before", 0, 'b', 2, 6, "2nd 'b' at 7, moves to 6"},
		{"1tx finds 1st 'x', moves before", 0, 'x', 1, 2, "1st 'x' at 3, moves to 2"},
		{"2tx finds 2nd 'x', moves before", 0, 'x', 2, 5, "2nd 'x' at 6, moves to 5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setCursorPosition(ctx, tt.startX, getCursorY(ctx))
			setCursorPosition(ctx, getCursorX(ctx), 5)

			ExecuteTillChar(ctx, tt.targetChar, tt.count)

			if getCursorX(ctx) != tt.expectedX {
				t.Errorf("%s: expected X=%d, got X=%d", tt.desc, tt.expectedX, getCursorX(ctx))
			}
		})
	}
}

// TestTillCharCountExceedsMatches tests when count > available matches
func TestTillCharCountExceedsMatches(t *testing.T) {
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
		{"10ta (only 2 'a's, move before last)", 0, 'a', 10, 3, "last 'a' at 4, move to 3"},
		{"5tb (only 2 'b's, move before last)", 0, 'b', 5, 4, "last 'b' at 5, move to 4"},
		{"100tc (only 2 'c's, move before last)", 0, 'c', 100, 5, "last 'c' at 6, move to 5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setCursorPosition(ctx, tt.startX, getCursorY(ctx))
			setCursorPosition(ctx, getCursorX(ctx), 10)

			ExecuteTillChar(ctx, tt.targetChar, tt.count)

			if getCursorX(ctx) != tt.expectedX {
				t.Errorf("%s: expected X=%d, got X=%d", tt.desc, tt.expectedX, getCursorX(ctx))
			}
		})
	}
}

// TestTillCharNoMatch tests when target character not found
func TestTillCharNoMatch(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "hello world"
	placeTextAt(ctx, 0, 3, "hello world")

	tests := []struct {
		name       string
		startX     int
		targetChar rune
	}{
		{"till 'z' (not in line)", 0, 'z'},
		{"till 'x' (not in line)", 2, 'x'},
		{"till 'q' (not in line)", 5, 'q'},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setCursorPosition(ctx, tt.startX, getCursorY(ctx))
			setCursorPosition(ctx, getCursorX(ctx), 3)
			originalX := tt.startX

			ExecuteTillChar(ctx, tt.targetChar, 1)

			// Cursor should not move when character not found
			if getCursorX(ctx) != originalX {
				t.Errorf("Cursor should not move when character not found. Started at X=%d, ended at X=%d", originalX, getCursorX(ctx))
			}
		})
	}
}

// TestTillCharTooClose tests when target is immediately adjacent (should not move)
func TestTillCharTooClose(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "ab"
	placeTextAt(ctx, 0, 8, "ab")

	t.Run("ta when 'a' is at next position", func(t *testing.T) {
		setCursorPosition(ctx, 0, getCursorY(ctx))
		setCursorPosition(ctx, getCursorX(ctx), 8)
		originalX := getCursorX(ctx)

		ExecuteTillChar(ctx, 'b', 1) // 'b' is at position 1 (cursor+1)

		// Can't move to position before 'b' because we're already past it
		// Should stay in place
		if getCursorX(ctx) != originalX {
			t.Errorf("Cursor should not move when target is too close. Started at X=%d, ended at X=%d", originalX, getCursorX(ctx))
		}
	})
}

// ========================================
// Till Backward (T) Tests
// ========================================

// TestTillCharBackwardBasic tests single backward till (moves one position after target)
func TestTillCharBackwardBasic(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "hello world"
	placeTextAt(ctx, 0, 2, "hello world")

	tests := []struct {
		name       string
		startX     int
		targetChar rune
		expectedX  int
		desc       string
	}{
		{"till 'o' backward from end", 10, 'o', 8, "last 'o' at 7, move to 8"},
		{"till 'l' backward from 9", 9, 'l', 4, "last 'l' before 9 is at 3, move to 4"},
		{"till 'h' backward from 5", 5, 'h', 1, "'h' at 0, move to 1"},
		{"till 'e' backward from 8", 8, 'e', 2, "'e' at 1, move to 2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setCursorPosition(ctx, tt.startX, getCursorY(ctx))
			setCursorPosition(ctx, getCursorX(ctx), 2)

			ExecuteTillCharBackward(ctx, tt.targetChar, 1)

			if getCursorX(ctx) != tt.expectedX {
				t.Errorf("%s: expected X=%d, got X=%d", tt.desc, tt.expectedX, getCursorX(ctx))
			}
		})
	}
}

// TestTillCharBackwardWithCountComprehensive tests 2Ta, 3Tb scenarios
func TestTillCharBackwardWithCountComprehensive(t *testing.T) {
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
		desc       string
	}{
		{"2Ta from end finds 2nd 'a' backward, move after", 12, 'a', 2, 11, "2nd 'a' back at 10, move to 11"},
		{"3Ta from end finds 3rd 'a' backward, move after", 12, 'a', 3, 10, "3rd 'a' back at 9, move to 10"},
		{"5Ta from end finds 5th 'a' backward, move after", 12, 'a', 5, 6, "5th 'a' back at 5, move to 6"},
		{"2Tb from end finds 2nd 'b' backward, move after", 12, 'b', 2, 4, "2nd 'b' back at 3, move to 4"},
		{"2Tx from end finds 2nd 'x' backward, move after", 12, 'x', 2, 5, "2nd 'x' back at 4, move to 5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setCursorPosition(ctx, tt.startX, getCursorY(ctx))
			setCursorPosition(ctx, getCursorX(ctx), 7)

			ExecuteTillCharBackward(ctx, tt.targetChar, tt.count)

			if getCursorX(ctx) != tt.expectedX {
				t.Errorf("%s: expected X=%d, got X=%d", tt.desc, tt.expectedX, getCursorX(ctx))
			}
		})
	}
}

// TestTillCharBackwardFromStart tests no matches when at line start
func TestTillCharBackwardFromStart(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "hello"
	placeTextAt(ctx, 0, 4, "hello")

	tests := []struct {
		name       string
		startX     int
		targetChar rune
	}{
		{"Th from position 0 (no chars before)", 0, 'h'},
		{"Te from position 0 (no chars before)", 0, 'e'},
		{"Ta from position 1 (char not behind)", 1, 'a'},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setCursorPosition(ctx, tt.startX, getCursorY(ctx))
			setCursorPosition(ctx, getCursorX(ctx), 4)
			originalX := tt.startX

			ExecuteTillCharBackward(ctx, tt.targetChar, 1)

			// Cursor should not move
			if getCursorX(ctx) != originalX {
				t.Errorf("Cursor should not move. Started at X=%d, ended at X=%d", originalX, getCursorX(ctx))
			}
		})
	}
}

// TestTillCharBackwardCountExceedsMatches tests when count exceeds available matches
func TestTillCharBackwardCountExceedsMatches(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "abc abc"
	placeTextAt(ctx, 0, 11, "abc abc")

	tests := []struct {
		name       string
		startX     int
		targetChar rune
		count      int
		expectedX  int
		desc       string
	}{
		{"10Ta from end (only 2 'a's, move after first)", 6, 'a', 10, 1, "first 'a' at 0, move to 1"},
		{"5Tb from end (only 2 'b's, move after first)", 6, 'b', 5, 2, "first 'b' at 1, move to 2"},
		{"100Tc from end (only 2 'c's, move after first)", 6, 'c', 100, 3, "first 'c' at 2, move to 3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setCursorPosition(ctx, tt.startX, getCursorY(ctx))
			setCursorPosition(ctx, getCursorX(ctx), 11)

			ExecuteTillCharBackward(ctx, tt.targetChar, tt.count)

			if getCursorX(ctx) != tt.expectedX {
				t.Errorf("%s: expected X=%d, got X=%d", tt.desc, tt.expectedX, getCursorX(ctx))
			}
		})
	}
}

// TestTillCharBackwardTooClose tests when target is immediately adjacent
func TestTillCharBackwardTooClose(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "ab"
	placeTextAt(ctx, 0, 9, "ab")

	t.Run("Ta when 'a' is at previous position", func(t *testing.T) {
		setCursorPosition(ctx, 1, getCursorY(ctx)) // At 'b'
		setCursorPosition(ctx, getCursorX(ctx), 9)
		originalX := getCursorX(ctx)

		ExecuteTillCharBackward(ctx, 'a', 1) // 'a' is at position 0 (cursor-1)

		// Can't move to position after 'a' because we'd be at cursor position
		// Should stay in place
		if getCursorX(ctx) != originalX {
			t.Errorf("Cursor should not move when target is too close. Started at X=%d, ended at X=%d", originalX, getCursorX(ctx))
		}
	})
}

// ========================================
// Repeat Find/Till Tests (; and ,)
// ========================================

// TestRepeatFindCharForward tests ';' command after various find/till operations
func TestRepeatFindCharForward(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "abcabcabc"
	placeTextAt(ctx, 0, 0, "abcabcabc")

	t.Run("fa then ; (repeat find forward)", func(t *testing.T) {
		setCursorPosition(ctx, 0, getCursorY(ctx))
		setCursorPosition(ctx, getCursorX(ctx), 0)

		// First: fa (find 'a' from position 0, which searches from position 1 onward)
		ExecuteFindChar(ctx, 'a', 1)
		assertCursorAt(t, ctx, 3, 0) // Next 'a' is at position 3

		// Second: ; (repeat find 'a' forward)
		RepeatFindChar(ctx, false)
		assertCursorAt(t, ctx, 6, 0) // Next 'a' at position 6
	})

	t.Run("ta then ; (repeat till forward)", func(t *testing.T) {
		setCursorPosition(ctx, 0, getCursorY(ctx))
		setCursorPosition(ctx, getCursorX(ctx), 0)

		// First: tb (till 'b' from position 0)
		ExecuteTillChar(ctx, 'b', 1)
		assertCursorAt(t, ctx, 0, 0) // 'b' at 1, till moves to 0 (too close, stays at 0)

		// Move to position 1 to test repeat (past the first 'b')
		setCursorPosition(ctx, 1, getCursorY(ctx))

		// Execute tb again to set up the last find state
		ExecuteTillChar(ctx, 'b', 1)
		assertCursorAt(t, ctx, 3, 0) // 'b' at 4, till moves to 3

		// Second: ; (repeat till 'b' forward)
		RepeatFindChar(ctx, false)
		assertCursorAt(t, ctx, 3, 0) // 'b' at 4 is too close (cursor+1), stays at 3
	})

	t.Run("Fa then ; (repeat find backward, same direction)", func(t *testing.T) {
		setCursorPosition(ctx, 8, getCursorY(ctx))
		setCursorPosition(ctx, getCursorX(ctx), 0)

		// First: Fa (find 'a' backward)
		ExecuteFindCharBackward(ctx, 'a', 1)
		assertCursorAt(t, ctx, 6, 0) // 'a' at position 6

		// Second: ; (repeat find 'a' backward - same direction)
		RepeatFindChar(ctx, false)
		assertCursorAt(t, ctx, 3, 0) // Previous 'a' at position 3
	})

	t.Run("Ta then ; (repeat till backward, same direction)", func(t *testing.T) {
		setCursorPosition(ctx, 8, getCursorY(ctx))
		setCursorPosition(ctx, getCursorX(ctx), 0)

		// First: Ta (till 'a' backward)
		ExecuteTillCharBackward(ctx, 'a', 1)
		assertCursorAt(t, ctx, 7, 0) // 'a' at 6, till backward moves to 7

		// Second: ; (repeat till 'a' backward - same direction)
		RepeatFindChar(ctx, false)
		assertCursorAt(t, ctx, 7, 0) // Previous 'a' at 6 is too close, stays at 7
	})
}

// TestRepeatFindCharReverse tests ',' command (reverse direction)
func TestRepeatFindCharReverse(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "abcabcabc"
	placeTextAt(ctx, 0, 1, "abcabcabc")

	t.Run("fa then , (reverse to Fa)", func(t *testing.T) {
		setCursorPosition(ctx, 3, getCursorY(ctx))
		setCursorPosition(ctx, getCursorX(ctx), 1)

		// First: fa (find 'a' forward)
		ExecuteFindChar(ctx, 'a', 1)
		assertCursorAt(t, ctx, 6, 1) // Next 'a' at position 6

		// Second: , (reverse direction - find 'a' backward)
		RepeatFindChar(ctx, true)
		assertCursorAt(t, ctx, 3, 1) // Previous 'a' at position 3
	})

	t.Run("ta then , (reverse to Ta)", func(t *testing.T) {
		setCursorPosition(ctx, 3, getCursorY(ctx))
		setCursorPosition(ctx, getCursorX(ctx), 1)

		// First: tb (till 'b' forward)
		ExecuteTillChar(ctx, 'b', 1)
		assertCursorAt(t, ctx, 3, 1) // 'b' at 4, till moves to 3

		// Second: , (reverse direction - till 'b' backward)
		RepeatFindChar(ctx, true)
		assertCursorAt(t, ctx, 2, 1) // Previous 'b' at 1, till backward moves to 2
	})

	t.Run("Fa then , (reverse to fa)", func(t *testing.T) {
		setCursorPosition(ctx, 6, getCursorY(ctx))
		setCursorPosition(ctx, getCursorX(ctx), 1)

		// First: Fa (find 'a' backward)
		ExecuteFindCharBackward(ctx, 'a', 1)
		assertCursorAt(t, ctx, 3, 1) // 'a' at position 3

		// Second: , (reverse direction - find 'a' forward)
		RepeatFindChar(ctx, true)
		assertCursorAt(t, ctx, 6, 1) // Next 'a' at position 6
	})

	t.Run("Ta then , (reverse to ta)", func(t *testing.T) {
		setCursorPosition(ctx, 6, getCursorY(ctx))
		setCursorPosition(ctx, getCursorX(ctx), 1)

		// First: Ta (till 'a' backward)
		ExecuteTillCharBackward(ctx, 'a', 1)
		assertCursorAt(t, ctx, 4, 1) // 'a' at 3, till backward moves to 4

		// Second: , (reverse direction - till 'a' forward)
		RepeatFindChar(ctx, true)
		assertCursorAt(t, ctx, 5, 1) // Next 'a' at 6, till forward moves to 5
	})
}

// TestRepeatFindCharNoLastFind tests ; and , when no previous find/till was executed
func TestRepeatFindCharNoLastFind(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "abc"
	placeTextAt(ctx, 0, 5, "abc")

	t.Run("; with no previous find", func(t *testing.T) {
		setCursorPosition(ctx, 0, getCursorY(ctx))
		setCursorPosition(ctx, getCursorX(ctx), 5)
		originalX := getCursorX(ctx)

		// Execute ; without any previous find
		RepeatFindChar(ctx, false)

		// Cursor should not move
		if getCursorX(ctx) != originalX {
			t.Errorf("Cursor should not move when no previous find. Started at X=%d, ended at X=%d", originalX, getCursorX(ctx))
		}
	})

	t.Run(", with no previous find", func(t *testing.T) {
		setCursorPosition(ctx, 0, getCursorY(ctx))
		setCursorPosition(ctx, getCursorX(ctx), 5)
		originalX := getCursorX(ctx)

		// Execute , without any previous find
		RepeatFindChar(ctx, true)

		// Cursor should not move
		if getCursorX(ctx) != originalX {
			t.Errorf("Cursor should not move when no previous find. Started at X=%d, ended at X=%d", originalX, getCursorX(ctx))
		}
	})
}

// TestRepeatFindCharMultipleTimes tests chaining ; and ,
func TestRepeatFindCharMultipleTimes(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "aaaaa" (5 'a's at positions 0-4)
	placeTextAt(ctx, 0, 6, "aaaaa")

	t.Run("fa then ;;; (three forward repeats)", func(t *testing.T) {
		setCursorPosition(ctx, 0, getCursorY(ctx))
		setCursorPosition(ctx, getCursorX(ctx), 6)

		// fa (from position 0, searches from position 1 onward, finds 'a' at position 1)
		ExecuteFindChar(ctx, 'a', 1)
		assertCursorAt(t, ctx, 1, 6) // finds 'a' at position 1

		// First ; (from position 1, finds next 'a' at position 2)
		RepeatFindChar(ctx, false)
		assertCursorAt(t, ctx, 2, 6) // next 'a' at position 2

		// Second ; (from position 2, finds next 'a' at position 3)
		RepeatFindChar(ctx, false)
		assertCursorAt(t, ctx, 3, 6) // next 'a' at position 3

		// Third ; (from position 3, finds next 'a' at position 4)
		RepeatFindChar(ctx, false)
		assertCursorAt(t, ctx, 4, 6) // next 'a' at position 4
	})

	t.Run("fa then ;,;, (forward, back, forward, back)", func(t *testing.T) {
		setCursorPosition(ctx, 0, getCursorY(ctx))
		setCursorPosition(ctx, getCursorX(ctx), 6)

		// fa (set up last find) - from position 0, finds 'a' at position 1
		ExecuteFindChar(ctx, 'a', 1)
		assertCursorAt(t, ctx, 1, 6)

		// ; - finds next 'a' at position 2
		RepeatFindChar(ctx, false)
		assertCursorAt(t, ctx, 2, 6)

		// , - finds previous 'a' backward at position 1
		RepeatFindChar(ctx, true)
		assertCursorAt(t, ctx, 1, 6)

		// ; - finds next 'a' forward at position 2
		RepeatFindChar(ctx, false)
		assertCursorAt(t, ctx, 2, 6)

		// , - finds previous 'a' backward from 2, searching from 1 to 0, finds 'a' at 1 first, but we're looking for 0
		// Actually from position 2, backward search from 1 finds 'a' at 1, but then continues to find 'a' at 0
		// Wait, the backward search finds the FIRST match (closest), so from position 2, it finds 'a' at 1
		// Actually, looking at ExecuteFindCharBackward, it finds the first occurrence and returns
		// So from position 2, searching backward, it should find 'a' at position 1 and stop
		// But I need to check the implementation again...
		// Actually from position 2 (cursor - 1 = 1), searching from 1 to 0
		// It will iterate x=1, find 'a' at 1, set occurrencesFound=1, firstMatchX=1, count=1, so it moves to position 1
		// It may seem position 1 is already passed, so it skips to 'a' at 0
		// NO - ExecuteFindCharBackward searches from CursorX - 1, which is position 1
		// It checks position 1, finds 'a', and since count==1 and occurrencesFound==1, it moves there
		RepeatFindChar(ctx, true)
		assertCursorAt(t, ctx, 1, 6)
	})
}

// TestFindTillStatePreservation tests that last find state is properly stored
func TestFindTillStatePreservation(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "abc xyz"
	placeTextAt(ctx, 0, 7, "abc xyz")

	t.Run("verify LastFindChar is stored", func(t *testing.T) {
		setCursorPosition(ctx, 0, getCursorY(ctx))
		setCursorPosition(ctx, getCursorX(ctx), 7)

		// Execute fa
		ExecuteFindChar(ctx, 'a', 1)

		// Verify state
		if ctx.LastFindChar != 'a' {
			t.Errorf("Expected LastFindChar='a', got '%c'", ctx.LastFindChar)
		}
		if ctx.LastFindForward != true {
			t.Errorf("Expected LastFindForward=true, got %v", ctx.LastFindForward)
		}
		if ctx.LastFindType != 'f' {
			t.Errorf("Expected LastFindType='f', got '%c'", ctx.LastFindType)
		}
	})

	t.Run("verify LastFindType changes for t/T/F", func(t *testing.T) {
		setCursorPosition(ctx, 0, getCursorY(ctx))
		setCursorPosition(ctx, getCursorX(ctx), 7)

		// Execute tb
		ExecuteTillChar(ctx, 'b', 1)
		if ctx.LastFindType != 't' {
			t.Errorf("Expected LastFindType='t', got '%c'", ctx.LastFindType)
		}

		// Execute Fc
		setCursorPosition(ctx, 5, getCursorY(ctx))
		ExecuteFindCharBackward(ctx, 'c', 1)
		if ctx.LastFindType != 'F' {
			t.Errorf("Expected LastFindType='F', got '%c'", ctx.LastFindType)
		}
		if ctx.LastFindForward != false {
			t.Errorf("Expected LastFindForward=false for F command")
		}

		// Execute Tx
		ExecuteTillCharBackward(ctx, 'x', 1)
		if ctx.LastFindType != 'T' {
			t.Errorf("Expected LastFindType='T', got '%c'", ctx.LastFindType)
		}
	})
}

// ========================================
// Edge Cases and Integration
// ========================================

// TestTillWithEmptyLine tests till on empty lines
func TestTillWithEmptyLine(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Line 0 is empty
	setCursorPosition(ctx, 5, 0)

	t.Run("ta on empty line", func(t *testing.T) {
		originalX := getCursorX(ctx)
		ExecuteTillChar(ctx, 'a', 1)

		// Cursor should not move
		if getCursorX(ctx) != originalX {
			t.Errorf("Cursor should not move on empty line")
		}
	})

	t.Run("Ta on empty line", func(t *testing.T) {
		originalX := getCursorX(ctx)
		ExecuteTillCharBackward(ctx, 'a', 1)

		// Cursor should not move
		if getCursorX(ctx) != originalX {
			t.Errorf("Cursor should not move on empty line")
		}
	})
}

// TestTillWithUnicodeCharacters tests till with Unicode
func TestTillWithUnicodeCharacters(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line with Unicode: "hello世界test"
	placeTextAt(ctx, 0, 12, "hello世界test")

	t.Run("till Unicode char forward", func(t *testing.T) {
		setCursorPosition(ctx, 0, getCursorY(ctx))
		setCursorPosition(ctx, getCursorX(ctx), 12)

		ExecuteTillChar(ctx, '世', 1)
		// '世' should be at position 5, till moves to 4
		if getCursorX(ctx) >= 5 {
			t.Errorf("Cursor should have moved to position before '世', got X=%d", getCursorX(ctx))
		}
	})

	t.Run("till Unicode char backward", func(t *testing.T) {
		setCursorPosition(ctx, 10, getCursorY(ctx))
		setCursorPosition(ctx, getCursorX(ctx), 12)

		ExecuteTillCharBackward(ctx, '世', 1)
		// '世' at position 5, till backward moves to 6
		if getCursorX(ctx) <= 5 {
			t.Errorf("Cursor should have moved to position after '世', got X=%d", getCursorX(ctx))
		}
	})
}

// TestTillWithCountZero tests count = 0 (should default to 1)
func TestTillWithCountZero(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "abcd"
	placeTextAt(ctx, 0, 13, "abcd")

	t.Run("ta with count=0", func(t *testing.T) {
		setCursorPosition(ctx, 0, getCursorY(ctx))
		setCursorPosition(ctx, getCursorX(ctx), 13)

		ExecuteTillChar(ctx, 'c', 0)

		// count=0 should behave like count=1
		// 'c' is at position 2, till moves to 1
		assertCursorAt(t, ctx, 1, 13)
	})

	t.Run("Tb with count=0", func(t *testing.T) {
		setCursorPosition(ctx, 3, getCursorY(ctx))
		setCursorPosition(ctx, getCursorX(ctx), 13)

		ExecuteTillCharBackward(ctx, 'b', 0)

		// count=0 should behave like count=1
		// 'b' is at position 1, till backward moves to 2
		assertCursorAt(t, ctx, 2, 13)
	})
}
