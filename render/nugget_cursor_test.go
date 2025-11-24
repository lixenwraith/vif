package render

import (
	"reflect"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestNuggetDarkensUnderCursor verifies that nugget character has dark foreground when cursor is on it
func TestNuggetDarkensUnderCursor(t *testing.T) {
	// Create test screen and context
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(100, 30)
	if err := screen.Init(); err != nil {
		t.Fatalf("Failed to init screen: %v", err)
	}
	defer screen.Fini()

	ctx := engine.NewGameContext(screen)
	world := ctx.World
	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime
	ctx.CursorX = 10
	ctx.CursorY = 5
	ctx.Mode = engine.ModeNormal

	// Create nugget at cursor position
	nuggetEntity := world.CreateEntity()
	world.Positions.Add(nuggetEntity, components.PositionComponent{X: 10, Y: 5})
	world.Characters.Add(nuggetEntity, components.CharacterComponent{
		Rune:  '●',
		Style: tcell.StyleDefault.Foreground(RgbNuggetOrange),
	})
	world.Nuggets.Add(nuggetEntity, components.NuggetComponent{
		ID:        1,
		SpawnTime: time.Now(),
	})

	tx := world.BeginSpatialTransaction()
	tx.Spawn(nuggetEntity, 10, 5)
	tx.Commit()

	// Create renderer
	renderer := NewTerminalRenderer(screen, 100, 30, 3, 1, 100, 24, 3)

	// Draw cursor
	defaultStyle := tcell.StyleDefault.Background(RgbBackground)
	renderer.drawCursor(ctx, defaultStyle)

	// Get the cell content at cursor position
	screenX := 3 + 10
	screenY := 1 + 5
	mainc, _, style, _ := screen.GetContent(screenX, screenY)

	// Verify character is nugget character
	if mainc != '●' {
		t.Errorf("Expected nugget character '●', got '%c'", mainc)
	}

	// Verify foreground is dark brown (RgbNuggetDark)
	fg, bg, _ := style.Decompose()
	if fg != RgbNuggetDark {
		t.Errorf("Expected dark nugget foreground color, got %v", fg)
	}

	// Verify background is orange (nugget color)
	if bg != RgbNuggetOrange {
		t.Errorf("Expected orange background (nugget color), got %v", bg)
	}
}

// TestNormalCharacterStaysBlackUnderCursor verifies normal characters keep black foreground under cursor
func TestNormalCharacterStaysBlackUnderCursor(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(100, 30)
	if err := screen.Init(); err != nil {
		t.Fatalf("Failed to init screen: %v", err)
	}
	defer screen.Fini()

	ctx := engine.NewGameContext(screen)
	world := ctx.World
	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime
	ctx.CursorX = 15
	ctx.CursorY = 10
	ctx.Mode = engine.ModeNormal

	// Create normal green character at cursor position
	charEntity := world.CreateEntity()
	world.Positions.Add(charEntity, components.PositionComponent{X: 15, Y: 10})
	world.Characters.Add(charEntity, components.CharacterComponent{
		Rune:  'a',
		Style: tcell.StyleDefault.Foreground(RgbSequenceGreenBright),
	})
	world.Sequences.Add(charEntity, components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceGreen,
		Level: components.LevelBright,
	})

	tx := world.BeginSpatialTransaction()
	tx.Spawn(charEntity, 15, 10)
	tx.Commit()

	renderer := NewTerminalRenderer(screen, 100, 30, 3, 1, 100, 24, 3)
	defaultStyle := tcell.StyleDefault.Background(RgbBackground)
	renderer.drawCursor(ctx, defaultStyle)

	screenX := 3 + 15
	screenY := 1 + 10
	mainc, _, style, _ := screen.GetContent(screenX, screenY)

	if mainc != 'a' {
		t.Errorf("Expected character 'a', got '%c'", mainc)
	}

	// Verify foreground is black (normal behavior)
	fg, bg, _ := style.Decompose()
	if fg != tcell.ColorBlack {
		t.Errorf("Expected black foreground for normal character, got %v", fg)
	}

	// Verify background is green (character color)
	if bg != RgbSequenceGreenBright {
		t.Errorf("Expected green background, got %v", bg)
	}
}

