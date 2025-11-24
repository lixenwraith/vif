package systems

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestBoostHeatMultiplier verifies that boost doubles heat gain
func TestBoostHeatMultiplier(t *testing.T) {
	// Create mock screen
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	// Create game context
	ctx := engine.NewGameContext(screen)
	scoreSystem := NewScoreSystem(ctx)

	// Create a green character at cursor position
	entity := ctx.World.CreateEntity()
	pos := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
	char := components.CharacterComponent{Rune: 'a', Style: tcell.StyleDefault}
	seq := components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceGreen,
		Level: components.LevelNormal,
	}

	// Spatial transaction handles PositionComponent
	ctx.World.Characters.Add(entity, char)
	ctx.World.Sequences.Add(entity, seq)
	{
		tx := ctx.World.BeginSpatialTransaction()
		tx.Spawn(entity, pos.X, pos.Y)
		tx.Commit()
	}

	// Test 1: Without boost, heat should increment by 1
	ctx.State.SetBoostEnabled(false)
	ctx.State.SetHeat(0)
	initialScore := ctx.State.GetScore()

	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'a')

	heat := ctx.State.GetHeat()
	if heat != 1 {
		t.Errorf("Expected heat to be 1 without boost, got %d", heat)
	}

	// Calculate expected score: heat * level_multiplier = 1 * 2 = 2
	expectedScore := initialScore + 2
	finalScore := ctx.State.GetScore()
	if finalScore != expectedScore {
		t.Errorf("Expected score %d, got %d", expectedScore, finalScore)
	}

	// Test 2: Create another character for boost test
	entity2 := ctx.World.CreateEntity()
	pos2 := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
	char2 := components.CharacterComponent{Rune: 'b', Style: tcell.StyleDefault}
	seq2 := components.SequenceComponent{
		ID:    2,
		Index: 0,
		Type:  components.SequenceGreen,
		Level: components.LevelNormal,
	}

	ctx.World.Positions.Add(entity2, pos2)
	ctx.World.Characters.Add(entity2, char2)
	ctx.World.Sequences.Add(entity2, seq2)
	{
		tx := ctx.World.BeginSpatialTransaction()
		tx.Spawn(entity2, pos2.X, pos2.Y)
		tx.Commit()
	}

	// Enable boost with matching color (2 = Green)
	ctx.State.SetBoostEnabled(true)
	ctx.State.SetBoostColor(2)
	ctx.State.SetHeat(1) // Reset to starting heat
	initialScore = ctx.State.GetScore()

	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'b')

	heat = ctx.State.GetHeat()
	if heat != 3 {
		t.Errorf("Expected heat to be 3 with boost (1 + 2), got %d", heat)
	}

	// Calculate expected score: heat * level_multiplier = 3 * 2 = 6
	expectedScore = initialScore + 6
	finalScore = ctx.State.GetScore()
	if finalScore != expectedScore {
		t.Errorf("Expected score %d with boost, got %d", expectedScore, finalScore)
	}
}

// TestRedCharacterResetsHeat verifies red characters reset heat
func TestRedCharacterResetsHeat(t *testing.T) {
	// Create mock screen
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	// Create game context
	ctx := engine.NewGameContext(screen)
	scoreSystem := NewScoreSystem(ctx)

	// Set heat to non-zero
	ctx.State.SetHeat(50)

	// Create a red character at cursor position
	entity := ctx.World.CreateEntity()
	pos := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
	char := components.CharacterComponent{Rune: 'r', Style: tcell.StyleDefault}
	seq := components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceRed,
		Level: components.LevelNormal,
	}

	// Spatial transaction handles PositionComponent
	ctx.World.Characters.Add(entity, char)
	ctx.World.Sequences.Add(entity, seq)
	{
		tx := ctx.World.BeginSpatialTransaction()
		tx.Spawn(entity, pos.X, pos.Y)
		tx.Commit()
	}

	// Type the red character
	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'r')

	// Heat should be reset to 0
	heat := ctx.State.GetHeat()
	if heat != 0 {
		t.Errorf("Expected heat to be 0 after red character, got %d", heat)
	}
}

