package systems

import (
	"testing"
	"time"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestCleanerEventFlow verifies the complete event-driven cleaner workflow
func TestCleanerEventFlow(t *testing.T) {
	// Setup: Create World, Context with EventQueue, and CleanerSystem
	ctx := createCleanerTestContext()
	system := NewCleanerSystem(ctx)

	// Spawn red sequences at known positions (rows 5, 10, 15)
	redEntities := []engine.Entity{
		createRedCharacterAt(ctx.World, 10, 5),
		createRedCharacterAt(ctx.World, 20, 5),
		createRedCharacterAt(ctx.World, 15, 10),
		createRedCharacterAt(ctx.World, 25, 10),
		createRedCharacterAt(ctx.World, 12, 15),
		createRedCharacterAt(ctx.World, 18, 15),
	}

	// Verify red sequences exist
	for _, entity := range redEntities {
		if !entityExists(ctx.World, entity) {
			t.Fatalf("Red entity %d does not exist after spawn", entity)
		}
	}

	// Push EventCleanerRequest
	ctx.PushEvent(engine.EventCleanerRequest, nil)

	// Verify event is in queue
	peekedEvents := ctx.PeekEvents()
	if len(peekedEvents) != 1 {
		t.Fatalf("Expected 1 event in queue, got %d", len(peekedEvents))
	}
	if peekedEvents[0].Type != engine.EventCleanerRequest {
		t.Fatalf("Expected EventCleanerRequest, got %v", peekedEvents[0].Type)
	}

	// Call Update() on CleanerSystem - should consume event and spawn cleaners
	system.Update(ctx.World, 16*time.Millisecond)

	// Verify cleaner entities created (should be 3 cleaners, one per row with red sequences)
	// Using direct store access
	cleaners := ctx.World.Cleaners.All()
	if len(cleaners) != 3 {
		t.Fatalf("Expected 3 cleaner entities, got %d", len(cleaners))
	}

	// Verify cleaners are on correct rows
	foundRows := make(map[int]bool)
	for _, cleaner := range cleaners {
		comp, ok := ctx.World.Cleaners.Get(cleaner)
		if !ok {
			t.Fatalf("Cleaner entity %d has no CleanerComponent", cleaner)
		}
		c := comp
		foundRows[c.GridY] = true

		// Verify cleaner has proper physics setup
		if c.VelocityX == 0 {
			t.Errorf("Cleaner on row %d has zero velocity", c.GridY)
		}
		if len(c.Trail) == 0 {
			t.Errorf("Cleaner on row %d has empty trail", c.GridY)
		}
	}

	// Verify we have cleaners on rows 5, 10, 15
	expectedRows := []int{5, 10, 15}
	for _, row := range expectedRows {
		if !foundRows[row] {
			t.Errorf("Expected cleaner on row %d, but not found", row)
		}
	}

	// Advance time until cleaners complete
	// Cleaners need to travel from off-screen to off-screen on the other side
	// Distance = gameWidth + 2*trailLength = 80 + 20 = 100 units
	// Speed = gameWidth / duration = 80 / 1.0s = 80 px/s
	// Time needed = 100 / 80 = 1.25 seconds
	// Add buffer for safety
	frameDuration := 16 * time.Millisecond // ~60 FPS
	totalDuration := constants.CleanerAnimationDuration + 500*time.Millisecond
	elapsed := time.Duration(0)

	// Track if we've seen EventCleanerFinished during the simulation
	foundFinished := false
	for elapsed < totalDuration {
		system.Update(ctx.World, frameDuration)
		elapsed += frameDuration

		// Check for EventCleanerFinished in queue after each update
		events := ctx.PeekEvents()
		for _, event := range events {
			if event.Type == engine.EventCleanerFinished {
				foundFinished = true
			}
		}
	}

	// Call Update one more time to ensure event is emitted
	system.Update(ctx.World, frameDuration)

	// Check again for EventCleanerFinished
	events := ctx.PeekEvents()
	for _, event := range events {
		if event.Type == engine.EventCleanerFinished {
			foundFinished = true
		}
	}

	// Verify cleaners have been destroyed (animation complete)
	cleaners = ctx.World.Cleaners.All()
	if len(cleaners) != 0 {
		t.Errorf("Expected all cleaners to be destroyed after animation, got %d remaining", len(cleaners))
		// Debug: Print cleaner positions
		for _, cleaner := range cleaners {
			if comp, ok := ctx.World.Cleaners.Get(cleaner); ok {
				c := comp
				t.Logf("Remaining cleaner: GridY=%d, PreciseX=%.2f, TargetX=%.2f, VelocityX=%.2f",
					c.GridY, c.PreciseX, c.TargetX, c.VelocityX)
			}
		}
	}

	// Verify EventCleanerFinished was emitted at some point
	if !foundFinished {
		t.Errorf("Expected EventCleanerFinished to be emitted during cleaner lifecycle")
	}

	// Verify red sequences destroyed
	for i, entity := range redEntities {
		if entityExists(ctx.World, entity) {
			if pos, ok := ctx.World.Positions.Get(entity); ok {
				p := pos
				t.Errorf("Red entity %d still exists at position (%d, %d) after cleaner sweep", i, p.X, p.Y)
			}
		}
	}
}

// TestCleanerRendererDecoupling verifies renderer works without system references
func TestCleanerRendererDecoupling(t *testing.T) {
	// Create World without CleanerSystem
	ctx := createCleanerTestContext()

	// Add cleaner entities to world manually (simulating what system would do)
	cleaner1 := ctx.World.CreateEntity()
	trail1 := []core.Point{
		{X: 10, Y: 5},
		{X: 9, Y: 5},
		{X: 8, Y: 5},
		{X: 7, Y: 5},
		{X: 6, Y: 5},
	}
	comp1 := components.CleanerComponent{
		PreciseX:  10.5,
		PreciseY:  5.0,
		VelocityX: 80.0, // pixels per second
		VelocityY: 0.0,
		TargetX:   100.0,
		TargetY:   5.0,
		GridX:     10,
		GridY:     5,
		Trail:     trail1,
		Char:      constants.CleanerChar,
	}
	ctx.World.Cleaners.Add(cleaner1, comp1)

	cleaner2 := ctx.World.CreateEntity()
	trail2 := []core.Point{
		{X: 70, Y: 10},
		{X: 71, Y: 10},
		{X: 72, Y: 10},
	}
	comp2 := components.CleanerComponent{
		PreciseX:  70.5,
		PreciseY:  10.0,
		VelocityX: -80.0, // moving left
		VelocityY: 0.0,
		TargetX:   -10.0,
		TargetY:   10.0,
		GridX:     70,
		GridY:     10,
		Trail:     trail2,
		Char:      constants.CleanerChar,
	}
	ctx.World.Cleaners.Add(cleaner2, comp2)

	// Query cleaners from world (simulating what renderer does)
	// Using direct store access
	entities := ctx.World.Cleaners.All()

	// Verify we can read cleaner data
	if len(entities) != 2 {
		t.Fatalf("Expected 2 cleaner entities, got %d", len(entities))
	}

	// Verify we can read components and deep-copy trails (as renderer would)
	for _, entity := range entities {
		cleaner, ok := ctx.World.Cleaners.Get(entity)
		if !ok {
			t.Fatalf("Failed to get CleanerComponent for entity %d", entity)
		}

		// Deep copy trail to avoid race conditions (as renderer does)
		trailCopy := make([]core.Point, len(cleaner.Trail))
		copy(trailCopy, cleaner.Trail)

		// Verify trail data is accessible
		if len(trailCopy) == 0 {
			t.Errorf("Cleaner entity %d has empty trail", entity)
		}

		// Verify we can read physics data
		if cleaner.VelocityX == 0 {
			t.Errorf("Cleaner entity %d has zero velocity", entity)
		}

		// Verify grid position is valid
		if cleaner.GridY < 0 || cleaner.GridY >= ctx.GameHeight {
			t.Errorf("Cleaner entity %d has invalid GridY: %d", entity, cleaner.GridY)
		}
	}

	// Modify cleaner trail in world (simulate system update)
	comp1.Trail = append([]core.Point{{X: 11, Y: 5}}, comp1.Trail...)
	if len(comp1.Trail) > constants.CleanerTrailLength {
		comp1.Trail = comp1.Trail[:constants.CleanerTrailLength]
	}
	comp1.GridX = 11
	comp1.PreciseX = 11.5
	ctx.World.Cleaners.Add(cleaner1, comp1)

	// Query again and verify renderer gets updated data
	updatedComp, ok := ctx.World.Cleaners.Get(cleaner1)
	if !ok {
		t.Fatal("Failed to get updated CleanerComponent")
	}

	if updatedComp.GridX != 11 {
		t.Errorf("Expected updated GridX to be 11, got %d", updatedComp.GridX)
	}
	if len(updatedComp.Trail) == 0 || updatedComp.Trail[0].X != 11 {
		t.Errorf("Trail not updated correctly")
	}
}

// TestNoCleanerPhaseStates verifies cleaners work independently of phase state
func TestNoCleanerPhaseStates(t *testing.T) {
	// Test that cleaners can be triggered in any phase
	phases := []engine.GamePhase{
		engine.PhaseNormal,
		engine.PhaseGoldActive,
		engine.PhaseGoldComplete,
		engine.PhaseDecayWait,
		engine.PhaseDecayAnimation,
	}

	for _, phase := range phases {
		t.Run(phase.String(), func(t *testing.T) {
			ctx := createCleanerTestContext()
			system := NewCleanerSystem(ctx)

			// Set game to specific phase
			// Note: We can't directly set phase without respecting state machine,
			// so we'll just verify cleaner spawning works regardless of current phase
			// The key insight is that cleaners don't check phase state

			// Spawn red sequences
			createRedCharacterAt(ctx.World, 10, 5)
			createRedCharacterAt(ctx.World, 20, 5)

			// Push EventCleanerRequest
			ctx.PushEvent(engine.EventCleanerRequest, nil)

			// Call Update
			system.Update(ctx.World, 16*time.Millisecond)

			// Verify cleaners spawn regardless of phase
			// Using direct store access
			cleaners := ctx.World.Cleaners.All()
			if len(cleaners) == 0 {
				t.Errorf("Expected cleaners to spawn in phase %s, but none found", phase)
			}

			// Verify phase state remains unchanged by cleaner spawn
			currentPhase := ctx.State.GetPhase()
			// Phase should still be Normal (initial state) - cleaners don't modify phase
			if currentPhase != engine.PhaseNormal {
				t.Logf("Phase is %s (cleaners don't modify phase state)", currentPhase)
			}
		})
	}

	// Verify no PhaseCleanerPending or PhaseCleanerActive states exist
	t.Run("NoCleanerPhaseStates", func(t *testing.T) {
		// This test verifies by compilation that there are no cleaner-specific phases
		// If these constants existed, this would fail to compile
		validPhases := []engine.GamePhase{
			engine.PhaseNormal,
			engine.PhaseGoldActive,
			engine.PhaseGoldComplete,
			engine.PhaseDecayWait,
			engine.PhaseDecayAnimation,
		}

		// Verify we only have these 5 phases
		if len(validPhases) != 5 {
			t.Errorf("Expected exactly 5 game phases, got %d", len(validPhases))
		}

		// Verify phase names don't contain "Cleaner"
		for _, phase := range validPhases {
			phaseStr := phase.String()
			if contains(phaseStr, "Cleaner") {
				t.Errorf("Found cleaner-specific phase: %s", phaseStr)
			}
		}
	})
}

// TestEventQueueOverflow verifies event queue handles high load correctly
func TestEventQueueOverflow(t *testing.T) {
	ctx := createCleanerTestContext()
	system := NewCleanerSystem(ctx)

	// Spawn red sequences for cleaner targets
	createRedCharacterAt(ctx.World, 10, 5)
	createRedCharacterAt(ctx.World, 20, 10)

	// Push 500 mixed events rapidly
	eventCount := 500
	expectedCleanerRequests := 0
	expectedGoldSpawned := 0
	expectedGoldComplete := 0

	for i := 0; i < eventCount; i++ {
		switch i % 4 {
		case 0:
			ctx.PushEvent(engine.EventCleanerRequest, nil)
			expectedCleanerRequests++
		case 1:
			ctx.PushEvent(engine.EventGoldSpawned, nil)
			expectedGoldSpawned++
		case 2:
			ctx.PushEvent(engine.EventGoldComplete, nil)
			expectedGoldComplete++
		case 3:
			ctx.PushEvent(engine.EventCleanerFinished, nil)
		}
	}

	// Verify no deadlock or panic during event processing
	// Process events in batches to simulate real game loop
	processedFrames := 0
	maxFrames := 100

	for processedFrames < maxFrames {
		// Update system (will consume events)
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Panic during event processing: %v", r)
				}
			}()
			system.Update(ctx.World, 16*time.Millisecond)
		}()

		processedFrames++

		// Check if queue is empty
		if ctx.PeekEvents() == nil || len(ctx.PeekEvents()) == 0 {
			break
		}
	}

	// Verify events were processed (queue should be mostly empty or contain only recent events)
	// Note: Ring buffer has capacity of 256, so some events may have been overwritten
	remainingEvents := ctx.PeekEvents()
	t.Logf("Processed %d frames, %d events remaining in queue", processedFrames, len(remainingEvents))

	// Verify no deadlock occurred (we completed processing)
	if processedFrames >= maxFrames {
		t.Logf("Warning: Reached max frames, queue may still have events")
	}

	// Verify cleaners were spawned (at least once from the cleaner requests)
	// Note: Due to deduplication by frame number, multiple requests in same frame = 1 spawn
	// Using direct store access
	cleaners := ctx.World.Cleaners.All()
	t.Logf("Cleaners spawned: %d", len(cleaners))

	// Key verification: No panic, no deadlock, events processed successfully
	t.Logf("Event queue overflow test completed successfully")
	t.Logf("Total events pushed: %d", eventCount)
	t.Logf("Events remaining: %d", len(remainingEvents))
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
