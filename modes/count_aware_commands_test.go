package modes

// Tests for count-aware command state management (MotionCount, PendingCount, state transitions).
// For comprehensive find motion functionality tests, see find_motion_test.go.

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestFindCharWithCount verifies that count-aware 'f' commands work correctly
func TestFindCharWithCount(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create a line with multiple 'a' characters: "baabaabaaa"
	// Positions: b=0, a=1, a=2, b=3, a=4, a=5, b=6, a=7, a=8, a=9
	createTestChar(ctx, 0, 10, 'b')
	createTestChar(ctx, 1, 10, 'a')
	createTestChar(ctx, 2, 10, 'a')
	createTestChar(ctx, 3, 10, 'b')
	createTestChar(ctx, 4, 10, 'a')
	createTestChar(ctx, 5, 10, 'a')
	createTestChar(ctx, 6, 10, 'b')
	createTestChar(ctx, 7, 10, 'a')
	createTestChar(ctx, 8, 10, 'a')
	createTestChar(ctx, 9, 10, 'a')

	// Set cursor at position 0, line 10
	setCursorPosition(ctx, 0, 10)

	tests := []struct {
		name                 string
		count                int
		targetChar           rune
		expectedX            int
		expectedPendingCount int
	}{
		{
			name:                 "fa finds first 'a'",
			count:                1,
			targetChar:           'a',
			expectedX:            1,
			expectedPendingCount: 0, // Should be cleared after execution
		},
		{
			name:                 "2fa finds second 'a'",
			count:                2,
			targetChar:           'a',
			expectedX:            2,
			expectedPendingCount: 0,
		},
		{
			name:                 "3fa finds third 'a'",
			count:                3,
			targetChar:           'a',
			expectedX:            4,
			expectedPendingCount: 0,
		},
		{
			name:                 "5fa finds fifth 'a'",
			count:                5,
			targetChar:           'a',
			expectedX:            7,
			expectedPendingCount: 0,
		},
		{
			name:                 "10fa moves to last match (only 7 'a's after cursor)",
			count:                10,
			targetChar:           'a',
			expectedX:            9, // Should move to last 'a' when count exceeds matches
			expectedPendingCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset cursor to start position
			setCursorPosition(ctx, 0, 10)
			ctx.PendingCount = 0
			ctx.MotionCount = 0

			// Simulate the command flow:
			// 1. User types count (e.g., "2")
			ctx.MotionCount = tt.count

			// 2. User types 'f'
			ctx.WaitingForF = true
			ctx.PendingCount = ctx.MotionCount

			// 3. User types target character (e.g., "a")
			ExecuteFindChar(ctx, tt.targetChar, ctx.PendingCount)

			// 4. Clear state
			ctx.WaitingForF = false
			ctx.PendingCount = 0
			ctx.MotionCount = 0

			// Verify cursor position
			if getCursorX(ctx) != tt.expectedX {
				t.Errorf("Expected cursor at X=%d, got X=%d", tt.expectedX, getCursorX(ctx))
			}

			// Verify PendingCount was cleared
			if ctx.PendingCount != tt.expectedPendingCount {
				t.Errorf("Expected PendingCount=%d, got %d", tt.expectedPendingCount, ctx.PendingCount)
			}
		})
	}
}

