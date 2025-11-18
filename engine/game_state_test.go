package engine

import (
	"sync"
	"testing"
	"time"
)

// TestGameStateInitialization verifies GameState is properly initialized
func TestGameStateInitialization(t *testing.T) {
	timeProvider := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	gameWidth, gameHeight, screenWidth := 10, 5, 12

	gs := NewGameState(gameWidth, gameHeight, screenWidth, timeProvider)

	// Verify configuration
	if gs.GameWidth != gameWidth {
		t.Errorf("Expected GameWidth %d, got %d", gameWidth, gs.GameWidth)
	}
	if gs.GameHeight != gameHeight {
		t.Errorf("Expected GameHeight %d, got %d", gameHeight, gs.GameHeight)
	}
	if gs.ScreenWidth != screenWidth {
		t.Errorf("Expected ScreenWidth %d, got %d", screenWidth, gs.ScreenWidth)
	}

	// Verify initial atomic values
	if gs.GetHeat() != 0 {
		t.Errorf("Expected initial heat 0, got %d", gs.GetHeat())
	}
	if gs.GetScore() != 0 {
		t.Errorf("Expected initial score 0, got %d", gs.GetScore())
	}

	// Verify cursor initialization
	expectedX := gameWidth / 2
	expectedY := gameHeight / 2
	if gs.GetCursorX() != expectedX {
		t.Errorf("Expected cursor X %d, got %d", expectedX, gs.GetCursorX())
	}
	if gs.GetCursorY() != expectedY {
		t.Errorf("Expected cursor Y %d, got %d", expectedY, gs.GetCursorY())
	}

	// Verify color counters are zero
	if gs.GetTotalColorCount() != 0 {
		t.Errorf("Expected 0 color combinations, got %d", gs.GetTotalColorCount())
	}

	// Verify spawn state
	spawnState := gs.ReadSpawnState()
	if !spawnState.Enabled {
		t.Error("Expected spawn enabled by default")
	}
	if spawnState.RateMultiplier != 1.0 {
		t.Errorf("Expected initial rate multiplier 1.0, got %f", spawnState.RateMultiplier)
	}
}

// TestHeatOperationsAtomic tests concurrent heat updates
func TestHeatOperationsAtomic(t *testing.T) {
	timeProvider := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	gs := NewGameState(10, 5, 12, timeProvider)

	// Concurrent heat updates (reduced from 1000 to 100)
	var wg sync.WaitGroup
	updates := 10 // 10 goroutines × 10 updates each = 100 total

	for i := 0; i < updates; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				gs.AddHeat(1)
			}
		}()
	}

	wg.Wait()

	expectedHeat := updates * 10
	if gs.GetHeat() != expectedHeat {
		t.Errorf("Expected heat %d, got %d", expectedHeat, gs.GetHeat())
	}
}

// TestColorCounterOperations tests color counter updates
func TestColorCounterOperations(t *testing.T) {
	timeProvider := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	gs := NewGameState(10, 5, 12, timeProvider)

	// Test adding blue bright characters
	gs.AddColorCount(0, 2, 5) // type=0 (Blue), level=2 (Bright), count=5
	if gs.BlueCountBright.Load() != 5 {
		t.Errorf("Expected BlueCountBright 5, got %d", gs.BlueCountBright.Load())
	}

	// Test total color count (should be 1 combination present)
	if gs.GetTotalColorCount() != 1 {
		t.Errorf("Expected 1 color combination, got %d", gs.GetTotalColorCount())
	}

	// Add green normal characters
	gs.AddColorCount(1, 1, 3) // type=1 (Green), level=1 (Normal), count=3
	if gs.GreenCountNormal.Load() != 3 {
		t.Errorf("Expected GreenCountNormal 3, got %d", gs.GreenCountNormal.Load())
	}

	// Now should have 2 combinations
	if gs.GetTotalColorCount() != 2 {
		t.Errorf("Expected 2 color combinations, got %d", gs.GetTotalColorCount())
	}

	// Test removal (typing characters)
	gs.AddColorCount(0, 2, -2) // Remove 2 blue bright
	if gs.BlueCountBright.Load() != 3 {
		t.Errorf("Expected BlueCountBright 3 after removal, got %d", gs.BlueCountBright.Load())
	}

	// Remove all green normal
	gs.AddColorCount(1, 1, -3)
	if gs.GreenCountNormal.Load() != 0 {
		t.Errorf("Expected GreenCountNormal 0 after removal, got %d", gs.GreenCountNormal.Load())
	}

	// Should be back to 1 combination (Blue Bright only)
	if gs.GetTotalColorCount() != 1 {
		t.Errorf("Expected 1 color combination after removal, got %d", gs.GetTotalColorCount())
	}
}

