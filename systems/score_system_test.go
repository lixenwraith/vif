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
	ctx.World.UpdateSpatialIndex(entity, pos.X, pos.Y)

	// Test 1: Without boost, heat should increment by 1
	ctx.BoostEnabled = false
	ctx.ScoreIncrement = 0
	initialScore := ctx.Score

	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'a')

	if ctx.ScoreIncrement != 1 {
		t.Errorf("Without boost, expected heat increment of 1, got %d", ctx.ScoreIncrement)
	}

	// Score should be heat * level_multiplier = 1 * 2 = 2
	expectedScore := initialScore + 2
	if ctx.Score != expectedScore {
		t.Errorf("Without boost, expected score %d, got %d", expectedScore, ctx.Score)
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
	ctx.World.UpdateSpatialIndex(entity2, pos2.X, pos2.Y)

	ctx.BoostEnabled = true
	previousHeat := ctx.ScoreIncrement
	previousScore := ctx.Score

	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'b')

	if ctx.ScoreIncrement != previousHeat+2 {
		t.Errorf("With boost, expected heat increment of 2 (total %d), got %d", previousHeat+2, ctx.ScoreIncrement)
	}

	// Score should increase by (heat * level_multiplier) = (3 * 2) = 6, total = 2 + 6 = 8
	expectedScore = previousScore + ctx.ScoreIncrement*2
	if ctx.Score != expectedScore {
		t.Errorf("With boost, expected score %d, got %d", expectedScore, ctx.Score)
	}
}

// TestBlueCharacterActivatesBoost verifies that typing blue characters triggers boost
func TestBlueCharacterActivatesBoost(t *testing.T) {
	// Create mock screen
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	// Create game context
	ctx := engine.NewGameContext(screen)
	scoreSystem := NewScoreSystem(ctx)

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
	ctx.World.UpdateSpatialIndex(entity, pos.X, pos.Y)

	// Boost should not be active initially
	if ctx.BoostEnabled {
		t.Error("Boost should not be active initially")
	}

	// Type the blue character
	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'a')

	// Boost should now be active
	if !ctx.BoostEnabled {
		t.Error("Boost should be active after typing blue character")
	}

	// Boost timer should be set
	if ctx.BoostTimer == nil {
		t.Error("Boost timer should be set")
	}

	// Boost end time should be in the future
	if !ctx.BoostEndTime.After(time.Now()) {
		t.Error("Boost end time should be in the future")
	}
}

// TestBoostExtension verifies that consecutive blue characters extend boost time
func TestBoostExtension(t *testing.T) {
	// Create mock screen
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	// Create game context
	ctx := engine.NewGameContext(screen)
	scoreSystem := NewScoreSystem(ctx)

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
	ctx.World.UpdateSpatialIndex(entity1, pos1.X, pos1.Y)

	// Type first blue character
	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'a')
	firstEndTime := ctx.BoostEndTime

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
	ctx.World.UpdateSpatialIndex(entity2, pos2.X, pos2.Y)

	// Type second blue character
	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'b')
	secondEndTime := ctx.BoostEndTime

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
	ctx.ScoreIncrement = 10

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
	ctx.World.UpdateSpatialIndex(entity, pos.X, pos.Y)

	// Type the red character
	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'a')

	// Heat should be reset to 0
	if ctx.ScoreIncrement != 0 {
		t.Errorf("Expected heat to be reset to 0, got %d", ctx.ScoreIncrement)
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
	ctx.ScoreIncrement = 0
	ctx.Score = 0

	// Test with boost disabled
	ctx.BoostEnabled = false

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
		ctx.World.UpdateSpatialIndex(entity, pos.X, pos.Y)

		scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, rune('a'+i))
	}

	// Heat should be 5 (1+1+1+1+1)
	// Score should be sum of (heat * 2): 2 + 4 + 6 + 8 + 10 = 30
	heatWithoutBoost := ctx.ScoreIncrement
	scoreWithoutBoost := ctx.Score

	if heatWithoutBoost != 5 {
		t.Errorf("Without boost, expected heat 5, got %d", heatWithoutBoost)
	}
	if scoreWithoutBoost != 30 {
		t.Errorf("Without boost, expected score 30, got %d", scoreWithoutBoost)
	}

	// Reset for boost test
	ctx.ScoreIncrement = 0
	ctx.Score = 0
	ctx.BoostEnabled = true

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
		ctx.World.UpdateSpatialIndex(entity, pos.X, pos.Y)

		scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, rune('f'+i))
	}

	// Heat should be 10 (2+2+2+2+2)
	// Score should be sum of (heat * 2): 4 + 8 + 12 + 16 + 20 = 60
	heatWithBoost := ctx.ScoreIncrement
	scoreWithBoost := ctx.Score

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
	ctx.ScoreIncrement = 10

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
	ctx.World.UpdateSpatialIndex(entity, pos.X, pos.Y)

	// Type wrong character
	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'b')

	// Heat should be reset to 0
	if ctx.ScoreIncrement != 0 {
		t.Errorf("Expected heat to be reset to 0 after incorrect character, got %d", ctx.ScoreIncrement)
	}

	// Cursor error should be set
	if !ctx.CursorError {
		t.Error("Expected cursor error to be set after incorrect character")
	}
}
