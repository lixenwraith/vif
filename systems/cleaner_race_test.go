package systems

// Race condition tests for cleaner system (snapshots, state access, flash effects, pool allocation).
// See also:
//   - race_condition_comprehensive_test.go: Spawn system and content management race conditions
//   - boost_race_test.go: Boost/heat system race conditions

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

// TestNoRaceCleanerConcurrentRenderUpdate verifies no race conditions between
// cleaner updates and snapshot rendering. This tests the frame-coherent snapshot mechanism.
func TestNoRaceCleanerConcurrentRenderUpdate(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Create Red characters across multiple rows
	for row := 0; row < 24; row++ {
		for x := 10; x < 70; x += 10 {
			createRedCharacterAt(world, x, row)
		}
	}

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	var wg sync.WaitGroup
	stopChan := make(chan struct{})
	errChan := make(chan error, 100)

	// Single goroutine updating cleaners (simulates main game loop)
	// This is the correct usage pattern - only ONE thread calls Update()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopChan:
				return
			default:
				cleanerSystem.Update(world, 16*time.Millisecond)
				time.Sleep(16 * time.Millisecond) // Simulate 60 FPS
			}
		}
	}()

	// 50 goroutines rendering (reading snapshots)
	// This simulates multiple render threads reading state
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stopChan:
					return
				default:
					snapshots := cleanerSystem.GetCleanerSnapshots()
					// Verify snapshots are valid
					for _, snapshot := range snapshots {
						if snapshot.Row < 0 || snapshot.Row >= 24 {
							errChan <- nil // Signal error without actual error object
							return
						}
						// Access trail positions to ensure deep copy worked
						_ = len(snapshot.TrailPositions)
					}
					time.Sleep(1 * time.Millisecond)
				}
			}
		}(i)
	}

	// Run for 1 second
	time.Sleep(1 * time.Second)
	close(stopChan)
	wg.Wait()

	// Check for errors
	select {
	case <-errChan:
		t.Fatal("Race condition detected: invalid snapshot data")
	default:
		// Success
	}
}

// TestNoRaceRapidCleanerCycles tests rapid activation/deactivation of cleaners
// to verify no race conditions in state transitions.
func TestNoRaceRapidCleanerCycles(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Create Red characters
	for row := 0; row < 10; row++ {
		createRedCharacterAt(world, 40, row)
	}

	var cycleCount atomic.Int32
	var wg sync.WaitGroup
	stopChan := make(chan struct{})

	// Goroutine 1: Rapidly trigger cleaners (from external thread)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopChan:
				return
			default:
				cleanerSystem.TriggerCleaners(world)
				cycleCount.Add(1)
				time.Sleep(5 * time.Millisecond)
			}
		}
	}()

	// Goroutine 2: Process updates (single thread, like main game loop)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopChan:
				return
			default:
				cleanerSystem.Update(world, 16*time.Millisecond)
				time.Sleep(16 * time.Millisecond) // 60 FPS
			}
		}
	}()

	// Goroutine 3-10: Check state from multiple readers
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stopChan:
					return
				default:
					_ = cleanerSystem.IsActive()
					_ = cleanerSystem.GetActiveCleanerCount()
					_ = cleanerSystem.GetSystemState()
					_ = cleanerSystem.GetCleanerSnapshots()
					time.Sleep(2 * time.Millisecond)
				}
			}
		}(i)
	}

	// Run for 1 second
	time.Sleep(1 * time.Second)
	close(stopChan)
	wg.Wait()

	cycles := cycleCount.Load()
	if cycles < 50 {
		t.Logf("Warning: Only completed %d cycles, expected at least 50", cycles)
	} else {
		t.Logf("Successfully completed %d rapid cycles without races", cycles)
	}
}

// TestNoRaceCleanerStateAccess verifies concurrent access to cleaner state
// is properly synchronized.
func TestNoRaceCleanerStateAccess(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Create Red characters
	for row := 0; row < 24; row++ {
		createRedCharacterAt(world, 10+row*2, row)
	}

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	var wg sync.WaitGroup
	stopChan := make(chan struct{})

	// Update loop running in single thread (main game loop)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopChan:
				return
			default:
				cleanerSystem.Update(world, 16*time.Millisecond)
				time.Sleep(16 * time.Millisecond) // 60 FPS
			}
		}
	}()

	// Multiple goroutines reading state concurrently (render threads, debug threads, etc.)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stopChan:
					return
				default:
					// Read various state atomically
					active := cleanerSystem.IsActive()
					count := cleanerSystem.GetActiveCleanerCount()
					state := cleanerSystem.GetSystemState()

					// Verify consistency
					if active && count == 0 {
						t.Errorf("Inconsistent state: active=true but count=0")
					}
					if !active && count > 0 {
						t.Errorf("Inconsistent state: active=false but count=%d", count)
					}

					// State string should contain expected information
					if active && len(state) == 0 {
						t.Error("Active system should have non-empty state string")
					}

					time.Sleep(1 * time.Millisecond)
				}
			}
		}(i)
	}

	// Run for 500ms
	time.Sleep(500 * time.Millisecond)
	close(stopChan)
	wg.Wait()
}

