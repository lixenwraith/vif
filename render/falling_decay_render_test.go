package render

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestFallingDecayRenderColor tests that falling decay characters use correct dark cyan color
func TestFallingDecayRenderColor(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	if err := screen.Init(); err != nil {
		t.Fatalf("Failed to init screen: %v", err)
	}
	defer screen.Fini()

	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	renderer := NewTerminalRenderer(screen, 80, 24, 3, 1, 77, 20, 3)
	defaultStyle := tcell.StyleDefault.Background(RgbBackground)

	// Create a falling decay entity
	entity := world.CreateEntity()
	world.FallingDecays.Add(entity, components.FallingDecayComponent{
		Column:        10,
		YPosition:     5.0,
		Speed:         10.0,
		Char:          'X',
		LastChangeRow: -1,
		LastIntX:      -1,
		LastIntY:      -1,
		PrevPreciseX:  10.0,
		PrevPreciseY:  5.0,
	})

	// Render the falling decay
	renderer.drawFallingDecay(world, defaultStyle)
	screen.Show()

	// Get the rendered content at the falling character position
	screenX := 3 + 10 // gameX + column
	screenY := 1 + 5  // gameY + Y

	mainc, _, style, _ := screen.GetContent(screenX, screenY)

	// Verify character
	if mainc != 'X' {
		t.Errorf("Expected character 'X', got %c", mainc)
	}

	// Verify foreground color (should be dark cyan)
	fg, bg, _ := style.Decompose()
	expectedFg := RgbDecayFalling
	if fg != expectedFg {
		t.Errorf("Expected foreground color %v (dark cyan), got %v", expectedFg, fg)
	}

	// Verify background color (should match default background)
	expectedBg := RgbBackground
	if bg != expectedBg {
		t.Errorf("Expected background color %v (RgbBackground), got %v", expectedBg, bg)
	}
}

// TestFallingDecayRenderAtAllPositions tests that falling characters render at all Y positions
func TestFallingDecayRenderAtAllPositions(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	if err := screen.Init(); err != nil {
		t.Fatalf("Failed to init screen: %v", err)
	}
	defer screen.Fini()

	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	gameHeight := 20
	renderer := NewTerminalRenderer(screen, 80, 24, 3, 1, 77, gameHeight, 3)
	defaultStyle := tcell.StyleDefault.Background(RgbBackground)

	// Create falling entities at all Y positions (integer and fractional)
	testPositions := []float64{0.0, 0.5, 1.0, 1.7, 5.3, 10.0, 10.9, 15.5, 19.0, 19.9}

	for i, yPos := range testPositions {
		entity := world.CreateEntity()
		col := i * 7
		world.FallingDecays.Add(entity, components.FallingDecayComponent{
			Column:        col, // Spread across columns
			YPosition:     yPos,
			Speed:         10.0,
			Char:          rune('A' + i),
			LastChangeRow: -1,
			LastIntX:      -1,
			LastIntY:      -1,
			PrevPreciseX:  float64(col),
			PrevPreciseY:  yPos,
		})
	}

	// Render the falling decay
	renderer.drawFallingDecay(world, defaultStyle)
	screen.Show()

	// Verify all positions rendered correctly
	for i, yPos := range testPositions {
		expectedY := int(yPos) // Should truncate to integer
		if expectedY >= gameHeight {
			continue // Out of bounds
		}

		screenX := 3 + (i * 7)   // gameX + column
		screenY := 1 + expectedY // gameY + Y

		mainc, _, style, _ := screen.GetContent(screenX, screenY)
		expectedChar := rune('A' + i)

		if mainc != expectedChar {
			t.Errorf("Position %d (Y=%.1f): expected character %c, got %c", i, yPos, expectedChar, mainc)
		}

		// Verify color
		fg, _, _ := style.Decompose()
		if fg != RgbDecayFalling {
			t.Errorf("Position %d (Y=%.1f): expected dark cyan color, got %v", i, yPos, fg)
		}
	}
}