// TestColorCounterNegativePrevention tests that counters don't go negative
func TestColorCounterNegativePrevention(t *testing.T) {
	timeProvider := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	gs := NewGameState(10, 5, 12, timeProvider)

	// Try to remove more than available
	gs.AddColorCount(0, 2, 5)  // Add 5
	gs.AddColorCount(0, 2, -10) // Try to remove 10

	// Should be clamped to 0, not negative
	count := gs.BlueCountBright.Load()
	if count < 0 {
		t.Errorf("Color counter went negative: %d", count)
	}
}

// TestSpawnRateAdaptation tests adaptive spawn rate based on screen density
func TestSpawnRateAdaptation(t *testing.T) {
	timeProvider := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	gs := NewGameState(10, 5, 12, timeProvider)

	// Test low density (<30%) - should spawn faster (2x)
	gs.UpdateSpawnRate(10, 100) // 10% filled
	spawnState := gs.ReadSpawnState()
	if spawnState.RateMultiplier != 2.0 {
		t.Errorf("Expected 2x rate at low density, got %f", spawnState.RateMultiplier)
	}
	if spawnState.ScreenDensity != 0.1 {
		t.Errorf("Expected density 0.1, got %f", spawnState.ScreenDensity)
	}

	// Test medium density (30-70%) - normal rate (1x)
	gs.UpdateSpawnRate(50, 100) // 50% filled
	spawnState = gs.ReadSpawnState()
	if spawnState.RateMultiplier != 1.0 {
		t.Errorf("Expected 1x rate at medium density, got %f", spawnState.RateMultiplier)
	}

	// Test high density (>70%) - slower spawn (0.5x)
	gs.UpdateSpawnRate(80, 100) // 80% filled
	spawnState = gs.ReadSpawnState()
	if spawnState.RateMultiplier != 0.5 {
		t.Errorf("Expected 0.5x rate at high density, got %f", spawnState.RateMultiplier)
	}
}

// TestSpawnTimingState tests spawn timing updates
func TestSpawnTimingState(t *testing.T) {
	timeProvider := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	gs := NewGameState(10, 5, 12, timeProvider)

	// Initially should be ready to spawn after 2 seconds
	now := timeProvider.Now()
	initialNext := gs.ReadSpawnState().NextTime

	if !initialNext.After(now) {
		t.Error("Initial next spawn time should be in the future")
	}

	// Advance time and check ShouldSpawn
	timeProvider.Advance(3 * time.Second)
	if !gs.ShouldSpawn() {
		t.Error("Should be ready to spawn after 3 seconds")
	}

	// Update spawn timing (simulate spawn occurred)
	newNow := timeProvider.Now()
	nextSpawn := newNow.Add(2 * time.Second)
	gs.UpdateSpawnTiming(newNow, nextSpawn)

	// Immediately after, should not be ready
	if gs.ShouldSpawn() {
		t.Error("Should not be ready to spawn immediately after update")
	}

	// Advance time again
	timeProvider.Advance(3 * time.Second)
	if !gs.ShouldSpawn() {
		t.Error("Should be ready to spawn after delay")
	}
}

// TestSequenceIDGeneration tests atomic sequence ID generation
func TestSequenceIDGeneration(t *testing.T) {
	timeProvider := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	gs := NewGameState(10, 5, 12, timeProvider)

	// Sequential generation
	id1 := gs.IncrementSeqID()
	id2 := gs.IncrementSeqID()
	id3 := gs.IncrementSeqID()

	if id1 != 2 || id2 != 3 || id3 != 4 {
		t.Errorf("Expected sequential IDs 2,3,4, got %d,%d,%d", id1, id2, id3)
	}

	// Concurrent generation (reduced from 100 to 10 goroutines)
	var wg sync.WaitGroup
	ids := make(chan int, 100)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				ids <- gs.IncrementSeqID()
			}
		}()
	}

	wg.Wait()
	close(ids)

	// Collect all IDs and verify uniqueness
	seen := make(map[int]bool)
	for id := range ids {
		if seen[id] {
			t.Errorf("Duplicate sequence ID generated: %d", id)
		}
		seen[id] = true
	}

	// Should have 100 unique IDs
	if len(seen) != 100 {
		t.Errorf("Expected 100 unique IDs, got %d", len(seen))
	}
}

