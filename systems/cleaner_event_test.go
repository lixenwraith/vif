package systems

import (
	"reflect"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestCleanerSpawnOnEvent verifies that cleaners spawn when EventCleanerRequest is pushed
func TestCleanerSpawnOnEvent(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	// Create Red characters across multiple rows
	for row := 0; row < 5; row++ {
		createRedCharacterAt(world, 10+row*5, row)
	}

	// Verify no cleaners exist initially
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	entities := world.GetEntitiesWith(cleanerType)
	if len(entities) != 0 {
		t.Fatalf("Expected no cleaners initially, got %d", len(entities))
	}

	// Push EventCleanerRequest
	ctx.PushEvent(engine.EventCleanerRequest, nil)

	// Run Update to process event
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Verify cleaners were spawned
	entities = world.GetEntitiesWith(cleanerType)
	if len(entities) == 0 {
		t.Fatal("Expected cleaners to be spawned after EventCleanerRequest")
	}

	// Verify one cleaner per red row
	expectedCleaners := 5 // 5 rows with red characters
	if len(entities) != expectedCleaners {
		t.Errorf("Expected %d cleaners, got %d", expectedCleaners, len(entities))
	}
}

// TestCleanerFinishedEvent verifies that EventCleanerFinished is emitted when cleaners complete
func TestCleanerFinishedEvent(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	// Create a single Red character
	createRedCharacterAt(world, 40, 5)

	// Push EventCleanerRequest to spawn cleaners
	ctx.PushEvent(engine.EventCleanerRequest, nil)
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Verify cleaner was spawned
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	entities := world.GetEntitiesWith(cleanerType)
	if len(entities) == 0 {
		t.Fatal("Expected cleaner to be spawned")
	}

	// Consume the CleanerRequest event to clear the queue
	ctx.ConsumeEvents()

	// Simulate cleaner animation completing (run updates until cleaner is destroyed)
	maxUpdates := 200 // Safety limit to prevent infinite loop
	for i := 0; i < maxUpdates; i++ {
		cleanerSystem.Update(world, 16*time.Millisecond)
		entities = world.GetEntitiesWith(cleanerType)
		if len(entities) == 0 {
			// Cleaners are gone, break
			break
		}
	}

	// Verify cleaners are gone
	if len(entities) != 0 {
		t.Fatalf("Expected cleaners to complete after %d updates, but %d cleaners still exist", maxUpdates, len(entities))
	}

	// Check for EventCleanerFinished in the event queue
	events := ctx.PeekEvents()
	hasFinishedEvent := false
	for _, event := range events {
		if event.Type == engine.EventCleanerFinished {
			hasFinishedEvent = true
			break
		}
	}

	if !hasFinishedEvent {
		t.Error("Expected EventCleanerFinished to be emitted when cleaners complete")
	}
}

// TestNoDuplicateSpawnsForSameFrame verifies that duplicate events for the same frame don't cause double spawning
func TestNoDuplicateSpawnsForSameFrame(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	// Create Red characters
	for row := 0; row < 3; row++ {
		createRedCharacterAt(world, 40, row)
	}

	// Set a specific frame number
	ctx.State.IncrementFrameNumber()
	currentFrame := ctx.State.GetFrameNumber()

	// Push multiple EventCleanerRequest events for the SAME frame
	for i := 0; i < 5; i++ {
		event := engine.GameEvent{
			Type:      engine.EventCleanerRequest,
			Payload:   nil,
			Frame:     currentFrame, // Same frame
			Timestamp: ctx.TimeProvider.Now(),
		}
		ctx.PushEvent(event.Type, event.Payload)
	}

	// Run Update to process events
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Verify cleaners were spawned only once (one per row)
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	entities := world.GetEntitiesWith(cleanerType)
	expectedCleaners := 3 // 3 rows with red characters

	if len(entities) != expectedCleaners {
		t.Errorf("Expected %d cleaners (no duplicates), got %d", expectedCleaners, len(entities))
	}
}

// TestMultipleFrameEvents verifies that events from different frames can spawn cleaners multiple times
func TestMultipleFrameEvents(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	// Create Red characters
	createRedCharacterAt(world, 40, 5)

	// Spawn cleaners for frame 1
	ctx.State.IncrementFrameNumber()
	ctx.PushEvent(engine.EventCleanerRequest, nil)
	cleanerSystem.Update(world, 16*time.Millisecond)

	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	entities := world.GetEntitiesWith(cleanerType)
	firstSpawnCount := len(entities)
	if firstSpawnCount == 0 {
		t.Fatal("Expected cleaners to spawn for first frame")
	}

	// Let cleaners complete (destroy them manually for this test)
	for _, entity := range entities {
		world.DestroyEntity(entity)
	}

	// Consume events to clear EventCleanerFinished
	ctx.ConsumeEvents()

	// Create more red characters
	createRedCharacterAt(world, 40, 10)

	// Spawn cleaners for frame 2
	ctx.State.IncrementFrameNumber()
	ctx.PushEvent(engine.EventCleanerRequest, nil)
	cleanerSystem.Update(world, 16*time.Millisecond)

	entities = world.GetEntitiesWith(cleanerType)
	secondSpawnCount := len(entities)
	if secondSpawnCount == 0 {
		t.Fatal("Expected cleaners to spawn for second frame")
	}

	// Both should have spawned cleaners successfully
	if firstSpawnCount == 0 || secondSpawnCount == 0 {
		t.Errorf("Expected cleaners to spawn for both frames, got %d and %d", firstSpawnCount, secondSpawnCount)
	}
}

// TestEventFrameTracking verifies that the spawned map correctly tracks frames
func TestEventFrameTracking(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	// Create Red characters
	createRedCharacterAt(world, 40, 5)

	// Set frame to 1
	ctx.State.IncrementFrameNumber()
	frame1 := ctx.State.GetFrameNumber()

	// Push event for frame 1
	ctx.PushEvent(engine.EventCleanerRequest, nil)
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Verify spawned map has frame 1
	if !cleanerSystem.spawned[frame1] {
		t.Error("Expected spawned map to have frame 1")
	}

	// Advance many frames to trigger cleanup (>10 frames)
	for i := 0; i < 12; i++ {
		ctx.State.IncrementFrameNumber()
		cleanerSystem.Update(world, 16*time.Millisecond)
	}

	// Verify frame 1 was cleaned up from spawned map
	if cleanerSystem.spawned[frame1] {
		t.Error("Expected frame 1 to be cleaned up from spawned map after 12 frames")
	}

	// Verify spawned map doesn't grow indefinitely
	if len(cleanerSystem.spawned) > 10 {
		t.Errorf("Expected spawned map to keep at most 10 entries, got %d", len(cleanerSystem.spawned))
	}
}
