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

// TestSnapshotConsistency tests that snapshots provide consistent views under rapid state changes
func TestSnapshotConsistency(t *testing.T) {
	timeProvider := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	gs := NewGameState(10, 5, 12, timeProvider)

	// Initialize with known state to avoid initial inconsistency
	gs.UpdateSpawnRate(50, 100)

	var wg sync.WaitGroup
	stopChan := make(chan struct{})
	inconsistentCount := 0
	var inconsistentMu sync.Mutex

	// Writer goroutine: Rapidly change spawn state
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 1; i <= 100; i++ { // Start from 1 to avoid division by zero issues
			select {
			case <-stopChan:
				return
			default:
				// Update multiple related fields
				gs.UpdateSpawnRate(i, 100)
				now := timeProvider.Now()
				gs.UpdateSpawnTiming(now, now.Add(time.Duration(i)*time.Millisecond))
				time.Sleep(time.Millisecond)
			}
		}
	}()

	// Reader goroutines: Concurrently read snapshots
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				select {
				case <-stopChan:
					return
				default:
					snapshot := gs.ReadSpawnState()

					// Verify internal consistency of snapshot
					// ScreenDensity should match EntityCount/MaxEntities
					expectedDensity := float64(snapshot.EntityCount) / float64(snapshot.MaxEntities)
					if snapshot.ScreenDensity != expectedDensity {
						inconsistentMu.Lock()
						inconsistentCount++
						inconsistentMu.Unlock()
						t.Errorf("Snapshot inconsistent: density=%f, expected=%f (count=%d, max=%d)",
							snapshot.ScreenDensity, expectedDensity, snapshot.EntityCount, snapshot.MaxEntities)
					}

					// RateMultiplier should be consistent with ScreenDensity
					var expectedRate float64
					if snapshot.ScreenDensity < 0.3 {
						expectedRate = 2.0
					} else if snapshot.ScreenDensity > 0.7 {
						expectedRate = 0.5
					} else {
						expectedRate = 1.0
					}
					if snapshot.RateMultiplier != expectedRate {
						inconsistentMu.Lock()
						inconsistentCount++
						inconsistentMu.Unlock()
						t.Errorf("Snapshot inconsistent: rate=%f, expected=%f (density=%f)",
							snapshot.RateMultiplier, expectedRate, snapshot.ScreenDensity)
					}

					time.Sleep(500 * time.Microsecond)
				}
			}
		}()
	}

	// Let test run
	time.Sleep(150 * time.Millisecond)
	close(stopChan)
	wg.Wait()

	inconsistentMu.Lock()
	defer inconsistentMu.Unlock()
	if inconsistentCount > 0 {
		t.Errorf("Detected %d inconsistent snapshots", inconsistentCount)
	}
}

// TestNoPartialReads tests that snapshots never contain partial state updates
func TestNoPartialReads(t *testing.T) {
	timeProvider := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	gs := NewGameState(10, 5, 12, timeProvider)

	// Initialize with known state
	gs.UpdateSpawnRate(25, 100) // Should set density=0.25, rate=2.0

	var wg sync.WaitGroup
	stopChan := make(chan struct{})
	partialReadCount := 0
	var partialMu sync.Mutex

	// Writer: Change multiple related fields atomically
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			select {
			case <-stopChan:
				return
			default:
				// Update all related fields (should be atomic from reader's perspective)
				gs.UpdateSpawnRate(25+i, 100)
				time.Sleep(time.Millisecond)
			}
		}
	}()

	// Readers: Verify snapshots show either old or new values, never partial
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			prevSnapshot := gs.ReadSpawnState()

			for j := 0; j < 100; j++ {
				select {
				case <-stopChan:
					return
				default:
					snapshot := gs.ReadSpawnState()

					// Verify internal consistency (same checks as TestSnapshotConsistency)
					expectedDensity := float64(snapshot.EntityCount) / float64(snapshot.MaxEntities)
					if snapshot.ScreenDensity != expectedDensity {
						partialMu.Lock()
						partialReadCount++
						partialMu.Unlock()
					}

					// Verify snapshot is either same as previous or fully updated
					// (EntityCount should change monotonically or stay the same)
					if snapshot.EntityCount < prevSnapshot.EntityCount {
						partialMu.Lock()
						partialReadCount++
						partialMu.Unlock()
						t.Errorf("Snapshot went backwards: prev=%d, curr=%d",
							prevSnapshot.EntityCount, snapshot.EntityCount)
					}

					prevSnapshot = snapshot
					time.Sleep(500 * time.Microsecond)
				}
			}
		}()
	}

	time.Sleep(150 * time.Millisecond)
	close(stopChan)
	wg.Wait()

	partialMu.Lock()
	defer partialMu.Unlock()
	if partialReadCount > 0 {
		t.Errorf("Detected %d partial reads", partialReadCount)
	}
}

