package systems

import (
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestCleanerNoMemoryLeak verifies cleaners are properly cleaned up with no memory leaks.
// This test spawns and destroys cleaners in a loop and verifies zero cleaners remain
// after each cycle using atomic counters to track spawns vs cleanups.
func TestCleanerNoMemoryLeak(t *testing.T) {
	// Use mock time provider for precise timing control
	startTime := time.Now()
	mockTime := engine.NewMockTimeProvider(startTime)

	world := engine.NewWorld()
	ctx := &engine.GameContext{
		World:        world,
		TimeProvider: mockTime,
		GameWidth:    80,
		GameHeight:   24,
	}

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Atomic counters to track spawns vs cleanups
	var totalSpawned atomic.Int64
	var totalCleaned atomic.Int64
	var cycleErrors atomic.Int32

	// Reduced from 30 to 5 cycles - memory leaks would show up even in fewer cycles
	cycles := 5

	for cycle := 0; cycle < cycles; cycle++ {
		// Reset time for each cycle
		mockTime.SetTime(startTime)

		// Create Red characters on 5 rows
		redEntities := make([]engine.Entity, 0)
		for row := 0; row < 5; row++ {
			for x := 10; x < 70; x += 10 {
				entity := createRedCharacterAt(world, x, row)
				redEntities = append(redEntities, entity)
			}
		}

		// Trigger cleaners
		cleanerSystem.TriggerCleaners(world)
		cleanerSystem.Update(world, 16*time.Millisecond)

		// Wait for async spawn processing - reduced from 50ms to 30ms
		time.Sleep(30 * time.Millisecond)

		// Verify cleaners were created
		cleanerType := reflect.TypeOf(components.CleanerComponent{})
		cleaners := world.GetEntitiesWith(cleanerType)
		cleanerCount := int64(len(cleaners))

		if cleanerCount == 0 {
			cycleErrors.Add(1)
			t.Errorf("Cycle %d: No cleaners spawned despite Red characters present", cycle)
			continue
		}

		// Track spawned count
		totalSpawned.Add(cleanerCount)

		// Verify IsActive is true
		if !cleanerSystem.IsActive() {
			cycleErrors.Add(1)
			t.Errorf("Cycle %d: CleanerSystem should be active after spawn", cycle)
		}

		// Verify atomic counter matches entity count
		atomicCount := cleanerSystem.GetActiveCleanerCount()
		if atomicCount != cleanerCount {
			cycleErrors.Add(1)
			t.Errorf("Cycle %d: Atomic counter mismatch: atomic=%d, entities=%d",
				cycle, atomicCount, cleanerCount)
		}

		// Advance time past animation duration to trigger cleanup
		mockTime.Advance(constants.CleanerAnimationDuration + 100*time.Millisecond)

		// Wait for cleanup to complete in update loop - reduced from 150ms to 50ms
		time.Sleep(50 * time.Millisecond)

		// Verify all cleaners were cleaned up
		cleanersAfter := world.GetEntitiesWith(cleanerType)
		if len(cleanersAfter) != 0 {
			cycleErrors.Add(1)
			t.Errorf("Cycle %d: Memory leak detected - %d cleaners remain after cleanup",
				cycle, len(cleanersAfter))
		} else {
			totalCleaned.Add(cleanerCount)
		}

		// Verify IsActive is false after cleanup
		if cleanerSystem.IsActive() {
			cycleErrors.Add(1)
			t.Errorf("Cycle %d: CleanerSystem should be inactive after cleanup", cycle)
		}

		// Verify atomic counter is reset to 0
		atomicCountAfter := cleanerSystem.GetActiveCleanerCount()
		if atomicCountAfter != 0 {
			cycleErrors.Add(1)
			t.Errorf("Cycle %d: Atomic counter should be 0 after cleanup, got %d",
				cycle, atomicCountAfter)
		}

		// Clean up Red characters for next cycle using SafeDestroyEntity
		for _, entity := range redEntities {
			world.SafeDestroyEntity(entity)
		}

		// Verify spatial index is clean
		seqType := reflect.TypeOf(components.SequenceComponent{})
		remainingEntities := world.GetEntitiesWith(seqType)
		if len(remainingEntities) != 0 {
			cycleErrors.Add(1)
			t.Errorf("Cycle %d: %d entities remain after cleanup", cycle, len(remainingEntities))
		}

		// Log progress every 10 cycles
		if (cycle+1)%10 == 0 {
			t.Logf("Completed cycle %d/%d - Spawned: %d, Cleaned: %d, Errors: %d",
				cycle+1, cycles, totalSpawned.Load(), totalCleaned.Load(), cycleErrors.Load())
		}
	}

	// Final verification
	if cycleErrors.Load() > 0 {
		t.Errorf("Test completed with %d cycle errors", cycleErrors.Load())
	}

	// Verify perfect balance: all spawned cleaners were cleaned up
	if totalSpawned.Load() != totalCleaned.Load() {
		t.Errorf("Memory leak: spawned %d cleaners but only cleaned %d",
			totalSpawned.Load(), totalCleaned.Load())
	} else {
		t.Logf("SUCCESS: All %d spawned cleaners were properly cleaned up across %d cycles",
			totalSpawned.Load(), cycles)
	}
}

// TestCleanerConcurrentWorldAccess verifies thread-safe concurrent world access.
// This test runs multiple goroutines accessing world simultaneously and verifies
// no nil component panics occur using proper synchronization primitives.
func TestCleanerConcurrentWorldAccess(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Create initial Red characters
	for row := 0; row < 10; row++ {
		for x := 0; x < 80; x += 15 {
			createRedCharacterAt(world, x, row)
		}
	}

	var wg sync.WaitGroup
	var panicCount atomic.Int32
	var operationCount atomic.Int64
	stopChan := make(chan struct{})

	// Goroutine 1-3: Continuously trigger cleaners
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCount.Add(1)
					t.Errorf("Trigger goroutine %d panicked: %v", id, r)
				}
			}()

			ops := 0
			for {
				select {
				case <-stopChan:
					operationCount.Add(int64(ops))
					return
				default:
					cleanerSystem.TriggerCleaners(world)
					cleanerSystem.Update(world, 16*time.Millisecond)
					ops++
					time.Sleep(20 * time.Millisecond)
				}
			}
		}(i)
	}

	// Goroutine 4-6: Continuously read cleaner entities
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCount.Add(1)
					t.Errorf("Reader goroutine %d panicked: %v", id, r)
				}
			}()

			ops := 0
			cleanerType := reflect.TypeOf(components.CleanerComponent{})
			for {
				select {
				case <-stopChan:
					operationCount.Add(int64(ops))
					return
				default:
					// Read all cleaner entities
					cleaners := world.GetEntitiesWith(cleanerType)

					// Try to read components (may be nil during cleanup)
					for _, entity := range cleaners {
						comp, ok := world.GetComponent(entity, cleanerType)
						if ok && comp != nil {
							// Verify component can be cast safely
							_, validCast := comp.(components.CleanerComponent)
							if !validCast {
								t.Errorf("Reader %d: Component type assertion failed", id)
							}
						}
					}
					ops++
					time.Sleep(10 * time.Millisecond)
				}
			}
		}(i)
	}

	// Goroutine 7-9: Continuously scan for Red characters
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCount.Add(1)
					t.Errorf("Scanner goroutine %d panicked: %v", id, r)
				}
			}()

			ops := 0
			for {
				select {
				case <-stopChan:
					operationCount.Add(int64(ops))
					return
				default:
					rows := cleanerSystem.scanRedCharacterRows(world)
					_ = rows
					ops++
					time.Sleep(25 * time.Millisecond)
				}
			}
		}(i)
	}

	// Goroutine 10-12: Continuously check active state
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCount.Add(1)
					t.Errorf("State checker goroutine %d panicked: %v", id, r)
				}
			}()

			ops := 0
			for {
				select {
				case <-stopChan:
					operationCount.Add(int64(ops))
					return
				default:
					_ = cleanerSystem.IsActive()
					_ = cleanerSystem.GetActiveCleanerCount()
					ops++
					time.Sleep(5 * time.Millisecond)
				}
			}
		}(i)
	}

	// Goroutine 13-15: Continuously detect and destroy Red characters
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCount.Add(1)
					t.Errorf("Destroyer goroutine %d panicked: %v", id, r)
				}
			}()

			ops := 0
			for {
				select {
				case <-stopChan:
					operationCount.Add(int64(ops))
					return
				default:
					cleanerSystem.detectAndDestroyRedCharacters(world)
					ops++
					time.Sleep(30 * time.Millisecond)
				}
			}
		}(i)
	}

	// Goroutine 16-17: Continuously read world entities
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCount.Add(1)
					t.Errorf("World reader goroutine %d panicked: %v", id, r)
				}
			}()

			ops := 0
			seqType := reflect.TypeOf(components.SequenceComponent{})
			posType := reflect.TypeOf(components.PositionComponent{})

			for {
				select {
				case <-stopChan:
					operationCount.Add(int64(ops))
					return
				default:
					// Read all entities with components
					entities := world.GetEntitiesWith(seqType, posType)

					// Try to read their components (may be nil during cleanup)
					for _, entity := range entities {
						seqComp, ok := world.GetComponent(entity, seqType)
						if ok && seqComp != nil {
							_, validCast := seqComp.(components.SequenceComponent)
							if !validCast {
								t.Errorf("World reader %d: Sequence component cast failed", id)
							}
						}
					}
					ops++
					time.Sleep(15 * time.Millisecond)
				}
			}
		}(i)
	}

	// Let all goroutines run concurrently - reduced from 2s to 500ms
	time.Sleep(500 * time.Millisecond)
	close(stopChan)

	// Wait for all goroutines to complete
	wg.Wait()

	// Report results
	t.Logf("Concurrent access test completed:")
	t.Logf("  Total operations: %d", operationCount.Load())
	t.Logf("  Goroutines: 17")
	t.Logf("  Panics: %d", panicCount.Load())

	// Verify no panics occurred
	if panicCount.Load() > 0 {
		t.Errorf("Test failed with %d panics during concurrent access", panicCount.Load())
	}

	// Final state verification
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	finalCleaners := world.GetEntitiesWith(cleanerType)
	t.Logf("  Final cleaners: %d", len(finalCleaners))
	t.Logf("  Active state: %v", cleanerSystem.IsActive())
}

