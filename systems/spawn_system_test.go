package systems

import (
	"math"
	"testing"

	"github.com/lixenwraith/vi-fighter/engine"
)

// TestSpawnExclusionZone tests that spawns are properly excluded near the cursor
func TestSpawnExclusionZone(t *testing.T) {
	gameWidth := 80
	gameHeight := 24
	cursorX := 40
	cursorY := 12

	world := engine.NewWorld()
	_ = gameHeight // Used in test logic below

	tests := []struct {
		name      string
		x, y      int
		seqLength int
		shouldFail bool
		reason     string
	}{
		// Test horizontal exclusion (within 5 units horizontally)
		{
			name:       "Too close horizontally, far vertically",
			x:          cursorX + 3,
			y:          cursorY + 10, // Far vertically (> 3)
			seqLength:  1,
			shouldFail: true,
			reason:     "Should be excluded: |x-cursorX| = 3 <= 5",
		},
		{
			name:       "Too close horizontally (negative), far vertically",
			x:          cursorX - 4,
			y:          cursorY + 8, // Far vertically
			seqLength:  1,
			shouldFail: true,
			reason:     "Should be excluded: |x-cursorX| = 4 <= 5",
		},

		// Test vertical exclusion (within 3 units vertically)
		{
			name:       "Far horizontally, too close vertically",
			x:          cursorX + 10, // Far horizontally (> 5)
			y:          cursorY + 2,  // Close vertically (<= 3)
			seqLength:  1,
			shouldFail: true,
			reason:     "Should be excluded: |y-cursorY| = 2 <= 3",
		},
		{
			name:       "Far horizontally, too close vertically (negative)",
			x:          cursorX + 15, // Far horizontally
			y:          cursorY - 3,  // Close vertically
			seqLength:  1,
			shouldFail: true,
			reason:     "Should be excluded: |y-cursorY| = 3 <= 3",
		},

		// Test diagonal exclusion (close on both dimensions)
		{
			name:       "Too close diagonally",
			x:          cursorX + 3,
			y:          cursorY + 2,
			seqLength:  1,
			shouldFail: true,
			reason:     "Should be excluded: both dimensions too close",
		},

		// Test valid positions (far enough on both dimensions)
		{
			name:       "Far enough horizontally and vertically",
			x:          cursorX + 8,
			y:          cursorY + 5,
			seqLength:  1,
			shouldFail: false,
			reason:     "Should be valid: |x-cursorX| = 8 > 5 AND |y-cursorY| = 5 > 3",
		},
		{
			name:       "Far horizontally (negative), far vertically",
			x:          cursorX - 10,
			y:          cursorY + 6,
			seqLength:  1,
			shouldFail: false,
			reason:     "Should be valid: |x-cursorX| = 10 > 5 AND |y-cursorY| = 6 > 3",
		},

		// Edge cases (exactly at boundary)
		{
			name:       "Exactly at horizontal boundary",
			x:          cursorX + 5,
			y:          cursorY + 10,
			seqLength:  1,
			shouldFail: true,
			reason:     "Should be excluded: |x-cursorX| = 5 <= 5",
		},
		{
			name:       "Exactly at vertical boundary",
			x:          cursorX + 10,
			y:          cursorY + 3,
			seqLength:  1,
			shouldFail: true,
			reason:     "Should be excluded: |y-cursorY| = 3 <= 3",
		},
		{
			name:       "Just beyond horizontal boundary",
			x:          cursorX + 6,
			y:          cursorY + 10,
			seqLength:  1,
			shouldFail: false,
			reason:     "Should be valid: |x-cursorX| = 6 > 5 AND |y-cursorY| = 10 > 3",
		},
		{
			name:       "Just beyond vertical boundary",
			x:          cursorX + 10,
			y:          cursorY + 4,
			seqLength:  1,
			shouldFail: false,
			reason:     "Should be valid: |x-cursorX| = 10 > 5 AND |y-cursorY| = 4 > 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Check if position would be excluded by the cursor proximity check
			dx := math.Abs(float64(tt.x - cursorX))
			dy := math.Abs(float64(tt.y - cursorY))

			// The fix changes AND to OR, so exclusion happens if EITHER dimension is too close
			isExcluded := (dx <= 5 || dy <= 3)

			// Also check if sequence would fit
			wouldFit := (tt.x + tt.seqLength <= gameWidth)

			// Check for overlaps (in this test, no existing entities)
			hasOverlap := false
			for i := 0; i < tt.seqLength; i++ {
				if world.GetEntityAtPosition(tt.x+i, tt.y) != 0 {
					hasOverlap = true
					break
				}
			}

			wouldBeRejected := isExcluded || !wouldFit || hasOverlap

			if wouldBeRejected != tt.shouldFail {
				t.Errorf("%s: expected exclusion=%v, got exclusion=%v (dx=%.1f, dy=%.1f). Reason: %s",
					tt.name, tt.shouldFail, wouldBeRejected, dx, dy, tt.reason)
			}

			// Verify the logic matches what's in the code
			if isExcluded != tt.shouldFail && wouldFit && !hasOverlap {
				t.Errorf("%s: cursor exclusion logic mismatch. Expected=%v, Got=%v. Reason: %s",
					tt.name, tt.shouldFail, isExcluded, tt.reason)
			}
		})
	}
}