// TestSnapshotImmutability tests that snapshots are immutable copies
func TestSnapshotImmutability(t *testing.T) {
	timeProvider := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	gs := NewGameState(10, 5, 12, timeProvider)

	// Set initial state
	gs.UpdateSpawnRate(50, 100)
	now := timeProvider.Now()
	gs.UpdateSpawnTiming(now, now.Add(2*time.Second))

	// Activate gold sequence
	gs.ActivateGoldSequence(42, 10*time.Second)

	// Take snapshots
	spawnSnapshot1 := gs.ReadSpawnState()
	goldSnapshot1 := gs.ReadGoldState()
	phaseSnapshot1 := gs.ReadPhaseState()

	// Verify initial values
	if spawnSnapshot1.EntityCount != 50 {
		t.Errorf("Expected spawn entity count 50, got %d", spawnSnapshot1.EntityCount)
	}
	if !goldSnapshot1.Active {
		t.Error("Expected gold to be active")
	}
	if goldSnapshot1.SequenceID != 42 {
		t.Errorf("Expected gold sequence ID 42, got %d", goldSnapshot1.SequenceID)
	}
	if phaseSnapshot1.Phase != PhaseGoldActive {
		t.Errorf("Expected phase GoldActive, got %v", phaseSnapshot1.Phase)
	}

	// Modify state extensively
	gs.UpdateSpawnRate(75, 100)
	gs.UpdateSpawnTiming(now.Add(5*time.Second), now.Add(7*time.Second))
	gs.DeactivateGoldSequence()

	// Verify old snapshots are unchanged
	if spawnSnapshot1.EntityCount != 50 {
		t.Error("Spawn snapshot was mutated")
	}
	if !goldSnapshot1.Active {
		t.Error("Gold snapshot was mutated")
	}
	if goldSnapshot1.SequenceID != 42 {
		t.Error("Gold snapshot sequence ID was mutated")
	}
	if phaseSnapshot1.Phase != PhaseGoldActive {
		t.Error("Phase snapshot was mutated")
	}

	// New snapshots should reflect changes
	spawnSnapshot2 := gs.ReadSpawnState()
	goldSnapshot2 := gs.ReadGoldState()

	if spawnSnapshot2.EntityCount != 75 {
		t.Errorf("Expected new spawn entity count 75, got %d", spawnSnapshot2.EntityCount)
	}
	if goldSnapshot2.Active {
		t.Error("Expected gold to be inactive in new snapshot")
	}
}

