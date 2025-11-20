package systems

// Race condition tests for GameState snapshot consistency.
// See also:
//   - cleaner_race_test.go: Cleaner system race conditions
//   - boost_race_test.go: Boost/heat system race conditions
//   - race_content_test.go: Content system race conditions
//   - race_counters_test.go: Color counter race conditions

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestSnapshotConsistencyUnderRapidChanges tests that GameState snapshots remain consistent
// even when state is changing rapidly across multiple goroutines
func TestSnapshotConsistencyUnderRapidChanges(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	var wg sync.WaitGroup
	stopChan := make(chan struct{})
	var inconsistencyCount atomic.Int32

	// Writer 1: Rapidly update spawn state
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			select {
			case <-stopChan:
				return
			default:
				ctx.State.UpdateSpawnRate(i%100, 100)
				time.Sleep(time.Millisecond)
			}
		}
	}()

	// Writer 2: Rapidly update atomic state (heat, score, cursor)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			select {
			case <-stopChan:
				return
			default:
				ctx.State.SetHeat(i % 80)
				ctx.State.SetScore(i * 10)
				ctx.State.SetCursorX(i % 80)
				ctx.State.SetCursorY(i % 24)
				time.Sleep(500 * time.Microsecond)
			}
		}
	}()

	// Writer 3: Rapidly update color counters
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			select {
			case <-stopChan:
				return
			default:
				ctx.State.AddColorCount(0, 2, 1) // Blue Bright
				// Only decrement if positive to avoid negative counts
				if ctx.State.ReadColorCounts().GreenNormal > 0 {
					ctx.State.AddColorCount(1, 1, -1) // Green Normal
				}
				time.Sleep(time.Millisecond)
			}
		}
	}()

	// Reader goroutines: Take snapshots and verify consistency
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				select {
				case <-stopChan:
					return
				default:
					// Take all snapshot types
					spawnSnap := ctx.State.ReadSpawnState()
					colorSnap := ctx.State.ReadColorCounts()
					cursorSnap := ctx.State.ReadCursorPosition()
					heat, score := ctx.State.ReadHeatAndScore()

					// Verify spawn snapshot internal consistency
					expectedDensity := float64(spawnSnap.EntityCount) / float64(spawnSnap.MaxEntities)
					if spawnSnap.ScreenDensity != expectedDensity {
						inconsistencyCount.Add(1)
						t.Errorf("Reader %d: Spawn snapshot inconsistent - density=%f, expected=%f",
							readerID, spawnSnap.ScreenDensity, expectedDensity)
					}

					// Verify no negative values
					if colorSnap.BlueBright < 0 || colorSnap.GreenNormal < 0 {
						inconsistencyCount.Add(1)
						t.Errorf("Reader %d: Color counters negative", readerID)
					}

					if cursorSnap.X < 0 || cursorSnap.Y < 0 {
						inconsistencyCount.Add(1)
						t.Errorf("Reader %d: Cursor negative: (%d, %d)", readerID, cursorSnap.X, cursorSnap.Y)
					}

					if heat < 0 || score < 0 {
						inconsistencyCount.Add(1)
						t.Errorf("Reader %d: Heat or score negative: heat=%d, score=%d", readerID, heat, score)
					}

					// Verify cursor bounds
					if cursorSnap.X >= 80 || cursorSnap.Y >= 24 {
						inconsistencyCount.Add(1)
						t.Errorf("Reader %d: Cursor out of bounds: (%d, %d)", readerID, cursorSnap.X, cursorSnap.Y)
					}

					time.Sleep(500 * time.Microsecond)
				}
			}
		}(i)
	}

	// Run test
	time.Sleep(200 * time.Millisecond)
	close(stopChan)
	wg.Wait()

	if inconsistencyCount.Load() > 0 {
		t.Errorf("Detected %d snapshot inconsistencies", inconsistencyCount.Load())
	}
}

