package systems

import (
	"reflect"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestGoldSequenceSpawnsAfterDecay tests that gold sequence spawns when decay ends
func TestGoldSequenceSpawnsAfterDecay(t *testing.T) {
	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx := createTestContext(mockTime)
	world := ctx.World

	decaySystem := NewDecaySystem(ctx.GameWidth, ctx.GameHeight, ctx.Width, 0, ctx)
	goldSystem := NewGoldSequenceSystem(ctx, decaySystem, ctx.GameWidth, ctx.GameHeight)

	// Initially, no gold sequence should be active
	if goldSystem.IsActive() {
		t.Error("Gold sequence should not be active initially")
	}

	// Simulate decay animation starting
	decaySystem.animating = true

	// Update gold system - should not spawn yet
	goldSystem.Update(world, 16*time.Millisecond)
	if goldSystem.IsActive() {
		t.Error("Gold sequence should not spawn while decay is animating")
	}

	// Simulate decay animation ending
	decaySystem.animating = false

	// Update gold system - should spawn now
	goldSystem.Update(world, 16*time.Millisecond)
	if !goldSystem.IsActive() {
		t.Error("Gold sequence should spawn after decay ends")
	}

	// Verify gold sequence entities were created
	seqType := reflect.TypeOf(components.SequenceComponent{})
	entities := world.GetEntitiesWith(seqType)

	goldCount := 0
	for _, entity := range entities {
		seqComp, _ := world.GetComponent(entity, seqType)
		seq := seqComp.(components.SequenceComponent)
		if seq.Type == components.SequenceGold {
			goldCount++
		}
	}

	if goldCount != constants.GoldSequenceLength {
		t.Errorf("Expected %d gold characters, got %d", constants.GoldSequenceLength, goldCount)
	}
}

// TestGoldSequenceTimeout tests that gold sequence disappears after timeout
func TestGoldSequenceTimeout(t *testing.T) {
	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx := createTestContext(mockTime)
	world := ctx.World

	decaySystem := NewDecaySystem(ctx.GameWidth, ctx.GameHeight, ctx.Width, 0, ctx)
	goldSystem := NewGoldSequenceSystem(ctx, decaySystem, ctx.GameWidth, ctx.GameHeight)

	// Trigger gold sequence spawn
	decaySystem.animating = true
	goldSystem.Update(world, 16*time.Millisecond)
	decaySystem.animating = false
	goldSystem.Update(world, 16*time.Millisecond)

	if !goldSystem.IsActive() {
		t.Fatal("Gold sequence should be active after spawning")
	}

	// Advance time by less than timeout
	mockTime.Advance(5 * time.Second)
	goldSystem.Update(world, 16*time.Millisecond)

	if !goldSystem.IsActive() {
		t.Error("Gold sequence should still be active before timeout")
	}

	// Advance time past timeout
	mockTime.Advance(6 * time.Second) // Total: 11 seconds
	goldSystem.Update(world, 16*time.Millisecond)

	if goldSystem.IsActive() {
		t.Error("Gold sequence should be inactive after timeout")
	}

	// Verify all gold entities were removed
	seqType := reflect.TypeOf(components.SequenceComponent{})
	entities := world.GetEntitiesWith(seqType)

	for _, entity := range entities {
		seqComp, _ := world.GetComponent(entity, seqType)
		seq := seqComp.(components.SequenceComponent)
		if seq.Type == components.SequenceGold {
			t.Error("Gold sequence entities should be removed after timeout")
		}
	}
}

// TestGoldSequenceTypingDoesNotAffectHeat tests that typing gold chars doesn't affect heat
func TestGoldSequenceTypingDoesNotAffectHeat(t *testing.T) {
	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx := createTestContext(mockTime)
	world := ctx.World

	decaySystem := NewDecaySystem(ctx.GameWidth, ctx.GameHeight, ctx.Width, 0, ctx)
	goldSystem := NewGoldSequenceSystem(ctx, decaySystem, ctx.GameWidth, ctx.GameHeight)
	scoreSystem := NewScoreSystem(ctx)
	scoreSystem.SetGoldSequenceSystem(goldSystem)

	// Spawn gold sequence
	decaySystem.animating = true
	goldSystem.Update(world, 16*time.Millisecond)
	decaySystem.animating = false
	goldSystem.Update(world, 16*time.Millisecond)

	// Set initial heat
	ctx.SetScoreIncrement(10)
	initialHeat := ctx.GetScoreIncrement()

	// Find first gold character
	seqType := reflect.TypeOf(components.SequenceComponent{})
	charType := reflect.TypeOf(components.CharacterComponent{})
	posType := reflect.TypeOf(components.PositionComponent{})
	entities := world.GetEntitiesWith(seqType, charType, posType)

	var firstGoldEntity engine.Entity
	var firstGoldChar components.CharacterComponent
	var firstGoldPos components.PositionComponent

	for _, entity := range entities {
		seqComp, _ := world.GetComponent(entity, seqType)
		seq := seqComp.(components.SequenceComponent)
		if seq.Type == components.SequenceGold && seq.Index == 0 {
			firstGoldEntity = entity
			charComp, _ := world.GetComponent(entity, charType)
			firstGoldChar = charComp.(components.CharacterComponent)
			posComp, _ := world.GetComponent(entity, posType)
			firstGoldPos = posComp.(components.PositionComponent)
			break
		}
	}

	if firstGoldEntity == 0 {
		t.Fatal("Could not find first gold character")
	}

	// Type the gold character correctly
	scoreSystem.HandleCharacterTyping(world, firstGoldPos.X, firstGoldPos.Y, firstGoldChar.Rune)

	// Heat should not have changed
	if ctx.GetScoreIncrement() != initialHeat {
		t.Errorf("Heat should not change when typing gold sequence, was %d, now %d", initialHeat, ctx.GetScoreIncrement())
	}
}

// TestGoldSequenceCompletionFillsHeat tests that completing gold sequence fills heat to max
func TestGoldSequenceCompletionFillsHeat(t *testing.T) {
	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx := createTestContext(mockTime)
	world := ctx.World

	decaySystem := NewDecaySystem(ctx.GameWidth, ctx.GameHeight, ctx.Width, 0, ctx)
	goldSystem := NewGoldSequenceSystem(ctx, decaySystem, ctx.GameWidth, ctx.GameHeight)
	scoreSystem := NewScoreSystem(ctx)
	scoreSystem.SetGoldSequenceSystem(goldSystem)

	// Spawn gold sequence
	decaySystem.animating = true
	goldSystem.Update(world, 16*time.Millisecond)
	decaySystem.animating = false
	goldSystem.Update(world, 16*time.Millisecond)

	// Set low initial heat
	ctx.SetScoreIncrement(5)

	// Type all gold characters
	for i := 0; i < constants.GoldSequenceLength; i++ {
		// Find gold character at index i
		seqType := reflect.TypeOf(components.SequenceComponent{})
		charType := reflect.TypeOf(components.CharacterComponent{})
		posType := reflect.TypeOf(components.PositionComponent{})
		entities := world.GetEntitiesWith(seqType, charType, posType)

		var targetEntity engine.Entity
		var targetChar components.CharacterComponent
		var targetPos components.PositionComponent

		for _, entity := range entities {
			seqComp, _ := world.GetComponent(entity, seqType)
			seq := seqComp.(components.SequenceComponent)
			if seq.Type == components.SequenceGold && seq.Index == i {
				targetEntity = entity
				charComp, _ := world.GetComponent(entity, charType)
				targetChar = charComp.(components.CharacterComponent)
				posComp, _ := world.GetComponent(entity, posType)
				targetPos = posComp.(components.PositionComponent)
				break
			}
		}

		if targetEntity == 0 {
			t.Fatalf("Could not find gold character at index %d", i)
		}

		// Type the character
		scoreSystem.HandleCharacterTyping(world, targetPos.X, targetPos.Y, targetChar.Rune)
	}

	// Heat should be at max
	heatBarWidth := ctx.Width - constants.HeatBarIndicatorWidth
	if ctx.GetScoreIncrement() != heatBarWidth {
		t.Errorf("Heat should be at max (%d) after completing gold sequence, got %d", heatBarWidth, ctx.GetScoreIncrement())
	}

	// Gold sequence should be inactive
	if goldSystem.IsActive() {
		t.Error("Gold sequence should be inactive after completion")
	}
}

// TestGoldSequenceDoesNotReduceHigherHeat tests that gold completion doesn't reduce heat if already higher
func TestGoldSequenceDoesNotReduceHigherHeat(t *testing.T) {
	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx := createTestContext(mockTime)
	world := ctx.World

	decaySystem := NewDecaySystem(ctx.GameWidth, ctx.GameHeight, ctx.Width, 0, ctx)
	goldSystem := NewGoldSequenceSystem(ctx, decaySystem, ctx.GameWidth, ctx.GameHeight)
	scoreSystem := NewScoreSystem(ctx)
	scoreSystem.SetGoldSequenceSystem(goldSystem)

	// Spawn gold sequence
	decaySystem.animating = true
	goldSystem.Update(world, 16*time.Millisecond)
	decaySystem.animating = false
	goldSystem.Update(world, 16*time.Millisecond)

	// Set heat higher than max
	heatBarWidth := ctx.Width - constants.HeatBarIndicatorWidth
	highHeat := heatBarWidth + 50
	ctx.SetScoreIncrement(highHeat)

	// Type all gold characters
	for i := 0; i < constants.GoldSequenceLength; i++ {
		seqType := reflect.TypeOf(components.SequenceComponent{})
		charType := reflect.TypeOf(components.CharacterComponent{})
		posType := reflect.TypeOf(components.PositionComponent{})
		entities := world.GetEntitiesWith(seqType, charType, posType)

		for _, entity := range entities {
			seqComp, ok := world.GetComponent(entity, seqType)
			if !ok {
				continue
			}
			seq := seqComp.(components.SequenceComponent)
			if seq.Type == components.SequenceGold && seq.Index == i {
				charComp, _ := world.GetComponent(entity, charType)
				targetChar := charComp.(components.CharacterComponent)
				posComp, _ := world.GetComponent(entity, posType)
				targetPos := posComp.(components.PositionComponent)

				scoreSystem.HandleCharacterTyping(world, targetPos.X, targetPos.Y, targetChar.Rune)
				break
			}
		}
	}

	// Heat should remain at high value
	if ctx.GetScoreIncrement() != highHeat {
		t.Errorf("Heat should remain at %d when already higher than max, got %d", highHeat, ctx.GetScoreIncrement())
	}
}

// TestIgnoringGoldSequenceHasNoEffect tests that ignoring gold sequence doesn't affect game
func TestIgnoringGoldSequenceHasNoEffect(t *testing.T) {
	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx := createTestContext(mockTime)
	world := ctx.World

	decaySystem := NewDecaySystem(ctx.GameWidth, ctx.GameHeight, ctx.Width, 0, ctx)
	goldSystem := NewGoldSequenceSystem(ctx, decaySystem, ctx.GameWidth, ctx.GameHeight)

	// Spawn gold sequence
	decaySystem.animating = true
	goldSystem.Update(world, 16*time.Millisecond)
	decaySystem.animating = false
	goldSystem.Update(world, 16*time.Millisecond)

	// Set initial heat
	ctx.SetScoreIncrement(10)
	initialHeat := ctx.GetScoreIncrement()
	initialScore := ctx.GetScore()

	// Wait for timeout without typing anything
	mockTime.Advance(11 * time.Second)
	goldSystem.Update(world, 16*time.Millisecond)

	// Heat and score should be unchanged
	if ctx.GetScoreIncrement() != initialHeat {
		t.Errorf("Heat should be unchanged after ignoring gold sequence, was %d, now %d", initialHeat, ctx.GetScoreIncrement())
	}

	if ctx.GetScore() != initialScore {
		t.Errorf("Score should be unchanged after ignoring gold sequence, was %d, now %d", initialScore, ctx.GetScore())
	}

	// Gold sequence should be inactive
	if goldSystem.IsActive() {
		t.Error("Gold sequence should be inactive after timeout")
	}
}

// Helper function to create a test context
func createTestContext(timeProvider engine.TimeProvider) *engine.GameContext {
	ctx := &engine.GameContext{
		World:        engine.NewWorld(),
		TimeProvider: timeProvider,
		Width:        100,
		Height:       30,
		GameWidth:    90,
		GameHeight:   26,
	}
	ctx.SetScore(0)
	ctx.SetScoreIncrement(0)
	return ctx
}