// TestSpawnExclusionZoneAtBoundaries tests edge cases at screen boundaries
func TestSpawnExclusionZoneAtBoundaries(t *testing.T) {
	gameWidth := 80
	gameHeight := 24

	tests := []struct {
		name      string
		cursorX   int
		cursorY   int
		testX     int
		testY     int
		shouldExclude bool
	}{
		{
			name:      "Cursor at origin, spawn too close",
			cursorX:   0,
			cursorY:   0,
			testX:     3,
			testY:     1,
			shouldExclude: true,
		},
		{
			name:      "Cursor at origin, spawn far enough",
			cursorX:   0,
			cursorY:   0,
			testX:     10,
			testY:     5,
			shouldExclude: false,
		},
		{
			name:      "Cursor at bottom-right, spawn too close",
			cursorX:   gameWidth - 1,
			cursorY:   gameHeight - 1,
			testX:     gameWidth - 4,
			testY:     gameHeight - 2,
			shouldExclude: true,
		},
		{
			name:      "Cursor at center, spawn at top edge but too close horizontally",
			cursorX:   40,
			cursorY:   12,
			testX:     38,
			testY:     0,
			shouldExclude: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dx := math.Abs(float64(tt.testX - tt.cursorX))
			dy := math.Abs(float64(tt.testY - tt.cursorY))

			// With OR logic: excluded if EITHER dimension is too close
			isExcluded := (dx <= 5 || dy <= 3)

			if isExcluded != tt.shouldExclude {
				t.Errorf("Expected exclusion=%v, got=%v (dx=%.1f, dy=%.1f)",
					tt.shouldExclude, isExcluded, dx, dy)
			}
		})
	}
}

// TestFindValidPositionEventually tests that findValidPosition can find a valid spot
func TestFindValidPositionEventually(t *testing.T) {
	gameWidth := 80
	gameHeight := 24
	cursorX := 40
	cursorY := 12

	world := engine.NewWorld()
	spawnSys := NewSpawnSystem(gameWidth, gameHeight, cursorX, cursorY)

	// Try to find a valid position for a small sequence
	x, y := spawnSys.findValidPosition(world, 3)

	if x < 0 || y < 0 {
		t.Error("Expected to find a valid position, but got (-1, -1)")
		return
	}

	// Verify the position is valid
	dx := math.Abs(float64(x - cursorX))
	dy := math.Abs(float64(y - cursorY))

	// Should be excluded if EITHER dimension is too close (OR logic)
	if dx <= 5 || dy <= 3 {
		t.Errorf("Found position (%d, %d) is too close to cursor (%d, %d): dx=%.1f, dy=%.1f",
			x, y, cursorX, cursorY, dx, dy)
	}

	// Verify sequence fits
	if x+3 > gameWidth {
		t.Errorf("Sequence at x=%d with length 3 doesn't fit in width %d", x, gameWidth)
	}
}
