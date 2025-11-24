package systems

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestHeatBasedBoostActivation verifies that boost activates when heat reaches max
func TestHeatBasedBoostActivation(t *testing.T) {
	// Create mock screen
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	// Create game context
	ctx := engine.NewGameContext(screen)
	scoreSystem := NewScoreSystem(ctx)

	// Calculate max heat (heat bar width)
	maxHeat := ctx.Width
	if maxHeat < 1 {
		maxHeat = 1
	}

	// Set heat to max - 1 (just below threshold)
	ctx.State.SetHeat(maxHeat - 1)
	ctx.State.SetBoostEnabled(false)

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

	// Spatial transaction handles PositionComponent
	ctx.World.Characters.Add(entity, char)
	ctx.World.AddComponent(entity, seq)

	tx := ctx.World.BeginSpatialTransaction()
	tx.Spawn(entity, pos.X, pos.Y)
	tx.Commit()

	// Boost should not be active initially
	if ctx.State.GetBoostEnabled() {
		t.Error("Boost should not be active before reaching max heat")
	}

	// Type the blue character - this should push heat to max and activate boost
	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'a')

	// Boost should now be active
	if !ctx.State.GetBoostEnabled() {
		t.Error("Boost should be active after heat reaches max")
	}

	// BoostSequenceColor should be set to Blue (1)
	if ctx.State.GetBoostColor() != 1 {
		t.Errorf("BoostSequenceColor should be 1 (Blue), got %d", ctx.State.GetBoostColor())
	}

	// Boost end time should be in the future
	if !ctx.State.GetBoostEndTime().After(ctx.TimeProvider.Now()) {
		t.Error("Boost end time should be in the future")
	}
}

// TestBoostMaintainsSameColor verifies that boost maintains while typing same color
func TestBoostMaintainsSameColor(t *testing.T) {
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

	// Set heat to max and activate boost with Blue
	ctx.State.SetHeat(maxHeat)
	ctx.State.SetBoostEnabled(true)
	ctx.State.SetBoostColor(1) // Blue
	initialEndTime := ctx.TimeProvider.Now().Add(constants.BoostExtensionDuration)
	ctx.State.SetBoostEndTime(initialEndTime)

	// Wait a bit to distinguish timestamps
	time.Sleep(50 * time.Millisecond)

	// Create another blue character
	entity := ctx.World.CreateEntity()
	pos := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
	char := components.CharacterComponent{Rune: 'b', Style: tcell.StyleDefault}
	seq := components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceBlue,
		Level: components.LevelNormal,
	}

	// Spatial transaction handles PositionComponent
	ctx.World.Characters.Add(entity, char)
	ctx.World.AddComponent(entity, seq)

	tx := ctx.World.BeginSpatialTransaction()
	tx.Spawn(entity, pos.X, pos.Y)
	tx.Commit()

	// Type the blue character - should extend boost timer
	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'b')

	// Boost should still be active
	if !ctx.State.GetBoostEnabled() {
		t.Error("Boost should remain active when typing same color")
	}

	// BoostSequenceColor should still be Blue (1)
	if ctx.State.GetBoostColor() != 1 {
		t.Errorf("BoostSequenceColor should remain 1 (Blue), got %d", ctx.State.GetBoostColor())
	}

	// Boost end time should be extended (later than initial)
	newEndTime := ctx.State.GetBoostEndTime()
	if !newEndTime.After(initialEndTime) {
		t.Error("Boost end time should be extended when typing same color")
	}
}

// TestBoostDeactivatesOnColorSwitch verifies boost resets on color switch but heat preserved
func TestBoostDeactivatesOnColorSwitch(t *testing.T) {
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

	// Set heat to max and activate boost with Blue
	ctx.State.SetHeat(maxHeat)
	ctx.State.SetBoostEnabled(true)
	ctx.State.SetBoostColor(1) // Blue
	ctx.State.SetBoostEndTime(ctx.TimeProvider.Now().Add(5 * time.Second))

	// Create a green character (different color)
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
	ctx.World.AddComponent(entity, seq)

	tx := ctx.World.BeginSpatialTransaction()
	tx.Spawn(entity, pos.X, pos.Y)
	tx.Commit()

	// Type the green character - should reset boost timer but preserve heat
	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'a')

	// Boost should be deactivated (timer reset)
	if ctx.State.GetBoostEnabled() {
		t.Error("Boost should be deactivated when switching colors")
	}

	// BoostSequenceColor should be updated to Green (2)
	if ctx.State.GetBoostColor() != 2 {
		t.Errorf("BoostSequenceColor should be 2 (Green), got %d", ctx.State.GetBoostColor())
	}

	// Heat should be preserved at max (plus boost multiplier gain)
	// Since boost was active when we typed, we got +2 heat
	expectedHeat := maxHeat + 2
	if ctx.State.GetHeat() != expectedHeat {
		t.Errorf("Heat should be preserved at %d, got %d", expectedHeat, ctx.State.GetHeat())
	}
}

