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

	ctx.World.AddComponent(entity, pos)
	ctx.World.AddComponent(entity, char)
	ctx.World.AddComponent(entity, seq)
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

	if ctx.State.GetHeat() != 1 {
		t.Errorf("Without boost, expected heat increment of 1, got %d", ctx.State.GetHeat())
	}

	// Score should be heat * level_multiplier = 1 * 2 = 2
	expectedScore := initialScore + 2
	if ctx.State.GetScore() != expectedScore {
		t.Errorf("Without boost, expected score %d, got %d", expectedScore, ctx.State.GetScore())
	}

	// Test 2: With boost, heat should increment by 2
	entity2 := ctx.World.CreateEntity()
	pos2 := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
	char2 := components.CharacterComponent{Rune: 'b', Style: tcell.StyleDefault}
	seq2 := components.SequenceComponent{
		ID:    2,
		Index: 0,
		Type:  components.SequenceGreen,
		Level: components.LevelNormal,
	}

	ctx.World.AddComponent(entity2, pos2)
	ctx.World.AddComponent(entity2, char2)
	ctx.World.AddComponent(entity2, seq2)
	{
		tx := ctx.World.BeginSpatialTransaction()
		tx.Spawn(entity2, pos2.X, pos2.Y)
		tx.Commit()
	}

	ctx.State.SetBoostEnabled(true)
	previousHeat := ctx.State.GetHeat()
	previousScore := ctx.State.GetScore()

	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'b')

	if ctx.State.GetHeat() != previousHeat+2 {
		t.Errorf("With boost, expected heat increment of 2 (total %d), got %d", previousHeat+2, ctx.State.GetHeat())
	}

	// Score should increase by (heat * level_multiplier) = (3 * 2) = 6, total = 2 + 6 = 8
	expectedScore = previousScore + ctx.State.GetHeat()*2
	if ctx.State.GetScore() != expectedScore {
		t.Errorf("With boost, expected score %d, got %d", expectedScore, ctx.State.GetScore())
	}
}

// TestBlueCharacterActivatesBoost verifies that typing blue characters triggers boost when heat reaches max
func TestBlueCharacterActivatesBoost(t *testing.T) {
	// Create mock screen
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	// Create game context
	ctx := engine.NewGameContext(screen)
	scoreSystem := NewScoreSystem(ctx)

	// Calculate max heat
	maxHeat := ctx.Width
	if maxHeat < 1 {
		maxHeat = 1
	}

	// Set heat to max - 1 (just below threshold)
	ctx.State.SetHeat(maxHeat - 1)

	// Create a blue character at cursor position
	entity := ctx.World.CreateEntity()
	pos := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
	char := components.CharacterComponent{Rune: 'a', Style: tcell.StyleDefault}
	seq := components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceBlue,
		Level: components.LevelNormal,
	}

	ctx.World.AddComponent(entity, pos)
	ctx.World.AddComponent(entity, char)
	ctx.World.AddComponent(entity, seq)
	{
		tx := ctx.World.BeginSpatialTransaction()
		tx.Spawn(entity, pos.X, pos.Y)
		tx.Commit()
	}

	// Boost should not be active initially
	if ctx.State.GetBoostEnabled() {
		t.Error("Boost should not be active initially")
	}

	// Type the blue character (should reach max heat and activate boost)
	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'a')

	// Boost should now be active
	if !ctx.State.GetBoostEnabled() {
		t.Error("Boost should be active after typing blue character and reaching max heat")
	}

	// Boost end time should be in the future
	if !ctx.State.GetBoostEndTime().After(ctx.TimeProvider.Now()) {
		t.Error("Boost end time should be in the future")
	}
}