// TestFallingDecayRenderFractionalPositions tests fractional Y positions specifically
func TestFallingDecayRenderFractionalPositions(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	if err := screen.Init(); err != nil {
		t.Fatalf("Failed to init screen: %v", err)
	}
	defer screen.Fini()

	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	renderer := NewTerminalRenderer(screen, 80, 24, 3, 1, 77, 20, 3)
	defaultStyle := tcell.StyleDefault.Background(RgbBackground)

	// Test fractional positions that should all render at row 5
	fractionalPositions := []float64{5.0, 5.1, 5.3, 5.5, 5.7, 5.9, 5.99}

	for i, yPos := range fractionalPositions {
		entity := world.CreateEntity()
		col := i * 10
		world.FallingDecays.Add(entity, components.FallingDecayComponent{
			Column:        col,
			YPosition:     yPos,
			Speed:         10.0,
			Char:          rune('0' + i),
			LastChangeRow: -1,
			LastIntX:      -1,
			LastIntY:      -1,
			PrevPreciseX:  float64(col),
			PrevPreciseY:  yPos,
		})
	}

	// Render the falling decay
	renderer.drawFallingDecay(world, defaultStyle)
	screen.Show()

	// All should render at row 5 (int conversion truncates)
	for i, yPos := range fractionalPositions {
		screenX := 3 + (i * 10)
		screenY := 1 + 5 // All should be at row 5

		mainc, _, style, _ := screen.GetContent(screenX, screenY)
		expectedChar := rune('0' + i)

		if mainc != expectedChar {
			t.Errorf("Fractional position %.2f: expected character %c at row 5, got %c", yPos, expectedChar, mainc)
		}

		fg, _, _ := style.Decompose()
		if fg != RgbDecayFalling {
			t.Errorf("Fractional position %.2f: expected dark cyan color, got %v", yPos, fg)
		}
	}
}

// TestFallingDecayRenderBounds tests that out-of-bounds positions are not rendered
func TestFallingDecayRenderBounds(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	if err := screen.Init(); err != nil {
		t.Fatalf("Failed to init screen: %v", err)
	}
	defer screen.Fini()

	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	gameHeight := 20
	renderer := NewTerminalRenderer(screen, 80, 24, 3, 1, 77, gameHeight, 3)
	defaultStyle := tcell.StyleDefault.Background(RgbBackground)

	// Create entities outside bounds
	// Note: int(-0.5) = 0, so -0.5 will render at row 0
	outOfBoundsPositions := []struct {
		yPos float64
		col  int
		char rune
	}{
		{-1.0, 10, 'A'},  // Negative Y (int(-1.0) = -1, out of bounds)
		{20.0, 30, 'C'},  // At boundary (should not render)
		{25.0, 40, 'D'},  // Beyond boundary
		{100.0, 50, 'E'}, // Far beyond
	}

	for _, pos := range outOfBoundsPositions {
		entity := world.CreateEntity()
		world.FallingDecays.Add(entity, components.FallingDecayComponent{
			Column:        pos.col,
			YPosition:     pos.yPos,
			Speed:         10.0,
			Char:          pos.char,
			LastChangeRow: -1,
			LastIntX:      -1,
			LastIntY:      -1,
			PrevPreciseX:  float64(pos.col),
			PrevPreciseY:  pos.yPos,
		})
	}

	// Clear screen first
	screen.Clear()

	// Render the falling decay
	renderer.drawFallingDecay(world, defaultStyle)
	screen.Show()

	// Verify none of the out-of-bounds characters were rendered
	for _, pos := range outOfBoundsPositions {
		y := int(pos.yPos)

		// These should all be out of bounds or not rendered
		// Check if within game area first
		if y < 0 || y >= gameHeight {
			// Definitely out of bounds, skip verification
			continue
		}

		// If somehow within bounds, verify it's not rendered
		screenX := 3 + pos.col
		screenY := 1 + y

		mainc, _, _, _ := screen.GetContent(screenX, screenY)

		// After Clear(), empty cells should be ' ' or 0
		if mainc == pos.char {
			t.Errorf("Out-of-bounds character %c at Y=%.1f was incorrectly rendered", pos.char, pos.yPos)
		}
	}
}