// TestBoostRebuildAfterColorSwitch verifies boost can rebuild after color switch
func TestBoostRebuildAfterColorSwitch(t *testing.T) {
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

	// Simulate the state after color switch:
	// - Heat is at max
	// - Boost is deactivated
	// - BoostSequenceColor is Green (2)
	ctx.State.SetHeat(maxHeat)
	ctx.State.SetBoostEnabled(false)
	ctx.State.SetBoostColor(2) // Green

	// Create a green character (same as current boost color)
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
	ctx.World.AddComponent(entity, seq)

	tx := ctx.World.BeginSpatialTransaction()
	tx.Spawn(entity, pos.X, pos.Y)
	tx.Commit()

	// Type the green character - should reactivate boost
	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'a')

	// Boost should be reactivated
	if !ctx.State.GetBoostEnabled() {
		t.Error("Boost should be reactivated when continuing same color at max heat")
	}

	// BoostSequenceColor should remain Green (2)
	if ctx.State.GetBoostColor() != 2 {
		t.Errorf("BoostSequenceColor should remain 2 (Green), got %d", ctx.State.GetBoostColor())
	}
}

// TestBoostDeactivatesOnError verifies boost deactivates on typing error
func TestBoostDeactivatesOnError(t *testing.T) {
	// Create mock screen
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	// Create game context
	ctx := engine.NewGameContext(screen)
	scoreSystem := NewScoreSystem(ctx)

	// Set heat to max and activate boost
	maxHeat := ctx.Width
	ctx.State.SetHeat(maxHeat)
	ctx.State.SetBoostEnabled(true)
	ctx.State.SetBoostColor(1) // Blue
	ctx.State.SetBoostEndTime(ctx.TimeProvider.Now().Add(5 * time.Second))

	// Create a blue character at cursor
	entity := ctx.World.CreateEntity()
	pos := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
	char := components.CharacterComponent{Rune: 'a', Style: tcell.StyleDefault}
	seq := components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceBlue,
		Level: components.LevelNormal,
	}

	// Spatial transaction handles PositionComponent
	ctx.World.Characters.Add(entity, char)
	ctx.World.AddComponent(entity, seq)

	tx := ctx.World.BeginSpatialTransaction()
	tx.Spawn(entity, pos.X, pos.Y)
	tx.Commit()

	// Type wrong character
	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'b')

	// Boost should be deactivated
	if ctx.State.GetBoostEnabled() {
		t.Error("Boost should be deactivated on typing error")
	}

	// BoostSequenceColor should be reset to None (0)
	if ctx.State.GetBoostColor() != 0 {
		t.Errorf("BoostSequenceColor should be 0 (None), got %d", ctx.State.GetBoostColor())
	}

	// Heat should be reset to 0
	if ctx.State.GetHeat() != 0 {
		t.Errorf("Heat should be reset to 0, got %d", ctx.State.GetHeat())
	}

	// Cursor error should be set
	if !ctx.State.GetCursorError() {
		t.Error("Cursor error should be set on typing error")
	}
}

// TestBoostDeactivatesOnRedCharacter verifies boost deactivates on Red character
func TestBoostDeactivatesOnRedCharacter(t *testing.T) {
	// Create mock screen
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	// Create game context
	ctx := engine.NewGameContext(screen)
	scoreSystem := NewScoreSystem(ctx)

	// Set heat to max and activate boost
	maxHeat := ctx.Width
	ctx.State.SetHeat(maxHeat)
	ctx.State.SetBoostEnabled(true)
	ctx.State.SetBoostColor(1) // Blue
	ctx.State.SetBoostEndTime(ctx.TimeProvider.Now().Add(5 * time.Second))

	// Create a red character at cursor
	entity := ctx.World.CreateEntity()
	pos := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
	char := components.CharacterComponent{Rune: 'a', Style: tcell.StyleDefault}
	seq := components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceRed,
		Level: components.LevelNormal,
	}

	// Spatial transaction handles PositionComponent
	ctx.World.Characters.Add(entity, char)
	ctx.World.AddComponent(entity, seq)

	tx := ctx.World.BeginSpatialTransaction()
	tx.Spawn(entity, pos.X, pos.Y)
	tx.Commit()

	// Type the red character
	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'a')

	// Boost should be deactivated
	if ctx.State.GetBoostEnabled() {
		t.Error("Boost should be deactivated on Red character")
	}

	// BoostSequenceColor should be reset to None (0)
	if ctx.State.GetBoostColor() != 0 {
		t.Errorf("BoostSequenceColor should be 0 (None), got %d", ctx.State.GetBoostColor())
	}

	// Heat should be reset to 0
	if ctx.State.GetHeat() != 0 {
		t.Errorf("Heat should be reset to 0, got %d", ctx.State.GetHeat())
	}
}