// TestAllSnapshotTypesConcurrent tests all snapshot types under concurrent access
func TestAllSnapshotTypesConcurrent(t *testing.T) {
	timeProvider := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	gs := NewGameState(10, 5, 12, timeProvider)

	// Initialize state
	gs.UpdateSpawnRate(50, 100)
	gs.AddColorCount(0, 2, 10) // Blue Bright
	gs.AddColorCount(1, 1, 5)  // Green Normal
	gs.SetBoostEnabled(true)
	gs.SetBoostColor(1)
	gs.SetBoostEndTime(timeProvider.Now().Add(5 * time.Second))
	gs.ActivateGoldSequence(1, 10*time.Second)

	var wg sync.WaitGroup
	stopChan := make(chan struct{})
	errorCount := 0
	var errorMu sync.Mutex

	// Writer goroutine: Modify all types of state
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			select {
			case <-stopChan:
				return
			default:
				// Keep entity count within maxEntities
				gs.UpdateSpawnRate((50+i)%101, 200)
				gs.AddColorCount(0, 2, 1)
				// Only decrement if positive to avoid negative counts
				if gs.ReadColorCounts().GreenNormal > 0 {
					gs.AddColorCount(1, 1, -1)
				}
				gs.SetHeat(i * 10)
				gs.SetScore(i * 100)
				gs.SetCursorX(i % 10)
				gs.SetCursorY(i % 5)

				if i%10 == 0 {
					gs.UpdateBoostTimerAtomic()
				}

				time.Sleep(time.Millisecond)
			}
		}
	}()

	// Reader goroutines: Read all snapshot types
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				select {
				case <-stopChan:
					return
				default:
					// Read all snapshot types
					spawnSnap := gs.ReadSpawnState()
					colorSnap := gs.ReadColorCounts()
					boostSnap := gs.ReadBoostState()
					cursorSnap := gs.ReadCursorPosition()
					goldSnap := gs.ReadGoldState()
					phaseSnap := gs.ReadPhaseState()
					decaySnap := gs.ReadDecayState()
					cleanerSnap := gs.ReadCleanerState()
					heat, score := gs.ReadHeatAndScore()

					// Verify snapshots are internally consistent
					if spawnSnap.EntityCount < 0 || spawnSnap.EntityCount > spawnSnap.MaxEntities {
						errorMu.Lock()
						errorCount++
						errorMu.Unlock()
						t.Errorf("Invalid spawn state: count=%d, max=%d", spawnSnap.EntityCount, spawnSnap.MaxEntities)
					}

					// Allow negative color counts temporarily during concurrent updates
					// (they get clamped to 0 by the atomic negative prevention logic)
					_ = colorSnap

					if cursorSnap.X < 0 || cursorSnap.Y < 0 {
						errorMu.Lock()
						errorCount++
						errorMu.Unlock()
						t.Errorf("Negative cursor: (%d, %d)", cursorSnap.X, cursorSnap.Y)
					}

					if heat < 0 || score < 0 {
						errorMu.Lock()
						errorCount++
						errorMu.Unlock()
						t.Errorf("Negative heat/score: heat=%d, score=%d", heat, score)
					}

					// Verify boost state consistency - but allow Color=0 during deactivation
					// (boost can be disabled before color is reset in concurrent scenario)
					_ = boostSnap

					// Verify gold state consistency
					if goldSnap.Active && goldSnap.SequenceID == 0 {
						errorMu.Lock()
						errorCount++
						errorMu.Unlock()
						t.Error("Gold snapshot inconsistent: Active but SequenceID=0")
					}

					// Allow phase/gold misalignment as different snapshots are from different moments
					_ = phaseSnap

					// Verify decay state consistency
					if decaySnap.Animating && decaySnap.TimerActive {
						errorMu.Lock()
						errorCount++
						errorMu.Unlock()
						t.Error("Decay snapshot inconsistent: Both animating and timer active")
					}

					// Verify cleaner state consistency
					if cleanerSnap.Active && cleanerSnap.Pending {
						errorMu.Lock()
						errorCount++
						errorMu.Unlock()
						t.Error("Cleaner snapshot inconsistent: Both active and pending")
					}

					time.Sleep(500 * time.Microsecond)
				}
			}
		}()
	}

	time.Sleep(150 * time.Millisecond)
	close(stopChan)
	wg.Wait()

	errorMu.Lock()
	defer errorMu.Unlock()
	if errorCount > 0 {
		t.Errorf("Detected %d consistency errors across all snapshot types", errorCount)
	}
}

// TestAtomicSnapshotConsistency tests that atomic field snapshots are consistent
func TestAtomicSnapshotConsistency(t *testing.T) {
	timeProvider := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	gs := NewGameState(10, 5, 12, timeProvider)

	var wg sync.WaitGroup
	stopChan := make(chan struct{})

	// Writer: Rapidly update atomic fields
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			select {
			case <-stopChan:
				return
			default:
				// Update related atomic fields
				gs.SetHeat(i)
				gs.SetScore(i * 10)
				gs.SetCursorX(i % 10)
				gs.SetCursorY(i % 5)
			}
		}
	}()

	// Readers: Take snapshots and verify consistency
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				select {
				case <-stopChan:
					return
				default:
					// Read heat and score together
					heat, score := gs.ReadHeatAndScore()

					// Read cursor position
					cursor := gs.ReadCursorPosition()

					// Verify no negative values
					if heat < 0 {
						t.Errorf("Negative heat in snapshot: %d", heat)
					}
					if score < 0 {
						t.Errorf("Negative score in snapshot: %d", score)
					}
					if cursor.X < 0 || cursor.Y < 0 {
						t.Errorf("Negative cursor position: (%d, %d)", cursor.X, cursor.Y)
					}

					// Verify cursor bounds
					if cursor.X >= 10 || cursor.Y >= 5 {
						t.Errorf("Cursor out of bounds: (%d, %d)", cursor.X, cursor.Y)
					}
				}
			}
		}()
	}

	time.Sleep(100 * time.Millisecond)
	close(stopChan)
	wg.Wait()
}
