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

// TestRapidGoldActivation performs stress testing with rapid gold sequence activation
func TestRapidGoldActivation(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	decaySystem := NewDecaySystem(80, 24, 80, 0, ctx)
	goldSystem := NewGoldSequenceSystem(ctx, decaySystem, 80, 24, 0, 0)
	goldSystem.SetCleanerTrigger(cleanerSystem.TriggerCleaners)

	var wg sync.WaitGroup
	var errorCount atomic.Int32
	var activationCount atomic.Int32

	// Goroutine 1-5: Rapidly try to spawn gold sequences
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errorCount.Add(1)
					t.Errorf("Gold spawn goroutine %d panicked: %v", id, r)
				}
			}()

			for j := 0; j < 20; j++ {
				goldSystem.spawnGoldSequence(world)
				activationCount.Add(1)
				time.Sleep(25 * time.Millisecond)
			}
		}(i)
	}

	// Goroutine 6-8: Rapidly try to complete gold sequences
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errorCount.Add(1)
					t.Errorf("Gold completion goroutine %d panicked: %v", id, r)
				}
			}()

			for j := 0; j < 15; j++ {
				goldSystem.CompleteGoldSequence(world)
				time.Sleep(35 * time.Millisecond)
			}
		}(i)
	}

	// Goroutine 9: Update gold system
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("Gold update goroutine panicked: %v", r)
			}
		}()

		for i := 0; i < 100; i++ {
			goldSystem.Update(world, 16*time.Millisecond)
			time.Sleep(10 * time.Millisecond)
		}
	}()

	wg.Wait()

	// Verify no errors
	if errorCount.Load() > 0 {
		t.Errorf("Test completed with %d errors/panics", errorCount.Load())
	}

	t.Logf("Completed rapid gold activation test: %d activation attempts", activationCount.Load())
}

// TestMaximumCleanersOnScreen tests spawning maximum number of cleaners
func TestMaximumCleanersOnScreen(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Create Red characters on every row
	for row := 0; row < 24; row++ {
		for x := 10; x < 70; x += 10 {
			createRedCharacterAt(world, x, row)
		}
	}

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Wait for async processing
	time.Sleep(100 * time.Millisecond)

	// Verify cleaners created
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	if len(cleaners) != 24 {
		t.Fatalf("Expected 24 cleaners (one per row), got %d", len(cleaners))
	}

	var wg sync.WaitGroup
	var errorCount atomic.Int32
	stopChan := make(chan struct{})

	// Launch multiple goroutines to stress test with max cleaners
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errorCount.Add(1)
					t.Errorf("Stress goroutine %d panicked: %v", id, r)
				}
			}()

			for j := 0; j < 50; j++ {
				select {
				case <-stopChan:
					return
				default:
					// Various operations
					if id%3 == 0 {
						// Read cleaners
						_ = world.GetEntitiesWith(cleanerType)
					} else if id%3 == 1 {
						// Update cleaners
						cleanerSystem.Update(world, 16*time.Millisecond)
					} else {
						// Detect collisions
						cleanerSystem.detectAndDestroyRedCharacters(world)
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
		}(i)
	}

	// Let test run
	time.Sleep(600 * time.Millisecond)
	close(stopChan)

	wg.Wait()

	// Verify no errors
	if errorCount.Load() > 0 {
		t.Errorf("Test completed with %d errors/panics", errorCount.Load())
	}

	t.Logf("Successfully stressed system with %d concurrent cleaners", len(cleaners))
}

// TestMemoryLeakDetection checks for memory leaks in cleaner system
func TestMemoryLeakDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory leak test in short mode")
	}

	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Run multiple cycles of cleaner creation and cleanup
	cycles := 50
	for cycle := 0; cycle < cycles; cycle++ {
		// Create Red characters
		for i := 0; i < 10; i++ {
			createRedCharacterAt(world, 10+i*5, cycle%24)
		}

		// Trigger cleaners
		cleanerSystem.TriggerCleaners(world)
		cleanerSystem.Update(world, 16*time.Millisecond)
		time.Sleep(20 * time.Millisecond)

		// Wait for cleanup
		time.Sleep(1100 * time.Millisecond)

		// Verify cleaners were cleaned up
		cleanerType := reflect.TypeOf(components.CleanerComponent{})
		cleaners := world.GetEntitiesWith(cleanerType)
		if len(cleaners) > 0 {
			t.Logf("Cycle %d: %d cleaners not cleaned up", cycle, len(cleaners))
		}

		// Clean up Red characters for next cycle
		seqType := reflect.TypeOf(components.SequenceComponent{})
		posType := reflect.TypeOf(components.PositionComponent{})
		entities := world.GetEntitiesWith(seqType, posType)

		for _, entity := range entities {
			posComp, ok := world.GetComponent(entity, posType)
			if ok {
				pos := posComp.(components.PositionComponent)
				world.RemoveFromSpatialIndex(pos.X, pos.Y)
			}
			world.DestroyEntity(entity)
		}

		if cycle%10 == 0 {
			t.Logf("Completed cycle %d/%d", cycle, cycles)
		}
	}

	// Final verification: no cleaners should remain
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	finalCleaners := world.GetEntitiesWith(cleanerType)
	if len(finalCleaners) > 0 {
		t.Errorf("Memory leak detected: %d cleaners remain after all cycles", len(finalCleaners))
	}

	t.Logf("Completed %d cycles without memory leaks", cycles)
}