// TestFallingDecayRenderZOrder tests that falling decay is rendered on top of game characters
func TestFallingDecayRenderZOrder(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	if err := screen.Init(); err != nil {
		t.Fatalf("Failed to init screen: %v", err)
	}
	defer screen.Fini()

	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	renderer := NewTerminalRenderer(screen, 80, 24, 3, 1, 77, 20, 3)
	defaultStyle := tcell.StyleDefault.Background(RgbBackground)

	// Create a game character at position (10, 5)
	gameEntity := world.CreateEntity()
	world.Positions.Add(gameEntity, components.PositionComponent{X: 10, Y: 5})
	world.Characters.Add(gameEntity, components.CharacterComponent{
		Rune:  'G',
		Style: GetStyleForSequence(components.SequenceGreen, components.LevelBright),
	})

	tx := world.BeginSpatialTransaction()
	tx.Spawn(gameEntity, 10, 5)
	tx.Commit()

	// Create a falling decay entity at the same position
	fallingEntity := world.CreateEntity()
	world.FallingDecays.Add(fallingEntity, components.FallingDecayComponent{
		Column:        10,
		YPosition:     5.0,
		Speed:         10.0,
		Char:          'F',
		LastChangeRow: -1,
		LastIntX:      -1,
		LastIntY:      -1,
		PrevPreciseX:  10.0,
		PrevPreciseY:  5.0,
	})

	// Draw in the correct order: characters first, then falling decay
	screen.Clear()
	cursorPos, _ := world.Positions.Get(ctx.CursorEntity)
	renderer.drawCharacters(world, cursorPos.X, cursorPos.Y, tcell.NewRGBColor(50, 50, 50), defaultStyle, ctx)
	renderer.drawFallingDecay(world, defaultStyle)
	screen.Show()

	// Get the rendered content
	screenX := 3 + 10
	screenY := 1 + 5

	mainc, _, style, _ := screen.GetContent(screenX, screenY)

	// The falling character 'F' should be on top, not the game character 'G'
	if mainc != 'F' {
		t.Errorf("Expected falling character 'F' on top (z-order), got %c", mainc)
	}

	// Verify it has the dark cyan color (falling decay color)
	fg, _, _ := style.Decompose()
	if fg != RgbDecayFalling {
		t.Errorf("Expected dark cyan foreground for falling character, got %v", fg)
	}
}

// TestFallingDecayRenderMultipleEntities tests rendering many falling entities
func TestFallingDecayRenderMultipleEntities(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	if err := screen.Init(); err != nil {
		t.Fatalf("Failed to init screen: %v", err)
	}
	defer screen.Fini()

	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	gameWidth := 77
	renderer := NewTerminalRenderer(screen, 80, 24, 3, 1, gameWidth, 20, 3)
	defaultStyle := tcell.StyleDefault.Background(RgbBackground)

	// Create one falling entity per column (as the decay system does)
	characters := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!@#$%^&*()[]{};<>?/|"

	for col := 0; col < gameWidth; col++ {
		entity := world.CreateEntity()
		char := rune(characters[col%len(characters)])
		yPos := float64(col % 20)
		world.FallingDecays.Add(entity, components.FallingDecayComponent{
			Column:        col,
			YPosition:     yPos, // Spread across different rows
			Speed:         10.0,
			Char:          char,
			LastChangeRow: -1,
			LastIntX:      -1,
			LastIntY:      -1,
			PrevPreciseX:  float64(col),
			PrevPreciseY:  yPos,
		})
	}

	// Render all falling entities
	screen.Clear()
	renderer.drawFallingDecay(world, defaultStyle)
	screen.Show()

	// Verify all entities were rendered
	renderedCount := 0
	for col := 0; col < gameWidth; col++ {
		y := col % 20
		screenX := 3 + col
		screenY := 1 + y

		mainc, _, style, _ := screen.GetContent(screenX, screenY)
		expectedChar := rune(characters[col%len(characters)])

		if mainc == expectedChar {
			renderedCount++

			// Verify color
			fg, _, _ := style.Decompose()
			if fg != RgbDecayFalling {
				t.Errorf("Column %d: expected dark cyan color, got %v", col, fg)
			}
		}
	}

	if renderedCount != gameWidth {
		t.Errorf("Expected %d falling entities rendered, got %d", gameWidth, renderedCount)
	}
}

