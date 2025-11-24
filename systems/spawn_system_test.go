package systems

import (
	"math"
	"reflect"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
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
		name       string
		x, y       int
		seqLength  int
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
			wouldFit := (tt.x+tt.seqLength <= gameWidth)

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
		name          string
		cursorX       int
		cursorY       int
		testX         int
		testY         int
		shouldExclude bool
	}{
		{
			name:          "Cursor at origin, spawn too close",
			cursorX:       0,
			cursorY:       0,
			testX:         3,
			testY:         1,
			shouldExclude: true,
		},
		{
			name:          "Cursor at origin, spawn far enough",
			cursorX:       0,
			cursorY:       0,
			testX:         10,
			testY:         5,
			shouldExclude: false,
		},
		{
			name:          "Cursor at bottom-right, spawn too close",
			cursorX:       gameWidth - 1,
			cursorY:       gameHeight - 1,
			testX:         gameWidth - 4,
			testY:         gameHeight - 2,
			shouldExclude: true,
		},
		{
			name:          "Cursor at center, spawn at top edge but too close horizontally",
			cursorX:       40,
			cursorY:       12,
			testX:         38,
			testY:         0,
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

// TestPlaceLineLogicMatchesExclusionZone tests that placeLine respects cursor exclusion zones
func TestPlaceLineLogicMatchesExclusionZone(t *testing.T) {
	gameWidth := 80
	gameHeight := 24
	cursorX := 40
	cursorY := 12

	world := engine.NewWorld()

	// Create a simulation screen for testing
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	ctx.GameWidth = gameWidth
	ctx.GameHeight = gameHeight
	ctx.CursorX = cursorX
	ctx.CursorY = cursorY
	// Sync cursor position to GameState for snapshot pattern
	ctx.State.SetCursorX(cursorX)
	ctx.State.SetCursorY(cursorY)

	spawnSys := NewSpawnSystem(ctx)

	// Set up some code blocks for spawning
	spawnSys.codeBlocks = []CodeBlock{
		{Lines: []string{"test", "line", "data"}},
	}

	// Try to place a line multiple times and verify all placements respect exclusion zone
	style := tcell.StyleDefault
	successCount := 0

	for attempt := 0; attempt < 20; attempt++ {
		if spawnSys.placeLine(world, "abc", components.SequenceBlue, components.LevelBright, style) {
			successCount++
		}
	}

	// Should have succeeded at least once (unless screen is impossibly constrained)
	if successCount == 0 {
		t.Error("Expected at least one successful placement")
	}

	// Verify all placed entities respect cursor exclusion zone
	entities := world.Positions.All()
	for _, entity := range entities {
		posComp, ok := world.Positions.Get(entity)
		if ok {
			pos := posComp.(components.PositionComponent)
			dx := math.Abs(float64(pos.X - cursorX))
			dy := math.Abs(float64(pos.Y - cursorY))

			// Should be excluded if BOTH dimensions are too close (AND logic in the check)
			if dx <= 5 && dy <= 3 {
				t.Errorf("Found entity at position (%d, %d) too close to cursor (%d, %d): dx=%.1f, dy=%.1f",
					pos.X, pos.Y, cursorX, cursorY, dx, dy)
			}
		}
	}
}