// TestCursorWithoutCharacterHasNoContrast verifies cursor without character uses standard colors
func TestCursorWithoutCharacterHasNoContrast(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(100, 30)
	if err := screen.Init(); err != nil {
		t.Fatalf("Failed to init screen: %v", err)
	}
	defer screen.Fini()

	ctx := engine.NewGameContext(screen)
	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime
	ctx.CursorX = 20
	ctx.CursorY = 8
	ctx.Mode = engine.ModeNormal

	renderer := NewTerminalRenderer(screen, 100, 30, 3, 1, 100, 24, 3)
	defaultStyle := tcell.StyleDefault.Background(RgbBackground)
	renderer.drawCursor(ctx, defaultStyle)

	screenX := 3 + 20
	screenY := 1 + 8
	mainc, _, style, _ := screen.GetContent(screenX, screenY)

	// Verify character is space
	if mainc != ' ' {
		t.Errorf("Expected space character, got '%c'", mainc)
	}

	// Verify colors are standard cursor colors (orange for normal mode)
	fg, bg, _ := style.Decompose()
	if fg != tcell.ColorBlack {
		t.Errorf("Expected black foreground, got %v", fg)
	}
	if bg != RgbCursorNormal {
		t.Errorf("Expected orange background (normal cursor), got %v", bg)
	}
}

// TestNuggetContrastInInsertMode verifies dark foreground works in insert mode too
func TestNuggetContrastInInsertMode(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(100, 30)
	if err := screen.Init(); err != nil {
		t.Fatalf("Failed to init screen: %v", err)
	}
	defer screen.Fini()

	ctx := engine.NewGameContext(screen)
	world := ctx.World
	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime
	ctx.CursorX = 25
	ctx.CursorY = 12
	ctx.Mode = engine.ModeInsert

	// Create nugget at cursor position
	nuggetEntity := world.CreateEntity()
	world.Positions.Add(nuggetEntity, components.PositionComponent{X: 25, Y: 12})
	world.Characters.Add(nuggetEntity, components.CharacterComponent{
		Rune:  '●',
		Style: tcell.StyleDefault.Foreground(RgbNuggetOrange),
	})
	world.Nuggets.Add(nuggetEntity, components.NuggetComponent{
		ID:        2,
		SpawnTime: time.Now(),
	})

	tx := world.BeginSpatialTransaction()
	tx.Spawn(nuggetEntity, 25, 12)
	tx.Commit()

	renderer := NewTerminalRenderer(screen, 100, 30, 3, 1, 100, 24, 3)
	defaultStyle := tcell.StyleDefault.Background(RgbBackground)
	renderer.drawCursor(ctx, defaultStyle)

	screenX := 3 + 25
	screenY := 1 + 12
	_, _, style, _ := screen.GetContent(screenX, screenY)

	// Verify foreground is dark brown (even in insert mode)
	fg, bg, _ := style.Decompose()
	if fg != RgbNuggetDark {
		t.Errorf("Expected dark nugget foreground in insert mode, got %v", fg)
	}

	// Verify background is orange (nugget color, not white insert cursor)
	if bg != RgbNuggetOrange {
		t.Errorf("Expected orange background (nugget color), got %v", bg)
	}
}

// TestNuggetOffCursorHasNormalColor verifies nugget away from cursor has orange foreground
func TestNuggetOffCursorHasNormalColor(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(100, 30)
	if err := screen.Init(); err != nil {
		t.Fatalf("Failed to init screen: %v", err)
	}
	defer screen.Fini()

	ctx := engine.NewGameContext(screen)
	world := ctx.World
	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime
	ctx.CursorX = 10
	ctx.CursorY = 5

	// Create nugget NOT at cursor position
	nuggetEntity := world.CreateEntity()
	world.Positions.Add(nuggetEntity, components.PositionComponent{X: 30, Y: 15})
	world.Characters.Add(nuggetEntity, components.CharacterComponent{
		Rune:  '●',
		Style: tcell.StyleDefault.Foreground(RgbNuggetOrange),
	})
	world.Nuggets.Add(nuggetEntity, components.NuggetComponent{
		ID:        3,
		SpawnTime: time.Now(),
	})

	tx := world.BeginSpatialTransaction()
	tx.Spawn(nuggetEntity, 30, 15)
	tx.Commit()

	renderer := NewTerminalRenderer(screen, 100, 30, 3, 1, 100, 24, 3)
	defaultStyle := tcell.StyleDefault.Background(RgbBackground)

	// Draw characters (not cursor)
	renderer.drawCharacters(world, tcell.NewRGBColor(5, 5, 5), defaultStyle, ctx)

	// Get the nugget cell content (not at cursor)
	screenX := 3 + 30
	screenY := 1 + 15
	mainc, _, style, _ := screen.GetContent(screenX, screenY)

	if mainc != '●' {
		t.Errorf("Expected nugget character '●', got '%c'", mainc)
	}

	// Verify foreground is orange (normal nugget color, not dark)
	fg, _, _ := style.Decompose()
	if fg != RgbNuggetOrange {
		t.Errorf("Expected orange foreground when not under cursor, got %v", fg)
	}
}