// TestCleanerSystemUnderExtremeConcurrency performs extreme concurrency stress test
func TestCleanerSystemUnderExtremeConcurrency(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	spawnSys := NewSpawnSystem(80, 24, 40, 12, ctx)
	decaySys := NewDecaySystem(80, 24, 80, 0, ctx)
	decaySys.SetSpawnSystem(spawnSys)

	goldSystem := NewGoldSequenceSystem(ctx, decaySys, 80, 24, 0, 0)
	goldSystem.SetCleanerTrigger(cleanerSystem.TriggerCleaners)

	// Create initial entities
	for row := 0; row < 24; row++ {
		for x := 0; x < 80; x += 8 {
			if row%3 == 0 {
				createRedCharacterAt(world, x, row)
			} else if row%3 == 1 {
				entity := createGreenCharacterAt(world, x, row)
				spawnSys.AddColorCount(components.SequenceGreen, components.LevelBright, 1)
				_ = entity
			} else {
				createBlueCharacterAt(world, x, row)
			}
		}
	}

	var wg sync.WaitGroup
	var errorCount atomic.Int32
	var totalOps atomic.Int64
	stopChan := make(chan struct{})

	// Launch 20 concurrent goroutines performing various operations
	operationTypes := []string{
		"cleaner_trigger", "cleaner_update", "cleaner_detect",
		"gold_spawn", "gold_complete", "gold_update",
		"decay_apply", "entity_create", "entity_destroy",
		"world_read", "spatial_read", "component_read",
		"counter_read", "counter_write", "cleaner_scan",
		"gold_trigger", "state_check", "dimension_update",
		"pool_stress", "concurrent_access",
	}

	for i, opType := range operationTypes {
		wg.Add(1)
		go func(id int, operation string) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errorCount.Add(1)
					t.Errorf("Goroutine %d (%s) panicked: %v", id, operation, r)
				}
			}()

			localOps := 0
			for {
				select {
				case <-stopChan:
					totalOps.Add(int64(localOps))
					return
				default:
					// Perform operation based on type
					switch operation {
					case "cleaner_trigger":
						cleanerSystem.TriggerCleaners(world)
					case "cleaner_update":
						cleanerSystem.Update(world, 16*time.Millisecond)
					case "cleaner_detect":
						cleanerSystem.detectAndDestroyRedCharacters(world)
					case "gold_spawn":
						goldSystem.spawnGoldSequence(world)
					case "gold_complete":
						goldSystem.CompleteGoldSequence(world)
					case "gold_update":
						goldSystem.Update(world, 16*time.Millisecond)
					case "decay_apply":
						row := localOps % 24
						decaySys.applyDecayToRow(world, row)
					case "entity_create":
						x := (localOps * 7) % 80
						y := (localOps * 3) % 24
						createRedCharacterAt(world, x, y)
					case "entity_destroy":
						seqType := reflect.TypeOf(components.SequenceComponent{})
						posType := reflect.TypeOf(components.PositionComponent{})
						entities := world.GetEntitiesWith(seqType, posType)
						if len(entities) > 10 {
							entity := entities[localOps%len(entities)]
							posComp, ok := world.GetComponent(entity, posType)
							if ok {
								pos := posComp.(components.PositionComponent)
								world.RemoveFromSpatialIndex(pos.X, pos.Y)
							}
							world.DestroyEntity(entity)
						}
					case "world_read":
						_ = world.GetEntitiesWith()
					case "spatial_read":
						x := (localOps * 13) % 80
						y := (localOps * 7) % 24
						_ = world.GetEntityAtPosition(x, y)
					case "component_read":
						seqType := reflect.TypeOf(components.SequenceComponent{})
						entities := world.GetEntitiesWith(seqType)
						for _, entity := range entities {
							_, _ = world.GetComponent(entity, seqType)
							if localOps%10 == 0 {
								break
							}
						}
					case "counter_read":
						_ = spawnSys.GetColorCount(components.SequenceBlue, components.LevelBright)
						_ = spawnSys.GetColorCount(components.SequenceGreen, components.LevelNormal)
					case "counter_write":
						spawnSys.AddColorCount(components.SequenceBlue, components.LevelBright, 1)
						spawnSys.AddColorCount(components.SequenceBlue, components.LevelBright, -1)
					case "cleaner_scan":
						_ = cleanerSystem.scanRedCharacterRows(world)
					case "gold_trigger":
						goldSystem.TriggerCleanersIfHeatFull(world, 100, 100)
					case "state_check":
						_ = cleanerSystem.IsActive()
						_ = goldSystem.IsActive()
					case "dimension_update":
						cleanerSystem.UpdateDimensions(80, 24)
						goldSystem.UpdateDimensions(80, 24, 0, 0)
					case "pool_stress":
						// Stress the pool by getting cleaners
						cleanerType := reflect.TypeOf(components.CleanerComponent{})
						cleaners := world.GetEntitiesWith(cleanerType)
						for _, entity := range cleaners {
							_, _ = world.GetComponent(entity, cleanerType)
						}
					case "concurrent_access":
						// Mix of operations
						_ = cleanerSystem.IsActive()
						cleanerType := reflect.TypeOf(components.CleanerComponent{})
						_ = world.GetEntitiesWith(cleanerType)
					}

					localOps++
					time.Sleep(5 * time.Millisecond)
				}
			}
		}(i, opType)
	}

	// Let stress test run
	time.Sleep(2 * time.Second)
	close(stopChan)

	wg.Wait()

	t.Logf("Completed extreme concurrency test:")
	t.Logf("  Total operations: %d", totalOps.Load())
	t.Logf("  Error count: %d", errorCount.Load())
	t.Logf("  Goroutines: %d", len(operationTypes))

	// Verify no errors
	if errorCount.Load() > 0 {
		t.Errorf("Test completed with %d errors/panics", errorCount.Load())
	}

	// Verify system is in consistent state
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	finalCleaners := world.GetEntitiesWith(cleanerType)
	t.Logf("  Final cleaners: %d", len(finalCleaners))

	seqType := reflect.TypeOf(components.SequenceComponent{})
	finalEntities := world.GetEntitiesWith(seqType)
	t.Logf("  Final entities: %d", len(finalEntities))
}