// TestFindCharStateTransition verifies count preservation through WaitingForF state
func TestFindCharStateTransition(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "xaxaxa"
	createTestChar(ctx, 0, 10, 'x')
	createTestChar(ctx, 1, 10, 'a')
	createTestChar(ctx, 2, 10, 'x')
	createTestChar(ctx, 3, 10, 'a')
	createTestChar(ctx, 4, 10, 'x')
	createTestChar(ctx, 5, 10, 'a')

	setCursorPosition(ctx, 0, 10)

	// Simulate "2fa" command
	// Step 1: User types "2"
	ctx.MotionCount = 2

	// Step 2: User types "f"
	if ctx.MotionCount != 2 {
		t.Errorf("MotionCount should be 2 before 'f', got %d", ctx.MotionCount)
	}

	ctx.WaitingForF = true
	ctx.PendingCount = ctx.MotionCount

	// Verify state after 'f'
	if !ctx.WaitingForF {
		t.Error("WaitingForF should be true after pressing 'f'")
	}
	if ctx.PendingCount != 2 {
		t.Errorf("PendingCount should be 2 after pressing 'f', got %d", ctx.PendingCount)
	}

	// Step 3: User types "a"
	ExecuteFindChar(ctx, 'a', ctx.PendingCount)
	ctx.WaitingForF = false
	ctx.PendingCount = 0
	ctx.MotionCount = 0

	// Verify final state
	if getCursorX(ctx) != 3 {
		t.Errorf("Expected cursor at X=3 (second 'a'), got X=%d", getCursorX(ctx))
	}
	if ctx.WaitingForF {
		t.Error("WaitingForF should be false after completion")
	}
	if ctx.PendingCount != 0 {
		t.Errorf("PendingCount should be 0 after completion, got %d", ctx.PendingCount)
	}
	if ctx.MotionCount != 0 {
		t.Errorf("MotionCount should be 0 after completion, got %d", ctx.MotionCount)
	}
}

// TestFindCharBackwardWithCount verifies that count-aware 'F' commands work correctly
func TestFindCharBackwardWithCount(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create a line with multiple 'a' characters: "aaabaabaab"
	// Positions: a=0, a=1, a=2, b=3, a=4, a=5, b=6, a=7, a=8, b=9
	createTestChar(ctx, 0, 10, 'a')
	createTestChar(ctx, 1, 10, 'a')
	createTestChar(ctx, 2, 10, 'a')
	createTestChar(ctx, 3, 10, 'b')
	createTestChar(ctx, 4, 10, 'a')
	createTestChar(ctx, 5, 10, 'a')
	createTestChar(ctx, 6, 10, 'b')
	createTestChar(ctx, 7, 10, 'a')
	createTestChar(ctx, 8, 10, 'a')
	createTestChar(ctx, 9, 10, 'b')

	// Set cursor at position 9, line 10 (at the end)
	setCursorPosition(ctx, 9, 10)

	tests := []struct {
		name                 string
		count                int
		targetChar           rune
		expectedX            int
		expectedPendingCount int
	}{
		{
			name:                 "Fa finds first 'a' backward",
			count:                1,
			targetChar:           'a',
			expectedX:            8,
			expectedPendingCount: 0,
		},
		{
			name:                 "2Fa finds second 'a' backward",
			count:                2,
			targetChar:           'a',
			expectedX:            7,
			expectedPendingCount: 0,
		},
		{
			name:                 "3Fa finds third 'a' backward",
			count:                3,
			targetChar:           'a',
			expectedX:            5,
			expectedPendingCount: 0,
		},
		{
			name:                 "5Fa finds fifth 'a' backward",
			count:                5,
			targetChar:           'a',
			expectedX:            2,
			expectedPendingCount: 0,
		},
		{
			name:                 "10Fa moves to first match (only 7 'a's before cursor)",
			count:                10,
			targetChar:           'a',
			expectedX:            0, // Should move to first 'a' when count exceeds matches
			expectedPendingCount: 0,
		},
		{
			name:                 "Fx finds 'x' (not found, cursor doesn't move)",
			count:                1,
			targetChar:           'x',
			expectedX:            9, // Cursor should not move when character not found
			expectedPendingCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset cursor to start position
			setCursorPosition(ctx, 9, 10)
			ctx.PendingCount = 0
			ctx.MotionCount = 0

			// Simulate the command flow:
			// 1. User types count (e.g., "2")
			ctx.MotionCount = tt.count

			// 2. User types 'F'
			ctx.WaitingForFBackward = true
			ctx.PendingCount = ctx.MotionCount

			// 3. User types target character (e.g., "a")
			ExecuteFindCharBackward(ctx, tt.targetChar, ctx.PendingCount)

			// 4. Clear state
			ctx.WaitingForFBackward = false
			ctx.PendingCount = 0
			ctx.MotionCount = 0

			// Verify cursor position
			if getCursorX(ctx) != tt.expectedX {
				t.Errorf("Expected cursor at X=%d, got X=%d", tt.expectedX, getCursorX(ctx))
			}

			// Verify PendingCount was cleared
			if ctx.PendingCount != tt.expectedPendingCount {
				t.Errorf("Expected PendingCount=%d, got %d", tt.expectedPendingCount, ctx.PendingCount)
			}
		})
	}
}

