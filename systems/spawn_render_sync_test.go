package systems

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestSpawnRenderConcurrency tests that SpawnSystem updates don't interfere with rendering
// This test simulates concurrent spawn and render operations to detect race conditions
func TestSpawnRenderConcurrency(t *testing.T) {
	// Create test context
	timeProvider := engine.NewMonotonicTimeProvider()
	ctx := createTestContext(timeProvider)
	world := engine.NewWorld()

	// Create spawn system
	spawnSystem := NewSpawnSystem(80, 24, 40, 12, ctx)
	world.AddSystem(spawnSystem)

	posType := reflect.TypeOf(components.PositionComponent{})
	charType := reflect.TypeOf(components.CharacterComponent{})

	var wg sync.WaitGroup
	iterations := 100
	errChan := make(chan error, 10)

	// Goroutine 1: Continuously spawn entities (simulates SpawnSystem.Update)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			world.Update(16 * time.Millisecond)
			time.Sleep(1 * time.Millisecond) // Small delay to allow concurrent access
		}
	}()

	// Goroutine 2: Continuously read entities (simulates rendering)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations*2; i++ {
			// Wait for updates to complete (frame barrier)
			world.WaitForUpdates()

			// Simulate renderer reading entities
			entities := world.GetEntitiesWith(posType, charType)

			for _, entity := range entities {
				// Defensive read with nil checks (as in actual renderer)
				posComp, ok := world.GetComponent(entity, posType)
				if !ok {
					continue // Entity destroyed between GetEntitiesWith and GetComponent
				}

				charComp, ok := world.GetComponent(entity, charType)
				if !ok {
					continue // Component removed
				}

				// Type assertion should not panic
				pos, ok := posComp.(components.PositionComponent)
				if !ok {
					errChan <- &ComponentTypeError{Expected: "PositionComponent", Got: reflect.TypeOf(posComp).Name()}
					return
				}

				char, ok := charComp.(components.CharacterComponent)
				if !ok {
					errChan <- &ComponentTypeError{Expected: "CharacterComponent", Got: reflect.TypeOf(charComp).Name()}
					return
				}

				// Validate component data
				if pos.X < 0 || pos.X >= 80 || pos.Y < 0 || pos.Y >= 24 {
					errChan <- &InvalidPositionError{X: pos.X, Y: pos.Y}
					return
				}

				if char.Rune == 0 {
					errChan <- &InvalidCharacterError{Rune: char.Rune}
					return
				}
			}

			time.Sleep(1 * time.Millisecond)
		}
	}()

	wg.Wait()
	close(errChan)

	// Check for errors
	for err := range errChan {
		t.Errorf("Race condition detected: %v", err)
	}
}

// TestEntitySnapshotConsistency tests that GetEntitiesWithSnapshot provides atomic reads
func TestEntitySnapshotConsistency(t *testing.T) {
	world := engine.NewWorld()

	posType := reflect.TypeOf(components.PositionComponent{})
	charType := reflect.TypeOf(components.CharacterComponent{})

	// Create entities with both components
	for i := 0; i < 50; i++ {
		entity := world.CreateEntity()
		world.AddComponent(entity, components.PositionComponent{X: i, Y: i})
		world.AddComponent(entity, components.CharacterComponent{Rune: 'A', Style: tcell.StyleDefault})
		world.UpdateSpatialIndex(entity, i, i)
	}

	var wg sync.WaitGroup

	// Concurrent readers using snapshot
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				snapshots := world.GetEntitiesWithSnapshot(posType, charType)

				// All snapshots should have both components
				for _, snapshot := range snapshots {
					if _, ok := snapshot.Components[posType]; !ok {
						t.Errorf("Snapshot missing PositionComponent for entity %d", snapshot.Entity)
					}
					if _, ok := snapshot.Components[charType]; !ok {
						t.Errorf("Snapshot missing CharacterComponent for entity %d", snapshot.Entity)
					}
				}
			}
		}()
	}

	// Concurrent writers modifying entities
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				entity := world.CreateEntity()
				world.AddComponent(entity, components.PositionComponent{X: goroutineID, Y: j})
				world.AddComponent(entity, components.CharacterComponent{Rune: 'B', Style: tcell.StyleDefault})
				time.Sleep(1 * time.Millisecond)
				world.SafeDestroyEntity(entity)
			}
		}(i)
	}

	wg.Wait()
}