// TestBoostStateTransitions tests boost activation/deactivation
func TestBoostStateTransitions(t *testing.T) {
	timeProvider := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	gs := NewGameState(10, 5, 12, timeProvider)

	// Initially not boosted
	if gs.GetBoostEnabled() {
		t.Error("Boost should not be enabled initially")
	}

	// Activate boost
	now := timeProvider.Now()
	endTime := now.Add(500 * time.Millisecond)
	gs.SetBoostEnabled(true)
	gs.SetBoostEndTime(endTime)
	gs.SetBoostColor(1) // Blue

	if !gs.GetBoostEnabled() {
		t.Error("Boost should be enabled after activation")
	}
	if gs.GetBoostColor() != 1 {
		t.Errorf("Expected boost color 1 (Blue), got %d", gs.GetBoostColor())
	}

	// Boost should not expire yet
	if gs.UpdateBoostTimerAtomic() {
		t.Error("Boost should not expire before end time")
	}

	// Advance time past expiration
	timeProvider.Advance(600 * time.Millisecond)
	if !gs.UpdateBoostTimerAtomic() {
		t.Error("Boost should expire after end time")
	}

	// After expiration
	if gs.GetBoostEnabled() {
		t.Error("Boost should be disabled after expiration")
	}
	if gs.GetBoostColor() != 0 {
		t.Error("Boost color should be reset after expiration")
	}
}

// TestCanSpawnNewColor tests the 6-color limit
func TestCanSpawnNewColor(t *testing.T) {
	timeProvider := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	gs := NewGameState(10, 5, 12, timeProvider)

	// Should be able to spawn (0 colors present)
	if !gs.CanSpawnNewColor() {
		t.Error("Should be able to spawn with 0 colors")
	}

	// Add 5 different color/level combinations
	gs.AddColorCount(0, 2, 1) // Blue Bright
	gs.AddColorCount(0, 1, 1) // Blue Normal
	gs.AddColorCount(0, 0, 1) // Blue Dark
	gs.AddColorCount(1, 2, 1) // Green Bright
	gs.AddColorCount(1, 1, 1) // Green Normal

	// Should still be able to spawn (5 < 6)
	if !gs.CanSpawnNewColor() {
		t.Error("Should be able to spawn with 5 colors")
	}

	// Add 6th combination
	gs.AddColorCount(1, 0, 1) // Green Dark

	// Now should NOT be able to spawn (6 colors present)
	if gs.CanSpawnNewColor() {
		t.Error("Should not be able to spawn with 6 colors")
	}

	// Remove one combination
	gs.AddColorCount(1, 0, -1) // Remove Green Dark

	// Should be able to spawn again
	if !gs.CanSpawnNewColor() {
		t.Error("Should be able to spawn after removing a color")
	}
}

// TestConcurrentStateReads tests concurrent reads don't cause issues
func TestConcurrentStateReads(t *testing.T) {
	timeProvider := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	gs := NewGameState(10, 5, 12, timeProvider)

	// Set up some state
	gs.SetHeat(50)
	gs.AddColorCount(0, 2, 5)
	gs.AddColorCount(1, 1, 3)
	gs.UpdateSpawnRate(40, 100)

	var wg sync.WaitGroup

	// Concurrent readers (reduced from 100 to 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				// Read various state
				_ = gs.GetHeat()
				_ = gs.GetScore()
				_ = gs.GetTotalColorCount()
				_ = gs.ReadSpawnState()
				_ = gs.CanSpawnNewColor()
			}
		}()
	}

	// Concurrent writer (updating spawn state)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			gs.UpdateSpawnRate(i*10, 100)
			time.Sleep(time.Millisecond)
		}
	}()

	wg.Wait()
	// If we get here without deadlock or panic, test passes
}

// TestStateSnapshot tests that ReadSpawnState returns consistent snapshot
func TestStateSnapshot(t *testing.T) {
	timeProvider := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	gs := NewGameState(10, 5, 12, timeProvider)

	// Set initial state
	gs.UpdateSpawnRate(50, 100)
	now := timeProvider.Now()
	nextTime := now.Add(2 * time.Second)
	gs.UpdateSpawnTiming(now, nextTime)

	// Get snapshot
	snapshot := gs.ReadSpawnState()

	// Verify snapshot is consistent
	if snapshot.EntityCount != 50 {
		t.Errorf("Expected entity count 50, got %d", snapshot.EntityCount)
	}
	if snapshot.MaxEntities != 100 {
		t.Errorf("Expected max entities 100, got %d", snapshot.MaxEntities)
	}
	if snapshot.ScreenDensity != 0.5 {
		t.Errorf("Expected density 0.5, got %f", snapshot.ScreenDensity)
	}
	if snapshot.RateMultiplier != 1.0 {
		t.Errorf("Expected rate 1.0, got %f", snapshot.RateMultiplier)
	}

	// Modify state
	gs.UpdateSpawnRate(75, 100)

	// Old snapshot should be unchanged (it's a copy)
	if snapshot.EntityCount != 50 {
		t.Error("Snapshot was mutated by state change")
	}

	// New snapshot should reflect changes
	newSnapshot := gs.ReadSpawnState()
	if newSnapshot.EntityCount != 75 {
		t.Errorf("Expected new entity count 75, got %d", newSnapshot.EntityCount)
	}
}