// TestCursorErrorOverridesNuggetContrast verifies error cursor takes precedence
func TestCursorErrorOverridesNuggetContrast(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(100, 30)
	if err := screen.Init(); err != nil {
		t.Fatalf("Failed to init screen: %v", err)
	}
	defer screen.Fini()

	ctx := engine.NewGameContext(screen)
	world := ctx.World
	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime
	ctx.CursorX = 40
	ctx.CursorY = 18
	ctx.Mode = engine.ModeInsert

	// Trigger error cursor
	ctx.State.SetCursorError(true)
	ctx.State.SetCursorErrorTime(ctx.TimeProvider.Now())

	// Create nugget at cursor position
	nuggetEntity := world.CreateEntity()
	world.Positions.Add(nuggetEntity, components.PositionComponent{X: 40, Y: 18})
	world.Characters.Add(nuggetEntity, components.CharacterComponent{
		Rune:  '●',
		Style: tcell.StyleDefault.Foreground(RgbNuggetOrange),
	})
	world.Nuggets.Add(nuggetEntity, components.NuggetComponent{
		ID:        4,
		SpawnTime: time.Now(),
	})

	tx := world.BeginSpatialTransaction()
	tx.Spawn(nuggetEntity, 40, 18)
	tx.Commit()

	renderer := NewTerminalRenderer(screen, 100, 30, 3, 1, 100, 24, 3)
	defaultStyle := tcell.StyleDefault.Background(RgbBackground)
	renderer.drawCursor(ctx, defaultStyle)

	screenX := 3 + 40
	screenY := 1 + 18
	_, _, style, _ := screen.GetContent(screenX, screenY)

	// Verify error cursor takes precedence
	fg, bg, _ := style.Decompose()
	if fg != tcell.ColorBlack {
		t.Errorf("Expected black foreground for error cursor, got %v", fg)
	}
	if bg != RgbCursorError {
		t.Errorf("Expected red background for error cursor, got %v", bg)
	}
}

// TestNuggetComponentDetectionLogic verifies component detection works correctly
func TestNuggetComponentDetectionLogic(t *testing.T) {
	world := engine.NewWorld()

	// Create nugget entity
	nuggetEntity := world.CreateEntity()
	world.Positions.Add(nuggetEntity, components.PositionComponent{X: 5, Y: 5})
	world.Characters.Add(nuggetEntity, components.CharacterComponent{
		Rune:  '●',
		Style: tcell.StyleDefault.Foreground(RgbNuggetOrange),
	})
	world.Nuggets.Add(nuggetEntity, components.NuggetComponent{
		ID:        5,
		SpawnTime: time.Now(),
	})

 {
	tx := world.BeginSpatialTransaction()
	tx.Spawn(nuggetEntity, 5, 5)
	tx.Commit()
 }

	// Create normal character entity
	charEntity := world.CreateEntity()
	world.Positions.Add(charEntity, components.PositionComponent{X: 10, Y: 10})
	world.Characters.Add(charEntity, components.CharacterComponent{
		Rune:  'x',
		Style: tcell.StyleDefault.Foreground(RgbSequenceGreenNormal),
	})
	world.Sequences.Add(charEntity, components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceGreen,
		Level: components.LevelNormal,
	})

 {
	tx := world.BeginSpatialTransaction()
	tx.Spawn(charEntity, 10, 10)
	tx.Commit()
 }

	// Test nugget entity has NuggetComponent
	if _, ok := world.Nuggets.Get(nuggetEntity); !ok {
		t.Error("Nugget entity should have NuggetComponent")
	}

	// Test normal entity does NOT have NuggetComponent
	if _, ok := world.Nuggets.Get(charEntity); ok {
		t.Error("Normal character entity should NOT have NuggetComponent")
	}
}