// TestFindCharBackwardStateTransition verifies count preservation through WaitingForFBackward state
func TestFindCharBackwardStateTransition(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line: "axaxax"
	createTestChar(ctx, 0, 10, 'a')
	createTestChar(ctx, 1, 10, 'x')
	createTestChar(ctx, 2, 10, 'a')
	createTestChar(ctx, 3, 10, 'x')
	createTestChar(ctx, 4, 10, 'a')
	createTestChar(ctx, 5, 10, 'x')

	setCursorPosition(ctx, 5, 10)

	// Simulate "2Fa" command
	// Step 1: User types "2"
	ctx.MotionCount = 2

	// Step 2: User types "F"
	if ctx.MotionCount != 2 {
		t.Errorf("MotionCount should be 2 before 'F', got %d", ctx.MotionCount)
	}

	ctx.WaitingForFBackward = true
	ctx.PendingCount = ctx.MotionCount

	// Verify state after 'F'
	if !ctx.WaitingForFBackward {
		t.Error("WaitingForFBackward should be true after pressing 'F'")
	}
	if ctx.PendingCount != 2 {
		t.Errorf("PendingCount should be 2 after pressing 'F', got %d", ctx.PendingCount)
	}

	// Step 3: User types "a"
	ExecuteFindCharBackward(ctx, 'a', ctx.PendingCount)
	ctx.WaitingForFBackward = false
	ctx.PendingCount = 0
	ctx.MotionCount = 0

	// Verify final state
	if getCursorX(ctx) != 2 {
		t.Errorf("Expected cursor at X=2 (second 'a' backward), got X=%d", getCursorX(ctx))
	}
	if ctx.WaitingForFBackward {
		t.Error("WaitingForFBackward should be false after completion")
	}
	if ctx.PendingCount != 0 {
		t.Errorf("PendingCount should be 0 after completion, got %d", ctx.PendingCount)
	}
	if ctx.MotionCount != 0 {
		t.Errorf("MotionCount should be 0 after completion, got %d", ctx.MotionCount)
	}
}

// TestSingleKeystrokeCommandsStillWork verifies no regression in single-keystroke commands
func TestSingleKeystrokeCommandsStillWork(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	tests := []struct {
		name      string
		command   rune
		count     int
		initialX  int
		initialY  int
		expectedX int
		expectedY int
	}{
		{
			name:      "5j moves down 5 lines",
			command:   'j',
			count:     5,
			initialX:  10,
			initialY:  5,
			expectedX: 10,
			expectedY: 10,
		},
		{
			name:      "3l moves right 3 positions",
			command:   'l',
			count:     3,
			initialX:  10,
			initialY:  5,
			expectedX: 13,
			expectedY: 5,
		},
		{
			name:      "2k moves up 2 lines",
			command:   'k',
			count:     2,
			initialX:  10,
			initialY:  5,
			expectedX: 10,
			expectedY: 3,
		},
		{
			name:      "4h moves left 4 positions",
			command:   'h',
			count:     4,
			initialX:  10,
			initialY:  5,
			expectedX: 6,
			expectedY: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setCursorPosition(ctx, tt.initialX, tt.initialY)
			ctx.MotionCount = 0
			ctx.PendingCount = 0

			// Execute motion
			ExecuteMotion(ctx, tt.command, tt.count)

			if getCursorX(ctx) != tt.expectedX {
				t.Errorf("Expected X=%d, got X=%d", tt.expectedX, getCursorX(ctx))
			}
			if getCursorY(ctx) != tt.expectedY {
				t.Errorf("Expected Y=%d, got Y=%d", tt.expectedY, getCursorY(ctx))
			}

			// Verify PendingCount was not affected
			if ctx.PendingCount != 0 {
				t.Errorf("PendingCount should remain 0 for single-keystroke commands, got %d", ctx.PendingCount)
			}
		})
	}
}