// TestAtomicOperationConsistency verifies atomic operations maintain consistency
func TestAtomicOperationConsistency(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Create Red characters
	for i := 0; i < 5; i++ {
		createRedCharacterAt(world, 10+i*10, 5)
	}

	var wg sync.WaitGroup
	var activateCount atomic.Int32
	var readCount atomic.Int32

	// Goroutine 1-5: Try to activate
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				cleanerSystem.TriggerCleaners(world)
				cleanerSystem.Update(world, 16*time.Millisecond)
				activateCount.Add(1)
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}

	// Goroutine 6-10: Check activation state
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				active := cleanerSystem.IsActive()
				readCount.Add(1)
				_ = active
				time.Sleep(2 * time.Millisecond)
			}
		}()
	}

	wg.Wait()

	t.Logf("Atomic operation test completed:")
	t.Logf("  Activate attempts: %d", activateCount.Load())
	t.Logf("  State reads: %d", readCount.Load())
}

// TestFlashEffectStressTest verifies flash effect creation and cleanup under stress
func TestFlashEffectStressTest(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Create many Red characters
	for row := 0; row < 24; row++ {
		for x := 0; x < 80; x += 2 {
			createRedCharacterAt(world, x, row)
		}
	}

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	var wg sync.WaitGroup
	var errorCount atomic.Int32
	var flashCreatedCount atomic.Int32
	stopChan := make(chan struct{})

	// Goroutine 1: Rapidly trigger collisions (creating flashes)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("Collision goroutine panicked: %v", r)
			}
		}()

		for i := 0; i < 100; i++ {
			select {
			case <-stopChan:
				return
			default:
				cleanerSystem.detectAndDestroyRedCharacters(world)

				// Count flashes
				flashType := reflect.TypeOf(components.RemovalFlashComponent{})
				flashes := world.GetEntitiesWith(flashType)
				flashCreatedCount.Store(int32(len(flashes)))

				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	// Goroutine 2: Continuously clean up flashes
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("Cleanup goroutine panicked: %v", r)
			}
		}()

		for i := 0; i < 100; i++ {
			select {
			case <-stopChan:
				return
			default:
				cleanerSystem.cleanupExpiredFlashes(world)
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	// Goroutine 3: Read flash entities
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("Flash reader panicked: %v", r)
			}
		}()

		for i := 0; i < 150; i++ {
			select {
			case <-stopChan:
				return
			default:
				flashType := reflect.TypeOf(components.RemovalFlashComponent{})
				flashes := world.GetEntitiesWith(flashType)

				for _, entity := range flashes {
					_, _ = world.GetComponent(entity, flashType)
				}

				time.Sleep(7 * time.Millisecond)
			}
		}
	}()

	// Let test run
	time.Sleep(1200 * time.Millisecond)
	close(stopChan)

	wg.Wait()

	// Verify no errors
	if errorCount.Load() > 0 {
		t.Errorf("Test completed with %d errors/panics", errorCount.Load())
	}

	t.Logf("Flash stress test completed, max flashes created: %d", flashCreatedCount.Load())
}