// TestNoRaceFlashEffectManagement tests concurrent flash effect creation and cleanup
func TestNoRaceFlashEffectManagement(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	var wg sync.WaitGroup
	stopChan := make(chan struct{})

	// Goroutine 1: Trigger cleaners repeatedly (external thread)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopChan:
				return
			default:
				// Create new Red characters
				for i := 0; i < 5; i++ {
					createRedCharacterAt(world, 10+i*5, i%10)
				}

				// Trigger cleaners (will create flash effects when destroying Red chars)
				cleanerSystem.TriggerCleaners(world)
				time.Sleep(50 * time.Millisecond)
			}
		}
	}()

	// Goroutine 2: Update cleaners (single thread, main game loop)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopChan:
				return
			default:
				cleanerSystem.Update(world, 16*time.Millisecond)
				time.Sleep(16 * time.Millisecond) // 60 FPS
			}
		}
	}()

	// Goroutines 3-12: Check flash effects from multiple readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			flashType := reflect.TypeOf(components.RemovalFlashComponent{})
			for {
				select {
				case <-stopChan:
					return
				default:
					flashes := world.GetEntitiesWith(flashType)
					// Just reading flash count - shouldn't race
					_ = len(flashes)
					time.Sleep(10 * time.Millisecond)
				}
			}
		}(i)
	}

	// Run for 500ms
	time.Sleep(500 * time.Millisecond)
	close(stopChan)
	wg.Wait()

	// Verify cleanup worked
	flashType := reflect.TypeOf(components.RemovalFlashComponent{})
	flashes := world.GetEntitiesWith(flashType)
	t.Logf("Flash effects remaining at end: %d", len(flashes))
}

// TestNoRaceCleanerPoolAllocation tests the sync.Pool usage for trail slices
func TestNoRaceCleanerPoolAllocation(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Create Red characters
	for row := 0; row < 10; row++ {
		createRedCharacterAt(world, 40, row)
	}

	var wg sync.WaitGroup
	stopChan := make(chan struct{})
	var spawnCount atomic.Int32

	// Update loop (single thread, main game loop)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopChan:
				return
			default:
				cleanerSystem.Update(world, 16*time.Millisecond)
				time.Sleep(16 * time.Millisecond) // 60 FPS
			}
		}
	}()

	// Multiple goroutines triggering cleaners and forcing cleanup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stopChan:
					return
				default:
					cleanerSystem.TriggerCleaners(world)
					spawnCount.Add(1)
					time.Sleep(50 * time.Millisecond)
				}
			}
		}(i)
	}

	// Run for 1 second
	time.Sleep(1 * time.Second)
	close(stopChan)
	wg.Wait()

	t.Logf("Completed %d spawn/cleanup cycles without memory corruption", spawnCount.Load())
}

// TestNoRaceDimensionUpdate tests concurrent dimension updates
func TestNoRaceDimensionUpdate(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	var wg sync.WaitGroup
	stopChan := make(chan struct{})

	// Goroutine 1: Update dimensions
	wg.Add(1)
	go func() {
		defer wg.Done()
		dimensions := [][2]int{{80, 24}, {100, 30}, {120, 40}, {80, 24}}
		idx := 0
		for {
			select {
			case <-stopChan:
				return
			default:
				dim := dimensions[idx%len(dimensions)]
				cleanerSystem.UpdateDimensions(dim[0], dim[1])
				idx++
				time.Sleep(20 * time.Millisecond)
			}
		}
	}()

	// Goroutine 2: Trigger cleaners
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopChan:
				return
			default:
				createRedCharacterAt(world, 10, 5)
				cleanerSystem.TriggerCleaners(world)
				cleanerSystem.Update(world, 16*time.Millisecond)
				time.Sleep(30 * time.Millisecond)
			}
		}
	}()

	// Goroutine 3: Read snapshots
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopChan:
				return
			default:
				_ = cleanerSystem.GetCleanerSnapshots()
				time.Sleep(5 * time.Millisecond)
			}
		}
	}()

	// Run for 500ms
	time.Sleep(500 * time.Millisecond)
	close(stopChan)
	wg.Wait()
}

// TestNoRaceCleanerAnimationCompletion tests concurrent checks for animation completion
func TestNoRaceCleanerAnimationCompletion(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	// Use shorter animation duration for faster testing
	config := constants.DefaultCleanerConfig()
	config.AnimationDuration = 200 * time.Millisecond
	cleanerSystem := NewCleanerSystem(ctx, 80, 24, config)
	defer cleanerSystem.Shutdown()

	// Create Red characters
	for row := 0; row < 5; row++ {
		createRedCharacterAt(world, 40, row)
	}

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	var wg sync.WaitGroup
	stopChan := make(chan struct{})

	// Multiple goroutines checking IsAnimationComplete
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stopChan:
					return
				default:
					_ = cleanerSystem.IsAnimationComplete()
					time.Sleep(1 * time.Millisecond)
				}
			}
		}(i)
	}

	// Update loop
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopChan:
				return
			default:
				cleanerSystem.Update(world, 16*time.Millisecond)
				time.Sleep(16 * time.Millisecond)
			}
		}
	}()

	// Run until animation should be complete
	time.Sleep(300 * time.Millisecond)
	close(stopChan)
	wg.Wait()

	// Verify animation completed
	if !cleanerSystem.IsAnimationComplete() {
		t.Error("Animation should be complete after duration elapsed")
	}
	if cleanerSystem.IsActive() {
		t.Error("Cleaners should be inactive after animation complete")
	}
}
