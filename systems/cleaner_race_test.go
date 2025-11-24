package systems

// Race condition tests for cleaner system (snapshots, concurrent access).
// Run with: go test -race ./systems/... -run TestNoRace

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestNoRaceSnapshotRendering verifies no race conditions between updates and snapshot reads
func TestNoRaceSnapshotRendering(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	// Create Red characters across multiple rows
	for row := 0; row < 24; row++ {
		for x := 10; x < 70; x += 10 {
			createRedCharacterAt(world, x, row)
		}
	}

	// Activate cleaners via event
	ctx.PushEvent(engine.EventCleanerRequest, nil)

	var wg sync.WaitGroup
	stopChan := make(chan struct{})

	// Goroutine 1: Update cleaners (main game loop)
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

	// Goroutines 2-51: Read snapshots concurrently (render threads)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stopChan:
					return
				default:
					// Query World directly for cleaner components
					// Using direct store access
					entities := world.Cleaners.All()
					// Verify components are valid
					for _, entity := range entities {
						if comp, ok := world.Cleaners.Get(entity); ok {
							c := comp.(components.CleanerComponent)
							_ = c.GridY
							_ = len(c.Trail)
							_ = c.Char
						}
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
}

// TestNoRaceActivation tests concurrent activation calls
func TestNoRaceActivation(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	// Create Red characters
	for row := 0; row < 10; row++ {
		createRedCharacterAt(world, 40, row)
	}

	var wg sync.WaitGroup
	stopChan := make(chan struct{})

	// Goroutine 1: Update loop
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

	// Goroutines 2-11: Rapidly activate cleaners
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stopChan:
					return
				default:
					ctx.PushEvent(engine.EventCleanerRequest, nil)
					time.Sleep(5 * time.Millisecond)
				}
			}
		}(i)
	}

	// Goroutines 12-21: Read completion status
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stopChan:
					return
				default:
					// Check if cleaners exist (replacement for IsAnimationComplete)
					// Using direct store access
					_ = len(world.Cleaners.All()) == 0
					time.Sleep(2 * time.Millisecond)
				}
			}
		}(i)
	}

	// Run for 1 second
	time.Sleep(1 * time.Second)
	close(stopChan)
	wg.Wait()
}

// TestNoRaceFlashEffects tests concurrent flash effect creation and reading
func TestNoRaceFlashEffects(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	var wg sync.WaitGroup
	stopChan := make(chan struct{})

	// Goroutine 1: Update cleaners
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

	// Goroutine 2: Create Red characters and activate cleaners
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopChan:
				return
			default:
				// Create new Red characters
				for i := 0; i < 3; i++ {
					createRedCharacterAt(world, 10+i*5, i%10)
				}
				ctx.PushEvent(engine.EventCleanerRequest, nil)
				time.Sleep(50 * time.Millisecond)
			}
		}
	}()

	// Goroutines 3-12: Read flash effects
	flashType := reflect.TypeOf(components.RemovalFlashComponent{})
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stopChan:
					return
				default:
					flashes := world.RemovalFlashes.All()
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
}

// TestNoRaceComponentAccess tests concurrent component reads during updates
func TestNoRaceComponentAccess(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	// Create Red characters
	for row := 0; row < 24; row++ {
		createRedCharacterAt(world, 10+row*2, row)
	}

	// Activate cleaners via event
	ctx.PushEvent(engine.EventCleanerRequest, nil)

	var wg sync.WaitGroup
	stopChan := make(chan struct{})

	// Goroutine 1: Update cleaners
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

	// Goroutines 2-21: Read cleaner components
	// Using direct store access
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stopChan:
					return
				default:
					entities := world.Cleaners.All()
					for _, entity := range entities {
						if comp, ok := world.Cleaners.Get(entity); ok {
							c := comp.(components.CleanerComponent)
							_ = c.PreciseX
							_ = c.VelocityX
							_ = len(c.Trail)
						}
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