// TestGreenBrightCharacterScore verifies Bright green characters give correct points
func TestGreenBrightCharacterScore(t *testing.T) {
	// Create mock screen
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	// Create game context
	ctx := engine.NewGameContext(screen)
	scoreSystem := NewScoreSystem(ctx)

	// Set heat to 5
	ctx.State.SetHeat(5)
	initialScore := ctx.State.GetScore()

	// Create a bright green character at cursor position
	entity := ctx.World.CreateEntity()
	pos := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
	char := components.CharacterComponent{Rune: 'g', Style: tcell.StyleDefault}
	seq := components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceGreen,
		Level: components.LevelBright,
	}

	// Spatial transaction handles PositionComponent
	ctx.World.Characters.Add(entity, char)
	ctx.World.Sequences.Add(entity, seq)
	{
		tx := ctx.World.BeginSpatialTransaction()
		tx.Spawn(entity, pos.X, pos.Y)
		tx.Commit()
	}

	// Type the character
	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'g')

	// Heat should increment to 6
	heat := ctx.State.GetHeat()
	if heat != 6 {
		t.Errorf("Expected heat to be 6, got %d", heat)
	}

	// Score should increase by heat * level_multiplier = 6 * 3 = 18
	expectedScore := initialScore + 18
	finalScore := ctx.State.GetScore()
	if finalScore != expectedScore {
		t.Errorf("Expected score %d, got %d", expectedScore, finalScore)
	}
}

// TestIncorrectCharacterResetsHeat verifies incorrect characters reset heat
func TestIncorrectCharacterResetsHeat(t *testing.T) {
	// Create mock screen
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	// Create game context
	ctx := engine.NewGameContext(screen)
	scoreSystem := NewScoreSystem(ctx)

	// Set heat to non-zero
	ctx.State.SetHeat(25)

	// Create a green character at cursor position
	entity := ctx.World.CreateEntity()
	pos := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
	char := components.CharacterComponent{Rune: 'a', Style: tcell.StyleDefault}
	seq := components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceGreen,
		Level: components.LevelNormal,
	}

	// Spatial transaction handles PositionComponent
	ctx.World.Characters.Add(entity, char)
	ctx.World.Sequences.Add(entity, seq)
	{
		tx := ctx.World.BeginSpatialTransaction()
		tx.Spawn(entity, pos.X, pos.Y)
		tx.Commit()
	}

	// Type incorrect character
	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'x')

	// Heat should be reset to 0
	heat := ctx.State.GetHeat()
	if heat != 0 {
		t.Errorf("Expected heat to be 0 after incorrect character, got %d", heat)
	}

	// Cursor error should be set
	if !ctx.State.GetCursorError() {
		t.Error("Expected cursor error to be set")
	}
}

// TestBoostActivationAtMaxHeat verifies boost activation when heat reaches max
func TestBoostActivationAtMaxHeat(t *testing.T) {
	// Create mock screen
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	// Create game context
	ctx := engine.NewGameContext(screen)
	scoreSystem := NewScoreSystem(ctx)

	// Set heat to max - 1 (screen width is 80)
	ctx.State.SetHeat(79)
	ctx.State.SetBoostEnabled(false)

	// Create a green character at cursor position
	entity := ctx.World.CreateEntity()
	pos := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
	char := components.CharacterComponent{Rune: 'a', Style: tcell.StyleDefault}
	seq := components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceGreen,
		Level: components.LevelNormal,
	}

	// Spatial transaction handles PositionComponent
	ctx.World.Characters.Add(entity, char)
	ctx.World.Sequences.Add(entity, seq)
	{
		tx := ctx.World.BeginSpatialTransaction()
		tx.Spawn(entity, pos.X, pos.Y)
		tx.Commit()
	}

	// Type correct character to reach max heat
	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'a')

	// Heat should be at max (80)
	heat := ctx.State.GetHeat()
	if heat != 80 {
		t.Errorf("Expected heat to be 80, got %d", heat)
	}

	// Boost should now be enabled
	if !ctx.State.GetBoostEnabled() {
		t.Error("Expected boost to be enabled at max heat")
	}

	// Boost color should be 2 (Green)
	boostColor := ctx.State.GetBoostColor()
	if boostColor != 2 {
		t.Errorf("Expected boost color to be 2 (Green), got %d", boostColor)
	}
}

