package engine

import (
	"sync"
	"testing"
	"time"
)

// TestSystem is a simple system for testing
type TestSystem struct {
	updateCount int
	mu          sync.Mutex
}

func (s *TestSystem) Update(world *World, dt time.Duration) {
	s.mu.Lock()
	s.updateCount++
	s.mu.Unlock()

	// Simulate some work
	entities := world.GetEntitiesWith()
	_ = entities
}

func (s *TestSystem) Priority() int {
	return 10
}

func (s *TestSystem) GetUpdateCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.updateCount
}

// TestConcurrentWorldUpdate tests that World.Update() is thread-safe
func TestConcurrentWorldUpdate(t *testing.T) {
	world := NewWorld()
	system := &TestSystem{}
	world.AddSystem(system)

	// Create some test entities
	for i := 0; i < 10; i++ {
		entity := world.CreateEntity()
		world.AddComponent(entity, TestComponent{X: i, Y: i})
	}

	// Run Update concurrently from multiple goroutines
	var wg sync.WaitGroup
	iterations := 100

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				world.Update(16 * time.Millisecond)
			}
		}()
	}

	wg.Wait()

	// Verify the system was called the expected number of times
	expectedCount := 5 * iterations
	actualCount := system.GetUpdateCount()
	if actualCount != expectedCount {
		t.Errorf("Expected %d updates, got %d", expectedCount, actualCount)
	}
}

// TestConcurrentEntityOperations tests concurrent entity creation/destruction
func TestConcurrentEntityOperations(t *testing.T) {
	world := NewWorld()

	var wg sync.WaitGroup
	entitiesPerGoroutine := 50

	// Concurrently create entities
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < entitiesPerGoroutine; j++ {
				entity := world.CreateEntity()
				world.AddComponent(entity, TestComponent{X: j, Y: j})
			}
		}()
	}

	wg.Wait()

	// Verify entity count
	expectedCount := 5 * entitiesPerGoroutine
	actualCount := world.EntityCount()
	if actualCount != expectedCount {
		t.Errorf("Expected %d entities, got %d", expectedCount, actualCount)
	}
}

// TestConcurrentSpatialIndexAccess tests concurrent spatial index operations
func TestConcurrentSpatialIndexAccess(t *testing.T) {
	world := NewWorld()

	// Pre-create entities at different positions
	entities := make([]Entity, 100)
	for i := 0; i < 100; i++ {
		entities[i] = world.CreateEntity()
		world.AddComponent(entities[i], TestComponent{X: i, Y: i})
		world.UpdateSpatialIndex(entities[i], i, i)
	}

	var wg sync.WaitGroup

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				entity := world.GetEntityAtPosition(j, j)
				if entity == 0 {
					t.Errorf("Goroutine %d: Expected entity at position (%d, %d), got 0", goroutineID, j, j)
				}
			}
		}(i)
	}

	wg.Wait()
}

// TestConcurrentComponentAccess tests concurrent component operations
func TestConcurrentComponentAccess(t *testing.T) {
	world := NewWorld()

	// Create entities
	entities := make([]Entity, 50)
	for i := 0; i < 50; i++ {
		entities[i] = world.CreateEntity()
		world.AddComponent(entities[i], TestComponent{X: i, Y: i})
	}

	var wg sync.WaitGroup

	// Concurrent component reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, entity := range entities {
				comp, ok := world.GetComponent(entity, TestComponentType())
				if !ok {
					t.Error("Failed to get component")
				}
				testComp := comp.(TestComponent)
				_ = testComp
			}
		}()
	}

	// Concurrent component updates
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j, entity := range entities {
				world.AddComponent(entity, TestComponent{X: j + goroutineID*100, Y: j + goroutineID*100})
			}
		}(i)
	}

	wg.Wait()
}

// TestComponentType returns the reflect.Type for TestComponent
func TestComponentType() interface{} {
	return TestComponent{}
}

// TestSystemIterationSafety verifies systems are safely iterated
func TestSystemIterationSafety(t *testing.T) {
	world := NewWorld()

	// Add multiple systems
	for i := 0; i < 5; i++ {
		world.AddSystem(&TestSystem{})
	}

	var wg sync.WaitGroup

	// Concurrent Update calls
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				world.Update(16 * time.Millisecond)
			}
		}()
	}

	wg.Wait()

	// If we get here without panic, the test passed
}
