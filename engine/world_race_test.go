package engine

import (
	"sync"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
)

// TestRaceSystem is a simple system for testing race conditions
type TestRaceSystem struct {
	updateCount int
	mu          sync.Mutex
	testStore   *Store[TestComponent]
}

func (s *TestRaceSystem) Update(world *World, dt time.Duration) {
	s.mu.Lock()
	s.updateCount++
	s.mu.Unlock()

	// Simulate some work by iterating over entities with test components
	if s.testStore != nil {
		entities := s.testStore.All()
		_ = entities
	}
}

func (s *TestRaceSystem) Priority() int {
	return 10
}

func (s *TestRaceSystem) GetUpdateCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.updateCount
}

// TestConcurrentWorldUpdate tests that World.Update() is thread-safe
func TestConcurrentWorldUpdate(t *testing.T) {
	world := NewWorld()
	testStore := NewStore[TestComponent]()
	system := &TestRaceSystem{testStore: testStore}
	world.AddSystem(system)

	// Create some test entities
	for i := 0; i < 10; i++ {
		entity := world.CreateEntity()
		testStore.Add(entity, TestComponent{X: i, Y: i})
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
	testStore := NewStore[TestComponent]()

	var wg sync.WaitGroup
	entitiesPerGoroutine := 50

	// Concurrently create entities
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < entitiesPerGoroutine; j++ {
				entity := world.CreateEntity()
				testStore.Add(entity, TestComponent{X: j, Y: j})
			}
		}()
	}

	wg.Wait()

	// Verify entity count in test store
	expectedCount := 5 * entitiesPerGoroutine
	actualCount := testStore.Count()
	if actualCount != expectedCount {
		t.Errorf("Expected %d entities in store, got %d", expectedCount, actualCount)
	}
}

// TestConcurrentSpatialIndexAccess tests concurrent spatial index operations
func TestConcurrentSpatialIndexAccess(t *testing.T) {
	world := NewWorld()
	testStore := NewStore[TestComponent]()

	// Pre-create entities at different positions
	entities := make([]Entity, 100)
	for i := 0; i < 100; i++ {
		entities[i] = world.CreateEntity()
		testStore.Add(entities[i], TestComponent{X: i, Y: i})

		tx := world.BeginSpatialTransaction()
		tx.Spawn(entities[i], i, i)
		tx.Commit()
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
	testStore := NewStore[TestComponent]()

	// Create entities
	entities := make([]Entity, 50)
	for i := 0; i < 50; i++ {
		entities[i] = world.CreateEntity()
		testStore.Add(entities[i], TestComponent{X: i, Y: i})
	}

	var wg sync.WaitGroup

	// Concurrent component reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, entity := range entities {
				comp, ok := testStore.Get(entity)
				if !ok {
					t.Error("Failed to get component")
				}
				_ = comp
			}
		}()
	}

	// Concurrent component updates
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j, entity := range entities {
				testStore.Add(entity, TestComponent{X: j + goroutineID*100, Y: j + goroutineID*100})
			}
		}(i)
	}

	wg.Wait()
}

// TestSystemIterationSafety verifies systems are safely iterated
func TestSystemIterationSafety(t *testing.T) {
	world := NewWorld()

	// Add multiple systems
	for i := 0; i < 5; i++ {
		world.AddSystem(&TestRaceSystem{})
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

// TestConcurrentPositionStoreOperations tests race conditions in PositionStore
func TestConcurrentPositionStoreOperations(t *testing.T) {
	world := NewWorld()

	// Create entities with positions
	entities := make([]Entity, 100)
	for i := 0; i < 100; i++ {
		entities[i] = world.CreateEntity()
		world.Positions.Add(entities[i], components.PositionComponent{X: i, Y: i})
	}

	var wg sync.WaitGroup

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, entity := range entities {
				pos, ok := world.Positions.Get(entity)
				if !ok {
					t.Error("Failed to get position")
				}
				_ = pos
			}
		}()
	}

	// Concurrent writes (updates)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j, entity := range entities {
				// Move to a new position that won't collide
				newX := j + (goroutineID+1)*1000
				newY := j + (goroutineID+1)*1000
				world.Positions.Add(entity, components.PositionComponent{X: newX, Y: newY})
			}
		}(i)
	}

	wg.Wait()
}

// TestConcurrentQuery tests concurrent query operations
func TestConcurrentQuery(t *testing.T) {
	world := NewWorld()

	// Create entities with different component combinations
	for i := 0; i < 100; i++ {
		entity := world.CreateEntity()
		world.Positions.Add(entity, components.PositionComponent{X: i, Y: i})
		if i%2 == 0 {
			world.Characters.Add(entity, components.CharacterComponent{Rune: rune('A' + (i % 26))})
		}
	}

	var wg sync.WaitGroup

	// Concurrent queries
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				// Query entities with both Position and Character
				entities := world.Query().
					With(world.Positions).
					With(world.Characters).
					Execute()

				if len(entities) != 50 {
					t.Errorf("Expected 50 entities, got %d", len(entities))
				}
			}
		}()
	}

	wg.Wait()
}

// TestConcurrentEntityDestruction tests concurrent entity destruction
func TestConcurrentEntityDestruction(t *testing.T) {
	world := NewWorld()

	// Create entities
	entities := make([]Entity, 100)
	for i := 0; i < 100; i++ {
		entities[i] = world.CreateEntity()
		world.Positions.Add(entities[i], components.PositionComponent{X: i, Y: i})
		world.Characters.Add(entities[i], components.CharacterComponent{Rune: 'A'})
	}

	var wg sync.WaitGroup

	// Concurrently destroy half the entities
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			world.DestroyEntity(entities[idx])
		}(i)
	}

	wg.Wait()

	// Verify remaining entities
	posCount := world.Positions.Count()
	charCount := world.Characters.Count()

	if posCount != 50 {
		t.Errorf("Expected 50 entities with position, got %d", posCount)
	}
	if charCount != 50 {
		t.Errorf("Expected 50 entities with character, got %d", charCount)
	}
}