// TestBoostExtension verifies that consecutive blue characters extend boost time when heat is at max
func TestBoostExtension(t *testing.T) {
	// Create mock screen
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	// Create game context
	ctx := engine.NewGameContext(screen)
	scoreSystem := NewScoreSystem(ctx)

	// Calculate max heat
	maxHeat := ctx.Width
	if maxHeat < 1 {
		maxHeat = 1
	}

	// Set heat to max - 1 and activate boost with first character
	ctx.State.SetHeat(maxHeat - 1)

	// Create first blue character
	entity1 := ctx.World.CreateEntity()
	pos1 := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
	char1 := components.CharacterComponent{Rune: 'a', Style: tcell.StyleDefault}
	seq1 := components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceBlue,
		Level: components.LevelNormal,
	}

	ctx.World.AddComponent(entity1, pos1)
	ctx.World.AddComponent(entity1, char1)
	ctx.World.AddComponent(entity1, seq1)
	{
		tx := ctx.World.BeginSpatialTransaction()
		tx.Spawn(entity1, pos1.X, pos1.Y)
		tx.Commit()
	}

	// Type first blue character (should activate boost)
	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'a')
	firstEndTime := ctx.State.GetBoostEndTime()

	// Verify boost is active
	if !ctx.State.GetBoostEnabled() {
		t.Fatal("Boost should be active after first character")
	}

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	// Create second blue character
	entity2 := ctx.World.CreateEntity()
	pos2 := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
	char2 := components.CharacterComponent{Rune: 'b', Style: tcell.StyleDefault}
	seq2 := components.SequenceComponent{
		ID:    2,
		Index: 0,
		Type:  components.SequenceBlue,
		Level: components.LevelNormal,
	}

	ctx.World.AddComponent(entity2, pos2)
	ctx.World.AddComponent(entity2, char2)
	ctx.World.AddComponent(entity2, seq2)
	{
		tx := ctx.World.BeginSpatialTransaction()
		tx.Spawn(entity2, pos2.X, pos2.Y)
		tx.Commit()
	}

	// Type second blue character (should extend boost)
	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'b')
	secondEndTime := ctx.State.GetBoostEndTime()

	// Second end time should be later than first (extended)
	if !secondEndTime.After(firstEndTime) {
		t.Error("Second boost end time should be extended from first")
	}

	// The extension should be approximately BoostExtensionDuration
	expectedExtension := constants.BoostExtensionDuration
	actualExtension := secondEndTime.Sub(firstEndTime)

	// Allow some tolerance (within 200ms)
	diff := actualExtension - expectedExtension
	if diff < -200*time.Millisecond || diff > 200*time.Millisecond {
		t.Errorf("Boost extension should be approximately %v, got %v", expectedExtension, actualExtension)
	}
}

// TestRedCharacterResetsHeat verifies that red characters reset heat
func TestRedCharacterResetsHeat(t *testing.T) {
	// Create mock screen
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	// Create game context
	ctx := engine.NewGameContext(screen)
	scoreSystem := NewScoreSystem(ctx)

	// Set initial heat
	ctx.State.SetHeat(10)

	// Create a red character at cursor position
	entity := ctx.World.CreateEntity()
	pos := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
	char := components.CharacterComponent{Rune: 'a', Style: tcell.StyleDefault}
	seq := components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceRed,
		Level: components.LevelNormal,
	}

	ctx.World.AddComponent(entity, pos)
	ctx.World.AddComponent(entity, char)
	ctx.World.AddComponent(entity, seq)
	{
		tx := ctx.World.BeginSpatialTransaction()
		tx.Spawn(entity, pos.X, pos.Y)
		tx.Commit()
	}

	// Type the red character
	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'a')

	// Heat should be reset to 0
	if ctx.State.GetHeat() != 0 {
		t.Errorf("Expected heat to be reset to 0, got %d", ctx.State.GetHeat())
	}
}