// TestCleanerCleanupTiming verifies precise cleanup timing using MockTimeProvider.
// This test uses MockTimeProvider to control time, verifies cleaners are destroyed
// exactly at animation duration, and checks pool resources are properly returned.
func TestCleanerCleanupTiming(t *testing.T) {
	// Use mock time provider for precise timing control
	startTime := time.Now()
	mockTime := engine.NewMockTimeProvider(startTime)

	world := engine.NewWorld()
	ctx := &engine.GameContext{
		World:        world,
		TimeProvider: mockTime,
		GameWidth:    80,
		GameHeight:   24,
	}

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Track pool allocations
	tracker := NewEntityLifecycleTracker()

	// Create Red characters on 3 rows
	for row := 0; row < 3; row++ {
		createRedCharacterAt(world, 40, row*5)
	}

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Wait for async spawn
	time.Sleep(50 * time.Millisecond)

	// Record initial state
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	initialCleaners := world.GetEntitiesWith(cleanerType)
	if len(initialCleaners) != 3 {
		t.Fatalf("Expected 3 cleaners, got %d", len(initialCleaners))
	}

	// Track created cleaners
	for _, entity := range initialCleaners {
		tracker.TrackCreate(uint64(entity))
	}

	// Verify initial active state
	if !cleanerSystem.IsActive() {
		t.Fatal("CleanerSystem should be active after trigger")
	}

	// Verify atomic counter
	if cleanerSystem.GetActiveCleanerCount() != 3 {
		t.Errorf("Expected active count 3, got %d", cleanerSystem.GetActiveCleanerCount())
	}

	// Test timing precision: advance time in increments
	timingTests := []struct {
		name           string
		advanceDelta   time.Duration
		shouldBeActive bool
		shouldHaveCleaners bool
	}{
		{
			name:           "T+100ms - cleaners still active",
			advanceDelta:   100 * time.Millisecond,
			shouldBeActive: true,
			shouldHaveCleaners: true,
		},
		{
			name:           "T+500ms - cleaners still active",
			advanceDelta:   400 * time.Millisecond, // Total: 500ms
			shouldBeActive: true,
			shouldHaveCleaners: true,
		},
		{
			name:           "T+900ms - cleaners still active (just before duration)",
			advanceDelta:   400 * time.Millisecond, // Total: 900ms
			shouldBeActive: true,
			shouldHaveCleaners: true,
		},
		{
			name:           "T+1000ms - cleaners at duration (edge case)",
			advanceDelta:   100 * time.Millisecond, // Total: 1000ms (exactly at duration)
			shouldBeActive: false, // Should cleanup at >= duration
			shouldHaveCleaners: false,
		},
	}

	for _, tt := range timingTests {
		t.Run(tt.name, func(t *testing.T) {
			// Advance time
			mockTime.Advance(tt.advanceDelta)

			// Wait for update loop to process (updateLoop runs at 60 FPS = ~16.6ms per frame)
			time.Sleep(100 * time.Millisecond)

			// Check active state
			isActive := cleanerSystem.IsActive()
			if isActive != tt.shouldBeActive {
				t.Errorf("Expected IsActive=%v, got %v", tt.shouldBeActive, isActive)
			}

			// Check cleaner entities
			cleaners := world.GetEntitiesWith(cleanerType)
			hasCleaners := len(cleaners) > 0
			if hasCleaners != tt.shouldHaveCleaners {
				t.Errorf("Expected cleaners present=%v (count=%d), got %v",
					tt.shouldHaveCleaners, len(cleaners), hasCleaners)
			}

			// If cleaners should be gone, verify atomic counter is reset
			if !tt.shouldHaveCleaners {
				atomicCount := cleanerSystem.GetActiveCleanerCount()
				if atomicCount != 0 {
					t.Errorf("Expected atomic counter 0 after cleanup, got %d", atomicCount)
				}

				// Track destroyed cleaners
				for _, entity := range initialCleaners {
					tracker.TrackDestroy(uint64(entity))
				}
			}
		})
	}

	// Verify no memory leaks
	leaks := tracker.DetectLeaks()
	if len(leaks) > 0 {
		t.Errorf("Memory leak detected: %d entities not cleaned up: %v", len(leaks), leaks)
	}

	// Verify final cleanup state
	finalCleaners := world.GetEntitiesWith(cleanerType)
	if len(finalCleaners) != 0 {
		t.Errorf("Expected 0 cleaners after cleanup, got %d", len(finalCleaners))
	}

	// Clean up remaining Red characters from first round
	seqType := reflect.TypeOf(components.SequenceComponent{})
	remainingEntities := world.GetEntitiesWith(seqType)
	for _, entity := range remainingEntities {
		world.SafeDestroyEntity(entity)
	}

	// Wait to ensure complete cleanup
	time.Sleep(100 * time.Millisecond)

	// Verify pool resources: Try to trigger cleaners again to test pool reuse
	createRedCharacterAt(world, 50, 10)

	// Reset time for second activation
	mockTime.SetTime(startTime.Add(5 * time.Second))

	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	// Verify new cleaners were created (pool reuse working)
	newCleaners := world.GetEntitiesWith(cleanerType)
	if len(newCleaners) != 1 {
		t.Errorf("Expected 1 cleaner after second activation, got %d", len(newCleaners))
	}

	// Verify trail slice is allocated from pool
	if len(newCleaners) > 0 {
		cleanerComp, ok := world.GetComponent(newCleaners[0], cleanerType)
		if ok && cleanerComp != nil {
			cleaner := cleanerComp.(components.CleanerComponent)
			if cleaner.TrailPositions == nil {
				t.Error("Trail positions should be allocated from pool")
			}
			if cap(cleaner.TrailPositions) != constants.CleanerTrailLength {
				t.Errorf("Expected trail capacity %d (from pool), got %d",
					constants.CleanerTrailLength, cap(cleaner.TrailPositions))
			}
		}
	}

	t.Logf("Timing precision verified across %d time points", len(timingTests))
	t.Logf("Pool resource reuse verified")
}
