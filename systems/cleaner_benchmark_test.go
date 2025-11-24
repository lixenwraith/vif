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
	cleanerSystem := NewCleanerSystem(ctx)

	// Create Red characters on multiple rows
	for row := 0; row < 24; row++ {
		for x := 10; x < 70; x += 10 {
			createRedCharacterAt(world, x, row)
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ctx.PushEvent(engine.EventCleanerRequest, nil)
		cleanerSystem.Update(world, 16*time.Millisecond)

		// Clean up for next iteration
		// Using direct store access instead of reflection
		cleaners := world.Cleaners.All()
		for _, entity := range cleaners {
			world.DestroyEntity(entity)
		}
	}
}

// BenchmarkCleanerUpdate benchmarks cleaner physics updates
func BenchmarkCleanerUpdate(b *testing.B) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	// Create Red characters
	for row := 0; row < 24; row++ {
		for x := 10; x < 70; x += 10 {
			createRedCharacterAt(world, x, row)
		}
	}

	// Activate cleaners via event
	ctx.PushEvent(engine.EventCleanerRequest, nil)
	cleanerSystem.Update(world, 16*time.Millisecond)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cleanerSystem.Update(world, 16*time.Millisecond)
	}
}

// BenchmarkCleanerSnapshots benchmarks querying cleaner components from World
func BenchmarkCleanerSnapshots(b *testing.B) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	// Create Red characters
	for row := 0; row < 24; row++ {
		for x := 10; x < 70; x += 10 {
			createRedCharacterAt(world, x, row)
		}
	}

	// Activate cleaners via event
	ctx.PushEvent(engine.EventCleanerRequest, nil)
	cleanerSystem.Update(world, 16*time.Millisecond)

	// Using direct store access instead of reflection

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Query World directly as renderer does
		entities := world.Cleaners.All()
		for _, entity := range entities {
			if c, ok := world.Cleaners.Get(entity); ok {
				// c is already CleanerComponent from Get()
				_ = c.GridY
				_ = len(c.Trail)
			}
		}
	}
}

// BenchmarkCleanerCollision benchmarks collision detection performance
func BenchmarkCleanerCollision(b *testing.B) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	// Create Red characters densely across screen
	for row := 0; row < 24; row++ {
		for x := 0; x < 80; x++ {
			createRedCharacterAt(world, x, row)
		}
	}

	// Activate cleaners via event
	ctx.PushEvent(engine.EventCleanerRequest, nil)
	// Process the event to activate cleaners
	cleanerSystem.Update(world, 0)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cleanerSystem.Update(world, 16*time.Millisecond)
	}
}

// BenchmarkFlashEffectCreation benchmarks flash effect creation
func BenchmarkFlashEffectCreation(b *testing.B) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()
	cleanerSystem := NewCleanerSystem(ctx)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Create Red character
		redEntity := createRedCharacterAt(world, 40, 5)

		// Activate cleaners via event
		ctx.PushEvent(engine.EventCleanerRequest, nil)

		// Run a few updates to trigger collision and flash creation
		for j := 0; j < 10; j++ {
			cleanerSystem.Update(world, 16*time.Millisecond)
		}

		// Clean up
		// Using direct store access instead of reflection
		cleaners := world.Cleaners.All()
		for _, entity := range cleaners {
			world.DestroyEntity(entity)
		}

		flashType := reflect.TypeOf(components.RemovalFlashComponent{})
		flashes := world.RemovalFlashes.All()
		for _, entity := range flashes {
			world.DestroyEntity(entity)
		}

		if entityExists(world, redEntity) {
			world.DestroyEntity(redEntity)
		}
	}
}

// BenchmarkCompleteAnimation benchmarks full cleaner animation cycle
func BenchmarkCompleteAnimation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()

		world := engine.NewWorld()
		ctx := createCleanerTestContext()
		cleanerSystem := NewCleanerSystem(ctx)

		// Create Red characters
		for row := 0; row < 10; row++ {
			createRedCharacterAt(world, 40, row)
		}

		b.StartTimer()

		// Activate cleaners via event
		ctx.PushEvent(engine.EventCleanerRequest, nil)

		// Run until complete (check for cleaner entities)
		// Using direct store access instead of reflection
		maxIterations := 1000
		for j := 0; j < maxIterations; j++ {
			cleanerSystem.Update(world, 16*time.Millisecond)
			entities := world.Cleaners.All()
			if len(entities) == 0 {
				break // Animation complete
			}
		}
	}
}