// TestBoostDoesNotAffectScore verifies that boost affects heat, not score directly
func TestBoostDoesNotAffectScore(t *testing.T) {
	// Create mock screen
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	// Create game context
	ctx := engine.NewGameContext(screen)
	scoreSystem := NewScoreSystem(ctx)

	// Reset initial state
	ctx.State.SetHeat(0)
	ctx.State.SetScore(0)

	// Test with boost disabled
	ctx.State.SetBoostEnabled(false)

	// Type 5 green characters without boost
	for i := 0; i < 5; i++ {
		entity := ctx.World.CreateEntity()
		pos := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
		char := components.CharacterComponent{Rune: rune('a' + i), Style: tcell.StyleDefault}
		seq := components.SequenceComponent{
			ID:    i + 1,
			Index: 0,
			Type:  components.SequenceGreen,
			Level: components.LevelNormal,
		}

		ctx.World.AddComponent(entity, pos)
		ctx.World.AddComponent(entity, char)
		ctx.World.AddComponent(entity, seq)
		{
			tx := ctx.World.BeginSpatialTransaction()
			tx.Spawn(entity, pos.X, pos.Y)
			tx.Commit()
		}

		scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, rune('a'+i))
	}

	// Heat should be 5 (1+1+1+1+1)
	// Score should be sum of (heat * 2): 2 + 4 + 6 + 8 + 10 = 30
	heatWithoutBoost := ctx.State.GetHeat()
	scoreWithoutBoost := ctx.State.GetScore()

	if heatWithoutBoost != 5 {
		t.Errorf("Without boost, expected heat 5, got %d", heatWithoutBoost)
	}
	if scoreWithoutBoost != 30 {
		t.Errorf("Without boost, expected score 30, got %d", scoreWithoutBoost)
	}

	// Reset for boost test
	ctx.State.SetHeat(0)
	ctx.State.SetScore(0)
	ctx.State.SetBoostEnabled(true)

	// Type 5 green characters with boost
	for i := 0; i < 5; i++ {
		entity := ctx.World.CreateEntity()
		pos := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
		char := components.CharacterComponent{Rune: rune('f' + i), Style: tcell.StyleDefault}
		seq := components.SequenceComponent{
			ID:    i + 10,
			Index: 0,
			Type:  components.SequenceGreen,
			Level: components.LevelNormal,
		}

		ctx.World.AddComponent(entity, pos)
		ctx.World.AddComponent(entity, char)
		ctx.World.AddComponent(entity, seq)
		{
			tx := ctx.World.BeginSpatialTransaction()
			tx.Spawn(entity, pos.X, pos.Y)
			tx.Commit()
		}

		scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, rune('f'+i))
	}

	// Heat should be 10 (2+2+2+2+2)
	// Score should be sum of (heat * 2): 4 + 8 + 12 + 16 + 20 = 60
	heatWithBoost := ctx.State.GetHeat()
	scoreWithBoost := ctx.State.GetScore()

	if heatWithBoost != 10 {
		t.Errorf("With boost, expected heat 10, got %d", heatWithBoost)
	}
	if scoreWithBoost != 60 {
		t.Errorf("With boost, expected score 60, got %d", scoreWithBoost)
	}
}

// TestIncorrectCharacterResetsHeat verifies that typing wrong character resets heat
func TestIncorrectCharacterResetsHeat(t *testing.T) {
	// Create mock screen
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	// Create game context
	ctx := engine.NewGameContext(screen)
	scoreSystem := NewScoreSystem(ctx)

	// Set initial heat
	ctx.State.SetHeat(10)

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

	ctx.World.AddComponent(entity, pos)
	ctx.World.AddComponent(entity, char)
	ctx.World.AddComponent(entity, seq)
	{
		tx := ctx.World.BeginSpatialTransaction()
		tx.Spawn(entity, pos.X, pos.Y)
		tx.Commit()
	}

	// Type wrong character
	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'b')

	// Heat should be reset to 0
	if ctx.State.GetHeat() != 0 {
		t.Errorf("Expected heat to be reset to 0 after incorrect character, got %d", ctx.State.GetHeat())
	}

	// Cursor error should be set
	if !ctx.State.GetCursorError() {
		t.Error("Expected cursor error to be set after incorrect character")
	}
}

// TestScoreBlinkOnCorrectCharacter verifies that score blink is activated with character color
func TestScoreBlinkOnCorrectCharacter(t *testing.T) {
	// Create mock screen
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	// Create game context
	ctx := engine.NewGameContext(screen)
	scoreSystem := NewScoreSystem(ctx)

	// Test with different character types and levels
	tests := []struct {
		name  string
		seqType components.SequenceType
		level components.SequenceLevel
	}{
		{"Bright Blue", components.SequenceBlue, components.LevelBright},
		{"Normal Green", components.SequenceGreen, components.LevelNormal},
		{"Dark Blue", components.SequenceBlue, components.LevelDark},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a character at cursor position
			entity := ctx.World.CreateEntity()
			pos := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
			char := components.CharacterComponent{Rune: 'a', Style: tcell.StyleDefault}
			seq := components.SequenceComponent{
				ID:    1,
				Index: 0,
				Type:  tt.seqType,
				Level: tt.level,
			}

			ctx.World.AddComponent(entity, pos)
			ctx.World.AddComponent(entity, char)
			ctx.World.AddComponent(entity, seq)
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

			// Score blink type should match character color (non-error)
			blinkType := ctx.State.GetScoreBlinkType()
			if blinkType == 0 {
				t.Error("Expected score blink type to be non-zero (character color, not error)")
			}

			// Score blink time should be recent
			blinkTime := ctx.State.GetScoreBlinkTime()
			now := ctx.TimeProvider.Now()
			if blinkTime.After(now) || now.Sub(blinkTime) > 100*time.Millisecond {
				t.Errorf("Expected score blink time to be recent, got %v", blinkTime)
			}
		})
	}
}