// TestScoreBlinkOnCorrectCharacter verifies score blink activation on correct character
func TestScoreBlinkOnCorrectCharacter(t *testing.T) {
	tests := []struct {
		name  string
		color components.SequenceType
		level components.SequenceLevel
	}{
		{"Bright Blue", components.SequenceBlue, components.LevelBright},
		{"Normal Green", components.SequenceGreen, components.LevelNormal},
		{"Dark Blue", components.SequenceBlue, components.LevelDark},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock screen
			screen := tcell.NewSimulationScreen("UTF-8")
			screen.Init()
			defer screen.Fini()
			screen.SetSize(80, 24)

			// Create game context
			ctx := engine.NewGameContext(screen)
			scoreSystem := NewScoreSystem(ctx)

			// Create a character at cursor position
			entity := ctx.World.CreateEntity()
			pos := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
			char := components.CharacterComponent{Rune: 'a', Style: tcell.StyleDefault}
			seq := components.SequenceComponent{
				ID:    1,
				Index: 0,
				Type:  tt.color,
				Level: tt.level,
			}

			// Spatial transaction handles PositionComponent
			ctx.World.Characters.Add(entity, char)
			ctx.World.Sequences.Add(entity, seq)
			{
				tx := ctx.World.BeginSpatialTransaction()
				tx.Spawn(entity, pos.X, pos.Y)
				tx.Commit()
			}

			// Type correct character
			scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'a')

			// Score blink should be active
			if !ctx.State.GetScoreBlinkActive() {
				t.Error("Expected score blink to be active after correct character")
			}

			// Verify blink type matches color
			blinkType := ctx.State.GetScoreBlinkType()
			var expectedType uint32
			switch tt.color {
			case components.SequenceBlue:
				expectedType = 1
			case components.SequenceGreen:
				expectedType = 2
			}
			if blinkType != expectedType {
				t.Errorf("Expected blink type %d, got %d", expectedType, blinkType)
			}

			// Verify blink level matches level
			blinkLevel := ctx.State.GetScoreBlinkLevel()
			var expectedLevel uint32
			switch tt.level {
			case components.LevelDark:
				expectedLevel = 0
			case components.LevelNormal:
				expectedLevel = 1
			case components.LevelBright:
				expectedLevel = 2
			}
			if blinkLevel != expectedLevel {
				t.Errorf("Expected blink level %d, got %d", expectedLevel, blinkLevel)
			}
		})
	}
}

// TestScoreBlinkOnError verifies score blink activation on error
func TestScoreBlinkOnError(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*testing.T, *engine.GameContext, *ScoreSystem)
	}{
		{
			name: "Empty Space",
			fn: func(t *testing.T, ctx *engine.GameContext, scoreSystem *ScoreSystem) {
				// Type at empty position
				scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'a')
			},
		},
		{
			name: "Wrong Character",
			fn: func(t *testing.T, ctx *engine.GameContext, scoreSystem *ScoreSystem) {
				// Create a character
				entity := ctx.World.CreateEntity()
				pos := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
				char := components.CharacterComponent{Rune: 'a', Style: tcell.StyleDefault}
				seq := components.SequenceComponent{
					ID:    1,
					Index: 0,
					Type:  components.SequenceGreen,
					Level: components.LevelNormal,
				}

				// Spatial transaction handles PositionComponent
				ctx.World.Characters.Add(entity, char)
				ctx.World.Sequences.Add(entity, seq)
				{
					tx := ctx.World.BeginSpatialTransaction()
					tx.Spawn(entity, pos.X, pos.Y)
					tx.Commit()
				}

				// Type wrong character
				scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'b')
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock screen
			screen := tcell.NewSimulationScreen("UTF-8")
			screen.Init()
			defer screen.Fini()
			screen.SetSize(80, 24)

			// Create game context
			ctx := engine.NewGameContext(screen)
			scoreSystem := NewScoreSystem(ctx)

			// Run test function
			tt.fn(t, ctx, scoreSystem)

			// Score blink should be active
			if !ctx.State.GetScoreBlinkActive() {
				t.Error("Expected score blink to be active after error")
			}

			// Score blink type should be 0 (error)
			blinkType := ctx.State.GetScoreBlinkType()
			if blinkType != 0 {
				t.Errorf("Expected blink type 0 (error), got %d", blinkType)
			}

			// Cursor error should be set
			if !ctx.State.GetCursorError() {
				t.Error("Expected cursor error to be set")
			}
		})
	}
}