// TestSnapshotImmutabilityWithSystemUpdates tests that snapshots remain immutable
// even while systems are actively modifying GameState
func TestSnapshotImmutabilityWithSystemUpdates(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	// Set initial state
	ctx.State.UpdateSpawnRate(50, 100)
	ctx.State.ActivateGoldSequence(42, 10*time.Second)
	ctx.State.SetHeat(50)
	ctx.State.SetScore(1000)

	// Take initial snapshots
	initialSpawn := ctx.State.ReadSpawnState()
	initialGold := ctx.State.ReadGoldState()
	initialHeat, initialScore := ctx.State.ReadHeatAndScore()

	// Verify initial values
	if initialSpawn.EntityCount != 50 {
		t.Fatalf("Expected initial entity count 50, got %d", initialSpawn.EntityCount)
	}
	if !initialGold.Active {
		t.Fatal("Expected gold to be initially active")
	}
	if initialHeat != 50 || initialScore != 1000 {
		t.Fatalf("Expected heat=50, score=1000, got heat=%d, score=%d", initialHeat, initialScore)
	}

	var wg sync.WaitGroup
	stopChan := make(chan struct{})

	// Rapidly modify state in multiple goroutines
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			select {
			case <-stopChan:
				return
			default:
				ctx.State.UpdateSpawnRate(i, 100)
				ctx.State.SetHeat(i * 10)
				ctx.State.SetScore(i * 100)
				time.Sleep(time.Millisecond)
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			select {
			case <-stopChan:
				return
			default:
				if i == 5 {
					ctx.State.DeactivateGoldSequence()
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	// Let modifications happen
	time.Sleep(100 * time.Millisecond)
	close(stopChan)
	wg.Wait()

	// Verify original snapshots are unchanged
	if initialSpawn.EntityCount != 50 {
		t.Errorf("Initial spawn snapshot was mutated: expected 50, got %d", initialSpawn.EntityCount)
	}
	if !initialGold.Active {
		t.Error("Initial gold snapshot was mutated: expected active=true")
	}
	if initialGold.SequenceID != 42 {
		t.Errorf("Initial gold snapshot sequence ID mutated: expected 42, got %d", initialGold.SequenceID)
	}
	if initialHeat != 50 {
		t.Errorf("Initial heat mutated: expected 50, got %d", initialHeat)
	}
	if initialScore != 1000 {
		t.Errorf("Initial score mutated: expected 1000, got %d", initialScore)
	}

	// Verify new snapshots show changed state
	newSpawn := ctx.State.ReadSpawnState()
	newGold := ctx.State.ReadGoldState()

	if newSpawn.EntityCount == 50 {
		t.Error("New spawn snapshot didn't reflect changes")
	}
	if newGold.Active {
		t.Error("New gold snapshot should show inactive state")
	}
}

// TestNoPartialSnapshotReads tests that snapshots never show partial state updates
// This verifies that mutex-protected snapshots always capture consistent multi-field state
func TestNoPartialSnapshotReads(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	// Initialize with known state to avoid initial inconsistency
	ctx.State.UpdateSpawnRate(50, 100)

	var wg sync.WaitGroup
	stopChan := make(chan struct{})
	var partialReadCount atomic.Int32

	// Writer: Update related fields together
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 1; i <= 300; i++ { // Start from 1 to avoid zero density
			select {
			case <-stopChan:
				return
			default:
				// UpdateSpawnRate should atomically update multiple related fields
				ctx.State.UpdateSpawnRate(i, 100)
				time.Sleep(time.Millisecond)
			}
		}
	}()

	// Readers: Verify snapshots are never partial
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 150; j++ {
				select {
				case <-stopChan:
					return
				default:
					snapshot := ctx.State.ReadSpawnState()

					// Verify internal consistency - all fields should be from same update
					expectedDensity := float64(snapshot.EntityCount) / float64(snapshot.MaxEntities)
					if snapshot.ScreenDensity != expectedDensity {
						partialReadCount.Add(1)
						t.Errorf("Partial read detected: density=%f, expected=%f (count=%d, max=%d)",
							snapshot.ScreenDensity, expectedDensity, snapshot.EntityCount, snapshot.MaxEntities)
					}

					// Verify rate multiplier matches density
					var expectedRate float64
					if snapshot.ScreenDensity < 0.3 {
						expectedRate = 2.0
					} else if snapshot.ScreenDensity > 0.7 {
						expectedRate = 0.5
					} else {
						expectedRate = 1.0
					}

					if snapshot.RateMultiplier != expectedRate {
						partialReadCount.Add(1)
						t.Errorf("Partial read detected: rate=%f, expected=%f (density=%f)",
							snapshot.RateMultiplier, expectedRate, snapshot.ScreenDensity)
					}

					time.Sleep(300 * time.Microsecond)
				}
			}
		}()
	}

	// Run test
	time.Sleep(200 * time.Millisecond)
	close(stopChan)
	wg.Wait()

	if partialReadCount.Load() > 0 {
		t.Errorf("Detected %d partial snapshot reads", partialReadCount.Load())
	}
}