// TestSpatialIndexRaceConditions tests spatial index under heavy concurrent access
func TestSpatialIndexRaceConditions(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	var wg sync.WaitGroup
	var errorCount atomic.Int32
	var opsCount atomic.Int64
	stopChan := make(chan struct{})

	// Goroutine 1-5: Continuously create entities at random positions
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errorCount.Add(1)
					t.Errorf("Creator %d panicked: %v", id, r)
				}
			}()

			ops := 0
			for {
				select {
				case <-stopChan:
					opsCount.Add(int64(ops))
					return
				default:
					x := (ops*7 + id*13) % 80
					y := (ops*3 + id*5) % 24

					// Check if position is free
					if world.GetEntityAtPosition(x, y) == 0 {
						createRedCharacterAt(world, x, y)
					}

					ops++
					time.Sleep(3 * time.Millisecond)
				}
			}
		}(i)
	}

	// Goroutine 6-10: Continuously read spatial index
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errorCount.Add(1)
					t.Errorf("Reader %d panicked: %v", id, r)
				}
			}()

			ops := 0
			for {
				select {
				case <-stopChan:
					opsCount.Add(int64(ops))
					return
				default:
					for y := 0; y < 24; y++ {
						for x := 0; x < 80; x += 10 {
							_ = world.GetEntityAtPosition(x, y)
						}
					}
					ops++
					time.Sleep(5 * time.Millisecond)
				}
			}
		}(i)
	}

	// Goroutine 11-13: Trigger cleaners and scan
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errorCount.Add(1)
					t.Errorf("Cleaner %d panicked: %v", id, r)
				}
			}()

			ops := 0
			for {
				select {
				case <-stopChan:
					opsCount.Add(int64(ops))
					return
				default:
					_ = cleanerSystem.scanRedCharacterRows(world)
					cleanerSystem.TriggerCleaners(world)
					cleanerSystem.Update(world, 16*time.Millisecond)
					ops++
					time.Sleep(20 * time.Millisecond)
				}
			}
		}(i)
	}

	// Goroutine 14-16: Destroy entities
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errorCount.Add(1)
					t.Errorf("Destroyer %d panicked: %v", id, r)
				}
			}()

			ops := 0
			for {
				select {
				case <-stopChan:
					opsCount.Add(int64(ops))
					return
				default:
					seqType := reflect.TypeOf(components.SequenceComponent{})
					posType := reflect.TypeOf(components.PositionComponent{})
					entities := world.GetEntitiesWith(seqType, posType)

					if len(entities) > 50 {
						for j := 0; j < 5 && j < len(entities); j++ {
							entity := entities[j]
							posComp, ok := world.GetComponent(entity, posType)
							if ok {
								pos := posComp.(components.PositionComponent)
								world.RemoveFromSpatialIndex(pos.X, pos.Y)
							}
							world.DestroyEntity(entity)
						}
					}

					ops++
					time.Sleep(15 * time.Millisecond)
				}
			}
		}(i)
	}

	// Let test run
	time.Sleep(1500 * time.Millisecond)
	close(stopChan)

	wg.Wait()

	t.Logf("Spatial index race test completed:")
	t.Logf("  Total operations: %d", opsCount.Load())
	t.Logf("  Errors: %d", errorCount.Load())

	// Verify no errors
	if errorCount.Load() > 0 {
		t.Errorf("Test completed with %d errors/panics", errorCount.Load())
	}
}
