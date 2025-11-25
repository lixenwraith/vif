package modes

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// Helper function to create a test context
func createTestContext() *engine.GameContext {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	screen.SetSize(80, 24)

	ctx := engine.NewGameContext(screen)
	ctx.GameWidth = 80
	ctx.GameHeight = 20

	// Set cursor position in ECS (required after Phase 2 migration)
	setTestCursorPosition(ctx, 0, 0)
	return ctx
}

// Helper function to set cursor position in ECS (for testing after Phase 2 migration)
// Note: This is specific to motions_screen_test.go
func setTestCursorPosition(ctx *engine.GameContext, x, y int) {
	ctx.World.Positions.Add(ctx.CursorEntity, components.PositionComponent{X: x, Y: y})
}

// Helper function to place a character at a position
func placeChar(ctx *engine.GameContext, x, y int, r rune) {
	entity := ctx.World.CreateEntity()
	ctx.World.Positions.Add(entity, components.PositionComponent{X: x, Y: y})
	ctx.World.Characters.Add(entity, components.CharacterComponent{Rune: r, Style: tcell.StyleDefault})
	ctx.World.Sequences.Add(entity, components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceGreen,
		Level: components.LevelBright,
	})
}

func TestHMLMotions(t *testing.T) {
	ctx := createTestContext()
	setTestCursorPosition(ctx, 10, 10)

	// Test H - jump to top
	ExecuteMotion(ctx, 'H', 1)
	if getCursorY(ctx) != 0 {
		t.Errorf("H motion failed: expected Y=0, got Y=%d", getCursorY(ctx))
	}
	if getCursorX(ctx) != 10 {
		t.Errorf("H motion changed X: expected X=10, got X=%d", getCursorX(ctx))
	}

	// Test M - jump to middle
	setTestCursorPosition(ctx, getCursorX(ctx), 0)
	ExecuteMotion(ctx, 'M', 1)
	expectedMiddle := ctx.GameHeight / 2
	if getCursorY(ctx) != expectedMiddle {
		t.Errorf("M motion failed: expected Y=%d, got Y=%d", expectedMiddle, getCursorY(ctx))
	}

	// Test L - jump to bottom
	setTestCursorPosition(ctx, getCursorX(ctx), 0)
	ExecuteMotion(ctx, 'L', 1)
	expectedBottom := ctx.GameHeight - 1
	if getCursorY(ctx) != expectedBottom {
		t.Errorf("L motion failed: expected Y=%d, got Y=%d", expectedBottom, getCursorY(ctx))
	}
}

func TestCaretMotion(t *testing.T) {
	ctx := createTestContext()

	// Setup: "   hello" at y=0 (3 spaces before 'h')
	placeChar(ctx, 3, 0, 'h')
	placeChar(ctx, 4, 0, 'e')
	placeChar(ctx, 5, 0, 'l')
	placeChar(ctx, 6, 0, 'l')
	placeChar(ctx, 7, 0, 'o')

	// Test ^ - should jump to first non-whitespace
	setTestCursorPosition(ctx, 10, 0)
	ExecuteMotion(ctx, '^', 1)
	if getCursorX(ctx) != 3 {
		t.Errorf("^ motion failed: expected X=3, got X=%d", getCursorX(ctx))
	}

	// Test on empty line - should go to position 0
	setTestCursorPosition(ctx, 10, 1) // Empty line
	ExecuteMotion(ctx, '^', 1)
	if getCursorX(ctx) != 0 {
		t.Errorf("^ motion on empty line: expected X=0, got X=%d", getCursorX(ctx))
	}
}