// TestNuggetLayeringCursorOnTop verifies cursor is rendered on top of nugget
func TestNuggetLayeringCursorOnTop(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(100, 30)
	if err := screen.Init(); err != nil {
		t.Fatalf("Failed to init screen: %v", err)
	}
	defer screen.Fini()

	ctx := engine.NewGameContext(screen)
	world := ctx.World
	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime
	ctx.CursorX = 50
	ctx.CursorY = 20
	ctx.Mode = engine.ModeNormal

	// Create nugget at cursor position
	nuggetEntity := world.CreateEntity()
	world.Positions.Add(nuggetEntity, components.PositionComponent{X: 50, Y: 20})
	world.Characters.Add(nuggetEntity, components.CharacterComponent{
		Rune:  '●',
		Style: tcell.StyleDefault.Foreground(RgbNuggetOrange),
	})
	world.Nuggets.Add(nuggetEntity, components.NuggetComponent{
		ID:        6,
		SpawnTime: time.Now(),
	})

	tx := world.BeginSpatialTransaction()
	tx.Spawn(nuggetEntity, 50, 20)
	tx.Commit()

	renderer := NewTerminalRenderer(screen, 100, 30, 3, 1, 100, 24, 3)
	defaultStyle := tcell.StyleDefault.Background(RgbBackground)

	// Draw characters first
	renderer.drawCharacters(world, tcell.NewRGBColor(5, 5, 5), defaultStyle, ctx)

	// Get content after drawing characters
	screenX := 3 + 50
	screenY := 1 + 20
	_, _, styleBeforeCursor, _ := screen.GetContent(screenX, screenY)
	fgBefore, _, _ := styleBeforeCursor.Decompose()

	// Verify nugget has orange foreground (not dark) before cursor is drawn
	if fgBefore != RgbNuggetOrange {
		t.Errorf("Before cursor draw, expected orange foreground, got %v", fgBefore)
	}

	// Draw cursor on top
	renderer.drawCursor(ctx, defaultStyle)

	// Get content after drawing cursor
	_, _, styleAfterCursor, _ := screen.GetContent(screenX, screenY)
	fgAfter, bgAfter, _ := styleAfterCursor.Decompose()

	// Verify cursor overwrites with dark foreground and orange background
	if fgAfter != RgbNuggetDark {
		t.Errorf("After cursor draw, expected dark foreground, got %v", fgAfter)
	}
	if bgAfter != RgbNuggetOrange {
		t.Errorf("After cursor draw, expected orange background, got %v", bgAfter)
	}
}

// TestMultipleNuggetInstances verifies each nugget gets dark foreground when cursor is on it
func TestMultipleNuggetInstances(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(100, 30)
	if err := screen.Init(); err != nil {
		t.Fatalf("Failed to init screen: %v", err)
	}
	defer screen.Fini()

	ctx := engine.NewGameContext(screen)
	world := ctx.World
	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	// Create multiple nuggets
	positions := []struct{ x, y int }{
		{10, 5},
		{30, 8},
		{50, 12},
	}

	for i, pos := range positions {
		nuggetEntity := world.CreateEntity()
		world.Positions.Add(nuggetEntity, components.PositionComponent{X: pos.x, Y: pos.y})
		world.Characters.Add(nuggetEntity, components.CharacterComponent{
			Rune:  '●',
			Style: tcell.StyleDefault.Foreground(RgbNuggetOrange),
		})
		world.Nuggets.Add(nuggetEntity, components.NuggetComponent{
			ID:        i + 10,
			SpawnTime: time.Now(),
		})

		tx := world.BeginSpatialTransaction()
		tx.Spawn(nuggetEntity, pos.x, pos.y)
		tx.Commit()
	}

	renderer := NewTerminalRenderer(screen, 100, 30, 3, 1, 100, 24, 3)
	defaultStyle := tcell.StyleDefault.Background(RgbBackground)

	// Test each nugget position
	for _, pos := range positions {
		// Move cursor to nugget
		ctx.CursorX = pos.x
		ctx.CursorY = pos.y

		// Clear screen and redraw
		screen.Clear()
		renderer.drawCursor(ctx, defaultStyle)

		screenX := 3 + pos.x
		screenY := 1 + pos.y
		_, _, style, _ := screen.GetContent(screenX, screenY)

		fg, bg, _ := style.Decompose()
		if fg != RgbNuggetDark {
			t.Errorf("At position (%d,%d), expected dark foreground, got %v", pos.x, pos.y, fg)
		}
		if bg != RgbNuggetOrange {
			t.Errorf("At position (%d,%d), expected orange background, got %v", pos.x, pos.y, bg)
		}
	}
}