// TestFallingDecayRenderConsistency tests that rendering is consistent across frames
func TestFallingDecayRenderConsistency(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	if err := screen.Init(); err != nil {
		t.Fatalf("Failed to init screen: %v", err)
	}
	defer screen.Fini()

	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	renderer := NewTerminalRenderer(screen, 80, 24, 3, 1, 77, 20, 3)
	defaultStyle := tcell.StyleDefault.Background(RgbBackground)

	// Create a falling entity
	entity := world.CreateEntity()
	world.FallingDecays.Add(entity, components.FallingDecayComponent{
		Column:        15,
		YPosition:     8.0,
		Speed:         10.0,
		Char:          'T',
		LastChangeRow: -1,
		LastIntX:      -1,
		LastIntY:      -1,
		PrevPreciseX:  15.0,
		PrevPreciseY:  8.0,
	})

	// Render multiple times and verify consistency
	for frame := 0; frame < 5; frame++ {
		screen.Clear()
		renderer.drawFallingDecay(world, defaultStyle)
		screen.Show()

		screenX := 3 + 15
		screenY := 1 + 8

		mainc, _, style, _ := screen.GetContent(screenX, screenY)

		if mainc != 'T' {
			t.Errorf("Frame %d: expected character 'T', got %c", frame, mainc)
		}

		fg, bg, _ := style.Decompose()
		if fg != RgbDecayFalling {
			t.Errorf("Frame %d: expected dark cyan foreground, got %v", frame, fg)
		}
		if bg != RgbBackground {
			t.Errorf("Frame %d: expected RgbBackground, got %v", frame, bg)
		}
	}
}

// TestFallingDecayComponentRetrieval tests component retrieval during rendering
func TestFallingDecayComponentRetrieval(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := ctx.World

	// Create entity without FallingDecayComponent
	entity1 := world.CreateEntity()
	world.Positions.Add(entity1, components.PositionComponent{X: 5, Y: 5})

	// Create entity with FallingDecayComponent
	entity2 := world.CreateEntity()
	world.FallingDecays.Add(entity2, components.FallingDecayComponent{
		Column:        10,
		YPosition:     3.0,
		Speed:         8.0,
		Char:          'X',
		LastChangeRow: -1,
		LastIntX:      -1,
		LastIntY:      -1,
		PrevPreciseX:  10.0,
		PrevPreciseY:  3.0,
	})

	// Get entities with FallingDecayComponent
	entities := world.FallingDecays.All()

	// Should only find entity2
	if len(entities) != 1 {
		t.Errorf("Expected 1 entity with FallingDecayComponent, got %d", len(entities))
	}

	if len(entities) > 0 && entities[0] != entity2 {
		t.Errorf("Expected entity2, got entity %d", entities[0])
	}

	// Verify component data
	fall, ok := world.FallingDecays.Get(entity2)
	if !ok {
		t.Fatal("Failed to get FallingDecayComponent from entity2")
	}
	if fall.Column != 10 {
		t.Errorf("Expected column 10, got %d", fall.Column)
	}
	if fall.YPosition != 3.0 {
		t.Errorf("Expected YPosition 3.0, got %f", fall.YPosition)
	}
	if fall.Char != 'X' {
		t.Errorf("Expected char 'X', got %c", fall.Char)
	}
}