// TestScoreBlinkOnGoldCharacter verifies score blink on gold sequence character
func TestScoreBlinkOnGoldCharacter(t *testing.T) {
	// Create mock screen
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	// Create game context
	ctx := engine.NewGameContext(screen)
	scoreSystem := NewScoreSystem(ctx)
	goldSystem := NewGoldSystem(ctx)

	// Wire up gold system
	scoreSystem.SetGoldSystem(goldSystem)

	// Manually activate gold sequence for testing (10 second duration)
	ctx.State.ActivateGoldSequence(1, 10*time.Second)

	// Create a gold character at cursor position
	entity := ctx.World.CreateEntity()
	pos := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
	char := components.CharacterComponent{Rune: 'x', Style: tcell.StyleDefault}
	seq := components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceGold,
		Level: components.LevelNormal,
	}

	// Spatial transaction handles PositionComponent
	ctx.World.Characters.Add(entity, char)
	ctx.World.Sequences.Add(entity, seq)
	{
		tx := ctx.World.BeginSpatialTransaction()
		tx.Spawn(entity, pos.X, pos.Y)
		tx.Commit()
	}

	// Type correct character
	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'x')

	// Score blink should be active
	if !ctx.State.GetScoreBlinkActive() {
		t.Error("Expected score blink to be active after gold character")
	}

	// Score blink type should be 4 (gold)
	blinkType := ctx.State.GetScoreBlinkType()
	if blinkType != 4 {
		t.Errorf("Expected blink type 4 (gold), got %d", blinkType)
	}

	// Score blink type should be non-zero (gold type = 4)
	blinkType = ctx.State.GetScoreBlinkType()
	if blinkType == 0 {
		t.Error("Expected score blink type to be non-zero (gold color, not error)")
	}
}

// TestScoreBlinkTimeout verifies score blink deactivates after timeout
func TestScoreBlinkTimeout(t *testing.T) {
	// Use mock time provider for controlled time advancement
	startTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	mockTime := engine.NewMockTimeProvider(startTime)

	// Create mock screen
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	// Create game context with mock time provider
	ctx := engine.NewGameContext(screen)
	ctx.TimeProvider = mockTime
	scoreSystem := NewScoreSystem(ctx)

	// Create a green character at cursor position
	entity := ctx.World.CreateEntity()
	pos := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
	char := components.CharacterComponent{Rune: 'a', Style: tcell.StyleDefault}
	seq := components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceGreen,
		Level: components.LevelNormal,
	}

	// Spatial transaction handles PositionComponent
	ctx.World.Characters.Add(entity, char)
	ctx.World.Sequences.Add(entity, seq)
	{
		tx := ctx.World.BeginSpatialTransaction()
		tx.Spawn(entity, pos.X, pos.Y)
		tx.Commit()
	}

	// Type correct character to trigger score blink
	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'a')

	// Score blink should be active
	if !ctx.State.GetScoreBlinkActive() {
		t.Fatal("Expected score blink to be active initially")
	}

	// Advance time to just before timeout (ScoreBlinkTimeout = 200ms)
	mockTime.Advance(constants.ScoreBlinkTimeout - 10*time.Millisecond)
	scoreSystem.Update(ctx.World, 10*time.Millisecond)

	// Score blink should still be active
	if !ctx.State.GetScoreBlinkActive() {
		t.Error("Expected score blink to still be active before timeout")
	}

	// Advance time past timeout
	mockTime.Advance(20 * time.Millisecond)
	scoreSystem.Update(ctx.World, 20*time.Millisecond)

	// Score blink should now be inactive
	if ctx.State.GetScoreBlinkActive() {
		t.Error("Expected score blink to be inactive after timeout")
	}
}

// TestCursorErrorTimeout verifies cursor error clears after timeout
func TestCursorErrorTimeout(t *testing.T) {
	// Use mock time provider for controlled time advancement
	startTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	mockTime := engine.NewMockTimeProvider(startTime)

	// Create mock screen
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	// Create game context with mock time provider
	ctx := engine.NewGameContext(screen)
	ctx.TimeProvider = mockTime
	scoreSystem := NewScoreSystem(ctx)

	// Type at empty position to trigger cursor error
	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'a')

	// Cursor error should be active
	if !ctx.State.GetCursorError() {
		t.Fatal("Expected cursor error to be active initially")
	}

	// Advance time to just before timeout (ErrorBlinkTimeout = 200ms)
	mockTime.Advance(constants.ErrorBlinkTimeout - 10*time.Millisecond)
	scoreSystem.Update(ctx.World, 10*time.Millisecond)

	// Cursor error should still be active
	if !ctx.State.GetCursorError() {
		t.Error("Expected cursor error to still be active before timeout")
	}

	// Advance time past timeout
	mockTime.Advance(20 * time.Millisecond)
	scoreSystem.Update(ctx.World, 20*time.Millisecond)

	// Cursor error should now be inactive
	if ctx.State.GetCursorError() {
		t.Error("Expected cursor error to be inactive after timeout")
	}
}