// TestInvalidCommandResetsCount verifies that invalid commands reset count state
func TestInvalidCommandResetsCount(t *testing.T) {
	ctx := createMinimalTestContext(80, 24)

	// Create test line with one 'a'
	createTestChar(ctx, 5, 10, 'a')

	setCursorPosition(ctx, 0, 10)

	// Simulate "5fx" where 'x' doesn't exist (should not move cursor)
	ctx.MotionCount = 5
	ctx.WaitingForF = true
	ctx.PendingCount = 5

	// Try to find 'x' (doesn't exist)
	ExecuteFindChar(ctx, 'x', ctx.PendingCount)

	// Cursor should not move
	if getCursorX(ctx) != 0 {
		t.Errorf("Cursor should not move when character not found, got X=%d", getCursorX(ctx))
	}

	// Clean up state
	ctx.WaitingForF = false
	ctx.PendingCount = 0
	ctx.MotionCount = 0

	// Verify state is reset
	if ctx.PendingCount != 0 {
		t.Errorf("PendingCount should be reset to 0, got %d", ctx.PendingCount)
	}
}

// TestCommandCapabilities verifies the CommandCapability system
func TestCommandCapabilities(t *testing.T) {
	tests := []struct {
		command        rune
		acceptsCount   bool
		multiKeystroke bool
		requiresMotion bool
	}{
		{'f', true, true, false},
		{'F', true, true, false},
		{'h', true, false, false},
		{'j', true, false, false},
		{'k', true, false, false},
		{'l', true, false, false},
		{'w', true, false, false},
		{'d', true, true, true},
		{'x', true, false, false},
		{'i', false, false, false},
		{'/', false, false, false},
		{'0', false, false, false},
		{'^', false, false, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.command), func(t *testing.T) {
			cap := GetCommandCapability(tt.command)

			if cap.AcceptsCount != tt.acceptsCount {
				t.Errorf("Command '%c' AcceptsCount: expected %v, got %v",
					tt.command, tt.acceptsCount, cap.AcceptsCount)
			}
			if cap.MultiKeystroke != tt.multiKeystroke {
				t.Errorf("Command '%c' MultiKeystroke: expected %v, got %v",
					tt.command, tt.multiKeystroke, cap.MultiKeystroke)
			}
			if cap.RequiresMotion != tt.requiresMotion {
				t.Errorf("Command '%c' RequiresMotion: expected %v, got %v",
					tt.command, tt.requiresMotion, cap.RequiresMotion)
			}
		})
	}
}

// Helper function to create minimal test context for count tests (no screen simulation)
func createMinimalTestContext(width, height int) *engine.GameContext {
	world := engine.NewWorld()
	ctx := &engine.GameContext{
		World:      world,
		GameWidth:  width,
		GameHeight: height,
	}

	// Create cursor entity (required after Phase 2 migration)
	ctx.CursorEntity = engine.With(
		engine.WithPosition(
			world.NewEntity(),
			world.Positions,
			components.PositionComponent{X: width / 2, Y: height / 2},
		),
		world.Cursors,
		components.CursorComponent{},
	).Build()

	// Make cursor indestructible
	world.Protections.Add(ctx.CursorEntity, components.ProtectionComponent{
		Mask:      components.ProtectAll,
		ExpiresAt: 0,
	})

	// Initialize cursor cache (synced with ECS)
	if pos, ok := world.Positions.Get(ctx.CursorEntity); ok {
		setCursorPosition(ctx, pos.X, pos.Y)
	}

	return ctx
}

// Helper function to create a character at a position for testing
func createTestChar(ctx *engine.GameContext, x, y int, char rune) engine.Entity {
	entity := ctx.World.CreateEntity()
	ctx.World.Positions.Add(entity, components.PositionComponent{X: x, Y: y})
	ctx.World.Characters.Add(entity, components.CharacterComponent{Rune: char})
	return entity
}

// Helper to set cursor position removed - now using shared helper from test_helpers.go