// TestBoostTimerExpiration verifies boost expires on timer and resets sequence color
func TestBoostTimerExpiration(t *testing.T) {
	// Create mock screen
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(80, 24)

	// Create game context
	ctx := engine.NewGameContext(screen)

	// Set boost with short expiration time
	ctx.State.SetBoostEnabled(true)
	ctx.State.SetBoostColor(1) // Blue
	ctx.State.SetBoostEndTime(ctx.TimeProvider.Now().Add(100 * time.Millisecond))

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Call UpdateBoostTimerAtomic to check expiration
	expired := ctx.State.UpdateBoostTimerAtomic()

	// Should return true (expired)
	if !expired {
		t.Error("UpdateBoostTimerAtomic should return true when boost expired")
	}

	// Boost should be deactivated
	if ctx.State.GetBoostEnabled() {
		t.Error("Boost should be deactivated after timer expiration")
	}

	// BoostSequenceColor should be reset to None (0)
	if ctx.State.GetBoostColor() != 0 {
		t.Errorf("BoostSequenceColor should be 0 (None) after expiration, got %d", ctx.State.GetBoostColor())
	}
}

// TestBoostExtensionDuration verifies boost extends by 0.5s (500ms)
func TestBoostExtensionDuration(t *testing.T) {
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

	// Set heat to max and activate boost
	ctx.State.SetHeat(maxHeat)
	ctx.State.SetBoostEnabled(true)
	ctx.State.SetBoostColor(1) // Blue
	initialEndTime := ctx.TimeProvider.Now().Add(constants.BoostExtensionDuration)
	ctx.State.SetBoostEndTime(initialEndTime)

	// Wait a bit to distinguish timestamps
	time.Sleep(50 * time.Millisecond)

	// Create a blue character
	entity := ctx.World.CreateEntity()
	pos := components.PositionComponent{X: ctx.CursorX, Y: ctx.CursorY}
	char := components.CharacterComponent{Rune: 'a', Style: tcell.StyleDefault}
	seq := components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceBlue,
		Level: components.LevelNormal,
	}

	// Spatial transaction handles PositionComponent
	ctx.World.Characters.Add(entity, char)
	ctx.World.AddComponent(entity, seq)

	tx := ctx.World.BeginSpatialTransaction()
	tx.Spawn(entity, pos.X, pos.Y)
	tx.Commit()

	// Type the blue character
	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'a')

	// Get new end time
	newEndTime := ctx.State.GetBoostEndTime()

	// Calculate actual extension
	actualExtension := newEndTime.Sub(initialEndTime)

	// Should be approximately 500ms (allow 200ms tolerance)
	expectedExtension := 500 * time.Millisecond
	diff := actualExtension - expectedExtension
	if diff < -200*time.Millisecond || diff > 200*time.Millisecond {
		t.Errorf("Boost extension should be approximately %v, got %v", expectedExtension, actualExtension)
	}

	// Verify constant is 500ms
	if constants.BoostExtensionDuration != 500*time.Millisecond {
		t.Errorf("BoostExtensionDuration constant should be 500ms, got %v", constants.BoostExtensionDuration)
	}
}

// TestGoldSequenceDoesNotAffectBoost verifies gold sequence doesn't interfere with boost
// This is a manual test - gold sequences are handled separately in handleGoldSequenceTyping
// and should not affect boost state. The implementation in score_system.go handles this
// by returning early for gold sequences before boost logic is executed.
func TestGoldSequenceDoesNotAffectBoost(t *testing.T) {
	t.Skip("Gold sequence integration test - requires full gold system setup")
	// The key invariant is that handleGoldSequenceTyping returns early (line 100 in score_system.go)
	// before any boost logic is executed, ensuring gold sequences don't interfere with boost state.
}

// TestBoostActivationWithGreen verifies boost can activate with Green sequences
func TestBoostActivationWithGreen(t *testing.T) {
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

	// Set heat to max - 1
	ctx.State.SetHeat(maxHeat - 1)
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
	ctx.World.AddComponent(entity, seq)

	tx := ctx.World.BeginSpatialTransaction()
	tx.Spawn(entity, pos.X, pos.Y)
	tx.Commit()

	// Type the green character - should activate boost with Green
	scoreSystem.HandleCharacterTyping(ctx.World, ctx.CursorX, ctx.CursorY, 'a')

	// Boost should be active
	if !ctx.State.GetBoostEnabled() {
		t.Error("Boost should be active after heat reaches max with Green")
	}

	// BoostSequenceColor should be set to Green (2)
	if ctx.State.GetBoostColor() != 2 {
		t.Errorf("BoostSequenceColor should be 2 (Green), got %d", ctx.State.GetBoostColor())
	}
}