// TestPhaseSnapshotConsistency tests that phase snapshots remain consistent
// during rapid phase transitions (simulating gold→decay→normal cycle)
func TestPhaseSnapshotConsistency(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	var wg sync.WaitGroup
	stopChan := make(chan struct{})
	var inconsistencyCount atomic.Int32

	// Writer: Simulate phase transitions
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 1; i <= 50; i++ { // Start from 1 to ensure non-zero sequence IDs
			select {
			case <-stopChan:
				return
			default:
				// Cycle through phases
				ctx.State.ActivateGoldSequence(i, 10*time.Second)
				time.Sleep(5 * time.Millisecond)
				ctx.State.DeactivateGoldSequence()
				time.Sleep(5 * time.Millisecond)
				ctx.State.StartDecayTimer(80, 60, 50)
				time.Sleep(5 * time.Millisecond)
			}
		}
	}()

	// Readers: Verify phase and related state consistency
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				select {
				case <-stopChan:
					return
				default:
					goldSnap := ctx.State.ReadGoldState()
					decaySnap := ctx.State.ReadDecayState(0)

					// Verify phase-state consistency within each snapshot
					// Note: Different snapshots might be from different moments,
					// but each individual snapshot should be internally consistent

					// Gold snapshot should be internally consistent
					if goldSnap.Active && goldSnap.SequenceID == 0 {
						inconsistencyCount.Add(1)
						t.Error("Gold snapshot inconsistent: Active but SequenceID=0")
					}

					// Decay snapshot should be internally consistent
					if decaySnap.Animating && decaySnap.TimerActive {
						inconsistencyCount.Add(1)
						t.Error("Decay snapshot inconsistent: Both animating and timer active")
					}

					time.Sleep(time.Millisecond)
				}
			}
		}()
	}

	// Run test
	time.Sleep(200 * time.Millisecond)
	close(stopChan)
	wg.Wait()

	if inconsistencyCount.Load() > 0 {
		t.Errorf("Detected %d phase snapshot inconsistencies", inconsistencyCount.Load())
	}
}

// TestMultiSnapshotAtomicity tests taking multiple snapshots in rapid succession
// Verifies that even if state changes between snapshots, each snapshot is internally consistent
func TestMultiSnapshotAtomicity(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)

	// Initialize state
	ctx.State.UpdateSpawnRate(50, 100)
	ctx.State.AddColorCount(0, 2, 10) // Blue Bright
	ctx.State.SetHeat(50)
	ctx.State.SetScore(500)

	var wg sync.WaitGroup
	stopChan := make(chan struct{})
	var errorCount atomic.Int32

	// Rapid state modifier
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			select {
			case <-stopChan:
				return
			default:
				ctx.State.UpdateSpawnRate(i%100, 100)
				ctx.State.AddColorCount(0, 2, 1)
				ctx.State.SetHeat((i * 3) % 80)
				ctx.State.SetScore(i * 10)
				time.Sleep(500 * time.Microsecond)
			}
		}
	}()

	// Multiple snapshot readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				select {
				case <-stopChan:
					return
				default:
					// Take multiple snapshots in rapid succession
					spawn1 := ctx.State.ReadSpawnState()
					color1 := ctx.State.ReadColorCounts()
					heat1, score1 := ctx.State.ReadHeatAndScore()

					// Each snapshot should be internally valid
					if spawn1.EntityCount < 0 || spawn1.EntityCount > spawn1.MaxEntities {
						errorCount.Add(1)
					}

					if color1.BlueBright < 0 {
						errorCount.Add(1)
					}

					if heat1 < 0 || score1 < 0 {
						errorCount.Add(1)
					}

					// Verify spawn snapshot internal consistency
					expectedDensity := float64(spawn1.EntityCount) / float64(spawn1.MaxEntities)
					if spawn1.ScreenDensity != expectedDensity {
						errorCount.Add(1)
					}

					time.Sleep(time.Millisecond)
				}
			}
		}()
	}

	// Run test
	time.Sleep(150 * time.Millisecond)
	close(stopChan)
	wg.Wait()

	if errorCount.Load() > 0 {
		t.Errorf("Detected %d snapshot atomicity errors", errorCount.Load())
	}
}