// TestScoreBlinkOnError verifies that score blink is activated with error color (black)
func TestScoreBlinkOnError(t *testing.T) {
	// Create mock screen
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	// Create game context
	ctx := engine.NewGameContext(screen)
	scoreSystem := NewScoreSystem(ctx)

	// Test 1: Typing on empty space
	t.Run("Empty Space", func(t *testing.T) {
		scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'a')

		// Score blink should be active
		if !ctx.State.GetScoreBlinkActive() {
			t.Error("Expected score blink to be active after error")
		}

		// Score blink type should be 0 (error state)
		blinkType := ctx.State.GetScoreBlinkType()
		if blinkType != 0 {
			t.Errorf("Expected score blink type to be 0 (error state), got %d", blinkType)
		}
	})

	// Test 2: Typing wrong character
	t.Run("Wrong Character", func(t *testing.T) {
		// Create a character at cursor position
		entity := ctx.World.CreateEntity()
		pos := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
		char := components.CharacterComponent{Rune: 'a', Style: tcell.StyleDefault}
		seq := components.SequenceComponent{
			ID:    1,
			Index: 0,
			Type:  components.SequenceGreen,
			Level: components.LevelNormal,
		}

		ctx.World.AddComponent(entity, pos)
		ctx.World.AddComponent(entity, char)
		ctx.World.AddComponent(entity, seq)
		{
			tx := ctx.World.BeginSpatialTransaction()
			tx.Spawn(entity, pos.X, pos.Y)
			tx.Commit()
		}

		// Type wrong character
		scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'b')

		// Score blink should be active
		if !ctx.State.GetScoreBlinkActive() {
			t.Error("Expected score blink to be active after error")
		}

		// Score blink type should be 0 (error state)
		blinkType := ctx.State.GetScoreBlinkType()
		if blinkType != 0 {
			t.Errorf("Expected score blink type to be 0 (error state), got %d", blinkType)
		}
	})
}

// TestScoreBlinkOnGoldCharacter verifies that gold character typing triggers score blink
func TestScoreBlinkOnGoldCharacter(t *testing.T) {
	// Create mock screen
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	// Create game context
	ctx := engine.NewGameContext(screen)
	scoreSystem := NewScoreSystem(ctx)

	// Note: Without an active gold sequence system, gold characters are treated as regular characters
	// This test verifies that the score blink is activated when typing a gold character
	// when gold sequence is not active (which should not happen in normal gameplay but tests the code path)

	// Create a gold character at cursor position
	entity := ctx.World.CreateEntity()
	pos := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
	char := components.CharacterComponent{Rune: 'x', Style: tcell.StyleDefault}
	seq := components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceGold,
		Level: components.LevelBright,
	}

	ctx.World.AddComponent(entity, pos)
	ctx.World.AddComponent(entity, char)
	ctx.World.AddComponent(entity, seq)
	{
		tx := ctx.World.BeginSpatialTransaction()
		tx.Spawn(entity, pos.X, pos.Y)
		tx.Commit()
	}

	// Type correct gold character (without gold sequence active, it will be treated as regular character)
	// The character will still trigger score blink with its color
	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'x')

	// Score blink should be active
	if !ctx.State.GetScoreBlinkActive() {
		t.Error("Expected score blink to be active after gold character")
	}

	// Score blink type should be non-zero (gold type = 4)
	blinkType := ctx.State.GetScoreBlinkType()
	if blinkType == 0 {
		t.Error("Expected score blink type to be non-zero (gold color, not error)")
	}
}