// TestFrameBarrier tests that the frame barrier prevents rendering during updates
func TestFrameBarrier(t *testing.T) {
	world := engine.NewWorld()

	var updateComplete bool
	var updateMutex sync.Mutex
	var updateStarted sync.WaitGroup
	updateStarted.Add(1)

	// Create a test system that takes some time to update
	testSystem := &SlowUpdateSystem{
		updateFunc: func() {
			updateMutex.Lock()
			updateComplete = false
			updateMutex.Unlock()

			// Signal that update has started
			updateStarted.Done()

			time.Sleep(50 * time.Millisecond) // Simulate slow update

			updateMutex.Lock()
			updateComplete = true
			updateMutex.Unlock()
		},
	}
	world.AddSystem(testSystem)

	var wg sync.WaitGroup

	// Start update in background
	wg.Add(1)
	go func() {
		defer wg.Done()
		world.Update(16 * time.Millisecond)
	}()

	// Wait for update to start
	updateStarted.Wait()

	// Verify update is in progress but not complete
	updateMutex.Lock()
	inProgress := !updateComplete
	updateMutex.Unlock()

	if !inProgress {
		t.Error("Update completed too quickly")
	}

	// Now wait for update to complete
	world.WaitForUpdates()

	// After barrier, update should be complete
	updateMutex.Lock()
	complete := updateComplete
	updateMutex.Unlock()

	if !complete {
		t.Error("Frame barrier returned before update completed")
	}

	wg.Wait()
}

// TestAtomicSequenceIDGeneration tests that sequence IDs are generated atomically
func TestAtomicSequenceIDGeneration(t *testing.T) {
	timeProvider := engine.NewMonotonicTimeProvider()
	ctx := createTestContext(timeProvider)
	spawnSystem := NewSpawnSystem(80, 24, 40, 12, ctx)

	var wg sync.WaitGroup
	seqIDs := make(chan int, 1000)

	// Multiple goroutines generating sequence IDs
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				// Simulate atomic sequence ID generation from GameState
				seqID := ctx.State.IncrementSeqID()
				seqIDs <- seqID
			}
		}()
	}

	wg.Wait()
	close(seqIDs)

	// Check for duplicate IDs
	seen := make(map[int]bool)
	for id := range seqIDs {
		if seen[id] {
			t.Errorf("Duplicate sequence ID generated: %d", id)
		}
		seen[id] = true
	}

	// Should have exactly 1000 unique IDs
	if len(seen) != 1000 {
		t.Errorf("Expected 1000 unique IDs, got %d", len(seen))
	}
}

// TestSpatialIndexConsistency tests spatial index during concurrent entity creation/destruction
func TestSpatialIndexConsistency(t *testing.T) {
	world := engine.NewWorld()

	var wg sync.WaitGroup
	errChan := make(chan error, 10)

	// Concurrent entity creation/destruction at same positions
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(x, y int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				// Create entity
				entity := world.CreateEntity()
				world.AddComponent(entity, components.PositionComponent{X: x, Y: y})
				world.UpdateSpatialIndex(entity, x, y)

				// Verify entity is at position
				foundEntity := world.GetEntityAtPosition(x, y)
				if foundEntity == 0 {
					errChan <- &SpatialIndexError{X: x, Y: y, Msg: "Entity not found in spatial index after creation"}
					return
				}

				// Destroy entity
				world.RemoveFromSpatialIndex(x, y)
				world.DestroyEntity(entity)

				// Verify entity is removed from position
				foundEntity = world.GetEntityAtPosition(x, y)
				if foundEntity != 0 {
					errChan <- &SpatialIndexError{X: x, Y: y, Msg: "Entity still in spatial index after destruction"}
					return
				}
			}
		}(i, i)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Errorf("Spatial index inconsistency: %v", err)
	}
}

// Helper types and functions

type ComponentTypeError struct {
	Expected string
	Got      string
}

func (e *ComponentTypeError) Error() string {
	return "Component type mismatch: expected " + e.Expected + ", got " + e.Got
}

type InvalidPositionError struct {
	X, Y int
}

func (e *InvalidPositionError) Error() string {
	return "Invalid position: (" + string(rune(e.X+'0')) + ", " + string(rune(e.Y+'0')) + ")"
}

type InvalidCharacterError struct {
	Rune rune
}

func (e *InvalidCharacterError) Error() string {
	return "Invalid character: rune is zero"
}

type SpatialIndexError struct {
	X, Y int
	Msg  string
}

func (e *SpatialIndexError) Error() string {
	return e.Msg + " at (" + string(rune(e.X+'0')) + ", " + string(rune(e.Y+'0')) + ")"
}

type SlowUpdateSystem struct {
	updateFunc func()
}

func (s *SlowUpdateSystem) Update(world *engine.World, dt time.Duration) {
	if s.updateFunc != nil {
		s.updateFunc()
	}
}

func (s *SlowUpdateSystem) Priority() int {
	return 10
}
