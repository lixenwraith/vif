package systems

import (
	"reflect"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// BenchmarkCleanerSpawn benchmarks cleaner spawning performance
func BenchmarkCleanerSpawn(b *testing.B) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24)
	defer cleanerSystem.Shutdown()

	// Create Red characters on multiple rows
	for row := 0; row < 24; row++ {
		for x := 10; x < 70; x += 10 {
			createRedCharacterAt(world, x, row)
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cleanerSystem.TriggerCleaners(world)
		cleanerSystem.Update(world, 16*time.Millisecond)

		// Wait for spawn to complete
		time.Sleep(50 * time.Millisecond)

		// Clean up for next iteration
		cleanerType := reflect.TypeOf(components.CleanerComponent{})
		cleaners := world.GetEntitiesWith(cleanerType)
		for _, entity := range cleaners {
			world.DestroyEntity(entity)
		}
	}
}

// BenchmarkCleanerDetectAndDestroy benchmarks collision detection and destruction
func BenchmarkCleanerDetectAndDestroy(b *testing.B) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24)
	defer cleanerSystem.Shutdown()

	// Setup: Create cleaners and Red characters
	for row := 0; row < 10; row++ {
		for x := 10; x < 70; x += 5 {
			createRedCharacterAt(world, x, row)
		}
	}

	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cleanerSystem.detectAndDestroyRedCharacters(world)
	}
}

// BenchmarkCleanerScanRedRows benchmarks scanning for Red character rows
func BenchmarkCleanerScanRedRows(b *testing.B) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24)
	defer cleanerSystem.Shutdown()

	// Create Red characters scattered across the screen
	for row := 0; row < 24; row++ {
		for x := 0; x < 80; x += 8 {
			if row%3 == 0 {
				createRedCharacterAt(world, x, row)
			} else if row%3 == 1 {
				createBlueCharacterAt(world, x, row)
			} else {
				createGreenCharacterAt(world, x, row)
			}
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = cleanerSystem.scanRedCharacterRows(world)
	}
}

// BenchmarkCleanerUpdate benchmarks cleaner position updates
func BenchmarkCleanerUpdate(b *testing.B) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24)
	defer cleanerSystem.Shutdown()

	// Create Red characters and spawn cleaners
	for row := 0; row < 24; row++ {
		createRedCharacterAt(world, 40, row)
	}

	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cleanerSystem.Update(world, 16*time.Millisecond)
	}
}

// BenchmarkFlashEffectCreation benchmarks flash effect creation
func BenchmarkFlashEffectCreation(b *testing.B) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24)
	defer cleanerSystem.Shutdown()

	// Pre-create entities for consistent benchmarking
	for i := 0; i < 100; i++ {
		createRedCharacterAt(world, 10+i%70, i%24)
	}

	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cleanerSystem.detectAndDestroyRedCharacters(world)

		// Clean up flashes for next iteration
		flashType := reflect.TypeOf(components.RemovalFlashComponent{})
		flashes := world.GetEntitiesWith(flashType)
		for _, entity := range flashes {
			world.DestroyEntity(entity)
		}
	}
}

// BenchmarkFlashEffectCleanup benchmarks flash effect cleanup
func BenchmarkFlashEffectCleanup(b *testing.B) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24)
	defer cleanerSystem.Shutdown()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		b.StopTimer()

		// Create flash effects
		for j := 0; j < 50; j++ {
			flashEntity := world.CreateEntity()
			flash := components.RemovalFlashComponent{
				X:         j,
				Y:         0,
				Char:      'R',
				StartTime: ctx.TimeProvider.Now().Add(-200 * time.Millisecond), // Expired
				Duration:  150,
			}
			world.AddComponent(flashEntity, flash)
		}

		b.StartTimer()

		cleanerSystem.cleanupExpiredFlashes(world)
	}
}

// BenchmarkGoldSequenceSpawn benchmarks gold sequence spawning
func BenchmarkGoldSequenceSpawn(b *testing.B) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	decaySystem := NewDecaySystem(80, 24, 80, 0, ctx)
	goldSystem := NewGoldSequenceSystem(ctx, decaySystem, 80, 24, 0, 0)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		goldSystem.spawnGoldSequence(world)

		// Clean up for next iteration
		b.StopTimer()
		goldSystem.removeGoldSequence(world)
		b.StartTimer()
	}
}

// BenchmarkGoldSequenceCompletion benchmarks gold sequence completion
func BenchmarkGoldSequenceCompletion(b *testing.B) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	decaySystem := NewDecaySystem(80, 24, 80, 0, ctx)
	goldSystem := NewGoldSequenceSystem(ctx, decaySystem, 80, 24, 0, 0)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		goldSystem.spawnGoldSequence(world)
		b.StartTimer()

		goldSystem.CompleteGoldSequence(world)
	}
}

// BenchmarkConcurrentCleanerOperations benchmarks concurrent cleaner operations
func BenchmarkConcurrentCleanerOperations(b *testing.B) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24)
	defer cleanerSystem.Shutdown()

	// Create test environment
	for row := 0; row < 24; row++ {
		for x := 10; x < 70; x += 10 {
			createRedCharacterAt(world, x, row)
		}
	}

	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Mix of operations
			_ = cleanerSystem.IsActive()
			_ = cleanerSystem.scanRedCharacterRows(world)
			cleanerSystem.detectAndDestroyRedCharacters(world)
		}
	})
}

