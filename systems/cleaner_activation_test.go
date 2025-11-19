package systems

import (
	"reflect"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestCleanersTriggerConditions verifies cleaners only activate when heat is at max during gold completion
func TestCleanersTriggerConditions(t *testing.T) {
	tests := []struct {
		name          string
		currentHeat   int
		maxHeat       int
		shouldTrigger bool
	}{
		{
			name:          "Heat below max - no trigger",
			currentHeat:   50,
			maxHeat:       100,
			shouldTrigger: false,
		},
		{
			name:          "Heat at max - should trigger",
			currentHeat:   100,
			maxHeat:       100,
			shouldTrigger: true,
		},
		{
			name:          "Heat above max - should trigger",
			currentHeat:   110,
			maxHeat:       100,
			shouldTrigger: true,
		},
		{
			name:          "Heat zero - no trigger",
			currentHeat:   0,
			maxHeat:       100,
			shouldTrigger: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			world := engine.NewWorld()
			ctx := createCleanerTestContext()

			// Create cleaner system
			cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
			defer cleanerSystem.Shutdown()

			// Create some Red characters to clean
			createRedCharacterAt(world, 10, 5)

			// Test cleaner triggering via GameState
			// Simulate gold completion at max heat
			if tt.currentHeat >= tt.maxHeat {
				ctx.State.RequestCleaners()
			}

			// Check if cleaners should be triggered
			if ctx.State.GetCleanerPending() {
				ctx.State.ActivateCleaners()
				cleanerSystem.ActivateCleaners(world)
			}

			// Process spawn requests
			cleanerSystem.Update(world, 16*time.Millisecond)

			// Wait a bit for async processing
			time.Sleep(50 * time.Millisecond)

			// Verify activation state
			if cleanerSystem.IsActive() != tt.shouldTrigger {
				t.Errorf("Expected IsActive=%v, got %v", tt.shouldTrigger, cleanerSystem.IsActive())
			}

			// Verify cleaner entities were created if triggered
			cleanerType := reflect.TypeOf(components.CleanerComponent{})
			cleaners := world.GetEntitiesWith(cleanerType)

			if tt.shouldTrigger && len(cleaners) == 0 {
				t.Error("Expected cleaners to be created when triggered, but none found")
			}
			if !tt.shouldTrigger && len(cleaners) > 0 {
				t.Errorf("Expected no cleaners when not triggered, but found %d", len(cleaners))
			}
		})
	}
}

// TestCleanerActivationWithoutRed verifies cleaners activate even with no Red text (phantom cleaners)
// This ensures proper phase transitions when no visual cleaners are needed
func TestCleanerActivationWithoutRed(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Create only Blue and Green characters (no Red)
	createBlueCharacterAt(world, 10, 5)
	createBlueCharacterAt(world, 20, 10)
	createGreenCharacterAt(world, 30, 15)
	createGreenCharacterAt(world, 40, 20)

	// Verify no Red characters exist
	seqType := reflect.TypeOf(components.SequenceComponent{})
	entities := world.GetEntitiesWith(seqType)
	for _, entity := range entities {
		seqComp, ok := world.GetComponent(entity, seqType)
		if ok {
			seq := seqComp.(components.SequenceComponent)
			if seq.Type == components.SequenceRed {
				t.Fatal("Test setup error: Red character found when none should exist")
			}
		}
	}

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)

	// Process spawn request
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Wait for processing
	time.Sleep(50 * time.Millisecond)

	// CRITICAL: Verify isActive = true even though no Red characters exist
	if !cleanerSystem.IsActive() {
		t.Error("Expected cleaners to be active (phantom activation) even without Red text")
	}

	// Verify NO visual cleaner entities were spawned
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)
	if len(cleaners) != 0 {
		t.Errorf("Expected 0 visual cleaners (phantom mode), got %d", len(cleaners))
	}

	// Verify phase transitions work properly by checking animation completion
	// Initially should NOT be complete (animation just started)
	if cleanerSystem.IsAnimationComplete() {
		t.Error("Expected animation NOT complete immediately after activation")
	}

	// Simulate passage of animation duration
	time.Sleep(constants.DefaultCleanerConfig().AnimationDuration + 100*time.Millisecond)

	// Now animation should be complete
	if !cleanerSystem.IsAnimationComplete() {
		t.Error("Expected animation complete after duration elapsed")
	}

	// Update to trigger cleanup
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Verify cleaners deactivated after animation completes
	if cleanerSystem.IsActive() {
		t.Error("Expected cleaners to be inactive after animation completion")
	}

	// Verify Blue/Green characters remain untouched
	entities = world.GetEntitiesWith(seqType)
	blueCount := 0
	greenCount := 0
	for _, entity := range entities {
		seqComp, ok := world.GetComponent(entity, seqType)
		if ok {
			seq := seqComp.(components.SequenceComponent)
			if seq.Type == components.SequenceBlue {
				blueCount++
			} else if seq.Type == components.SequenceGreen {
				greenCount++
			}
		}
	}

	if blueCount != 2 {
		t.Errorf("Expected 2 Blue characters to remain, got %d", blueCount)
	}
	if greenCount != 2 {
		t.Errorf("Expected 2 Green characters to remain, got %d", greenCount)
	}
}

// TestCleanersDuplicateTriggerIgnored verifies duplicate triggers are ignored
func TestCleanersDuplicateTriggerIgnored(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Create Red characters
	createRedCharacterAt(world, 10, 5)

	// First trigger
	cleanerSystem.TriggerCleaners(world)

	// Process first request
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Wait for processing
	time.Sleep(50 * time.Millisecond)

	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners1 := world.GetEntitiesWith(cleanerType)
	count1 := len(cleaners1)

	// Second trigger (should be ignored)
	cleanerSystem.TriggerCleaners(world)

	// Process second request
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Wait for processing
	time.Sleep(50 * time.Millisecond)

	cleaners2 := world.GetEntitiesWith(cleanerType)
	count2 := len(cleaners2)

	if count1 != count2 {
		t.Errorf("Duplicate trigger created new cleaners: before=%d, after=%d", count1, count2)
	}
}