// BenchmarkSpatialIndexAccess benchmarks spatial index access patterns
func BenchmarkSpatialIndexAccess(b *testing.B) {
	world := engine.NewWorld()

	// Fill spatial index
	for y := 0; y < 24; y++ {
		for x := 0; x < 80; x += 2 {
			entity := createRedCharacterAt(world, x, y)
			_ = entity
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for y := 0; y < 24; y++ {
			for x := 0; x < 80; x += 4 {
				_ = world.GetEntityAtPosition(x, y)
			}
		}
	}
}

// BenchmarkEntityQueryByType benchmarks entity queries by component type
func BenchmarkEntityQueryByType(b *testing.B) {
	world := engine.NewWorld()

	// Create various entities
	for i := 0; i < 200; i++ {
		if i%3 == 0 {
			createRedCharacterAt(world, i%80, i%24)
		} else if i%3 == 1 {
			createBlueCharacterAt(world, i%80, i%24)
		} else {
			createGreenCharacterAt(world, i%80, i%24)
		}
	}

	seqType := reflect.TypeOf(components.SequenceComponent{})
	posType := reflect.TypeOf(components.PositionComponent{})

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = world.GetEntitiesWith(seqType, posType)
	}
}

// BenchmarkCleanerPoolAllocation benchmarks pool allocation performance
func BenchmarkCleanerPoolAllocation(b *testing.B) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24)
	defer cleanerSystem.Shutdown()

	// Create Red characters
	for row := 0; row < 10; row++ {
		createRedCharacterAt(world, 40, row)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cleanerSystem.TriggerCleaners(world)
		cleanerSystem.Update(world, 16*time.Millisecond)
		time.Sleep(10 * time.Millisecond)

		// Force cleanup to trigger pool return
		b.StopTimer()
		cleanerType := reflect.TypeOf(components.CleanerComponent{})
		cleaners := world.GetEntitiesWith(cleanerType)
		for _, entity := range cleaners {
			world.DestroyEntity(entity)
		}
		cleanerSystem.cleanupCleaners(world)
		b.StartTimer()
	}
}

// BenchmarkAtomicStateOperations benchmarks atomic state operations
func BenchmarkAtomicStateOperations(b *testing.B) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24)
	defer cleanerSystem.Shutdown()

	createRedCharacterAt(world, 40, 5)

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = cleanerSystem.IsActive()
		}
	})
}

// BenchmarkGoldTriggerCleaners benchmarks triggering cleaners via gold completion
func BenchmarkGoldTriggerCleaners(b *testing.B) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24)
	defer cleanerSystem.Shutdown()

	decaySystem := NewDecaySystem(80, 24, 80, 0, ctx)
	goldSystem := NewGoldSequenceSystem(ctx, decaySystem, 80, 24, 0, 0)
	goldSystem.SetCleanerTrigger(cleanerSystem.TriggerCleaners)

	// Create Red characters
	for i := 0; i < 10; i++ {
		createRedCharacterAt(world, 10+i*5, 5)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		goldSystem.TriggerCleanersIfHeatFull(world, 100, 100)
		cleanerSystem.Update(world, 16*time.Millisecond)

		// Clean up
		b.StopTimer()
		cleanerType := reflect.TypeOf(components.CleanerComponent{})
		cleaners := world.GetEntitiesWith(cleanerType)
		for _, entity := range cleaners {
			world.DestroyEntity(entity)
		}
		cleanerSystem.cleanupCleaners(world)
		b.StartTimer()
	}
}

// BenchmarkCompleteGoldCleanerPipeline benchmarks the full pipeline
func BenchmarkCompleteGoldCleanerPipeline(b *testing.B) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24)
	defer cleanerSystem.Shutdown()

	decaySystem := NewDecaySystem(80, 24, 80, 0, ctx)
	goldSystem := NewGoldSequenceSystem(ctx, decaySystem, 80, 24, 0, 0)
	goldSystem.SetCleanerTrigger(cleanerSystem.TriggerCleaners)

	// Create environment
	for row := 0; row < 24; row++ {
		for x := 10; x < 70; x += 10 {
			if row%2 == 0 {
				createRedCharacterAt(world, x, row)
			}
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Spawn gold
		goldSystem.spawnGoldSequence(world)

		// Complete gold (triggers cleaners if heat full)
		goldSystem.TriggerCleanersIfHeatFull(world, 100, 100)

		// Process cleaners
		cleanerSystem.Update(world, 16*time.Millisecond)
		cleanerSystem.detectAndDestroyRedCharacters(world)

		// Clean up
		b.StopTimer()
		goldSystem.removeGoldSequence(world)
		cleanerType := reflect.TypeOf(components.CleanerComponent{})
		cleaners := world.GetEntitiesWith(cleanerType)
		for _, entity := range cleaners {
			world.DestroyEntity(entity)
		}
		cleanerSystem.cleanupCleaners(world)
		b.StartTimer()
	}
}
