package engine

import (
	"sync"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/constants"
)

// Integration Tests: Deterministic clock testing for complete game cycles
// These tests use MockTimeProvider to precisely control time advancement and verify
// that all phase transitions happen deterministically without race conditions.

// TestCompleteGameCycle tests a full Gold→Decay→Normal cycle with deterministic time
func TestCompleteGameCycle(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	state := NewGameState(80, 24, 100, mockTime)

	// ===== PHASE 1: Normal → Gold Active =====
	if phase := state.GetPhase(); phase != PhaseNormal {
		t.Fatalf("Expected initial phase PhaseNormal, got %v", phase)
	}

	// Simulate gold spawn
	sequenceID := state.IncrementGoldSequenceID()
	state.ActivateGoldSequence(sequenceID, constants.GoldSequenceDuration)

	if phase := state.GetPhase(); phase != PhaseGoldActive {
		t.Fatalf("Expected PhaseGoldActive after activation, got %v", phase)
	}

	goldSnapshot := state.ReadGoldState()
	if !goldSnapshot.Active || goldSnapshot.SequenceID != sequenceID {
		t.Fatal("Gold sequence not properly activated")
	}

	// ===== PHASE 2: Gold Active → Decay Wait (timeout) =====
	// Simulate typing during gold - increase heat
	state.SetHeat(75) // 75 out of 94 max

	// Advance time to gold timeout
	mockTime.Advance(constants.GoldSequenceDuration + 100*time.Millisecond)

	if !state.IsGoldTimedOut() {
		t.Fatal("Gold should be timed out")
	}

	// Simulate clock scheduler transition: deactivate gold and start decay timer
	state.DeactivateGoldSequence()
	decayStartTime := mockTime.Now()
	state.StartDecayTimer(
		state.ScreenWidth,
		constants.HeatBarIndicatorWidth,
		constants.DecayIntervalBaseSeconds,
		constants.DecayIntervalRangeSeconds,
	)

	if phase := state.GetPhase(); phase != PhaseDecayWait {
		t.Fatalf("Expected PhaseDecayWait after decay timer start, got %v", phase)
	}

	// Verify decay timer is active and interval is correct for heat=75
	decaySnapshot := state.ReadDecayState()
	if !decaySnapshot.TimerActive {
		t.Fatal("Decay timer should be active")
	}

	// With heat=75 out of 94 max: heatPercentage = 75/94 ≈ 0.798
	// interval = 60 - 50*0.798 ≈ 20 seconds
	expectedInterval := 20.0 // approximate
	actualInterval := decaySnapshot.NextTime.Sub(decayStartTime).Seconds()
	if actualInterval < expectedInterval-5 || actualInterval > expectedInterval+5 {
		t.Errorf("Expected decay interval ~%.1f seconds (heat=75), got %.1f", expectedInterval, actualInterval)
	}

	// ===== PHASE 3: Decay Wait → Decay Animation =====
	// Advance time past decay interval
	mockTime.Advance(time.Duration(actualInterval+1) * time.Second)

	if !state.IsDecayReady() {
		t.Fatal("Decay should be ready")
	}

	// Simulate clock scheduler transition: start decay animation
	state.StartDecayAnimation()

	if phase := state.GetPhase(); phase != PhaseDecayAnimation {
		t.Fatalf("Expected PhaseDecayAnimation, got %v", phase)
	}

	decaySnapshot = state.ReadDecayState()
	if !decaySnapshot.Animating || decaySnapshot.TimerActive {
		t.Fatal("Decay animation should be running, timer should be inactive")
	}

	// ===== PHASE 4: Decay Animation → Normal =====
	// Simulate animation completion
	state.StopDecayAnimation()

	if phase := state.GetPhase(); phase != PhaseNormal {
		t.Fatalf("Expected PhaseNormal after animation stop, got %v", phase)
	}

	// Verify all state is clean
	decaySnapshot = state.ReadDecayState()
	goldSnapshot = state.ReadGoldState()

	if decaySnapshot.Animating || decaySnapshot.TimerActive {
		t.Error("Decay state should be clean")
	}
	if goldSnapshot.Active {
		t.Error("Gold state should be clean")
	}

	t.Logf("✓ Complete game cycle successful: Normal → Gold → DecayWait → DecayAnim → Normal")
}

// TestMultipleConsecutiveCycles tests multiple complete game cycles with varying heat
func TestMultipleConsecutiveCycles(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	state := NewGameState(80, 24, 100, mockTime)

	// Test 3 consecutive cycles with different heat levels
	heatLevels := []int{0, 47, 94} // 0%, 50%, 100% heat
	expectedIntervals := []float64{60.0, 35.0, 10.0}

	for cycle := 0; cycle < 3; cycle++ {
		t.Logf("--- Cycle %d: heat=%d ---", cycle+1, heatLevels[cycle])

		// 1. Activate gold
		sequenceID := state.IncrementGoldSequenceID()
		state.ActivateGoldSequence(sequenceID, constants.GoldSequenceDuration)

		if state.GetPhase() != PhaseGoldActive {
			t.Fatalf("Cycle %d: Expected PhaseGoldActive", cycle+1)
		}

		// 2. Set heat level for this cycle
		state.SetHeat(heatLevels[cycle])

		// 3. Advance time to gold timeout
		mockTime.Advance(constants.GoldSequenceDuration + 100*time.Millisecond)

		// 4. Deactivate gold and start decay timer
		state.DeactivateGoldSequence()
		decayStartTime := mockTime.Now()
		state.StartDecayTimer(
			state.ScreenWidth,
			constants.HeatBarIndicatorWidth,
			constants.DecayIntervalBaseSeconds,
			constants.DecayIntervalRangeSeconds,
		)

		if state.GetPhase() != PhaseDecayWait {
			t.Fatalf("Cycle %d: Expected PhaseDecayWait", cycle+1)
		}

		// 5. Verify decay interval matches heat level
		decaySnapshot := state.ReadDecayState()
		actualInterval := decaySnapshot.NextTime.Sub(decayStartTime).Seconds()
		expectedMin := expectedIntervals[cycle] - 2.0
		expectedMax := expectedIntervals[cycle] + 2.0

		if actualInterval < expectedMin || actualInterval > expectedMax {
			t.Errorf("Cycle %d: Expected interval %.1f-%.1f seconds, got %.1f",
				cycle+1, expectedMin, expectedMax, actualInterval)
		}

		t.Logf("Cycle %d: heat=%d → interval=%.1fs (expected ~%.1fs)",
			cycle+1, heatLevels[cycle], actualInterval, expectedIntervals[cycle])

		// 6. Advance time past decay interval
		mockTime.Advance(time.Duration(actualInterval+1) * time.Second)

		// 7. Start decay animation
		state.StartDecayAnimation()

		if state.GetPhase() != PhaseDecayAnimation {
			t.Fatalf("Cycle %d: Expected PhaseDecayAnimation", cycle+1)
		}

		// 8. Stop decay animation (return to Normal)
		state.StopDecayAnimation()

		if state.GetPhase() != PhaseNormal {
			t.Fatalf("Cycle %d: Expected PhaseNormal", cycle+1)
		}
	}

	t.Logf("✓ All %d cycles completed successfully with correct intervals", len(heatLevels))
}

// TestGoldCompletionBeforeTimeout tests gold completion (not timeout) transition
func TestGoldCompletionBeforeTimeout(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	state := NewGameState(80, 24, 100, mockTime)

	// Activate gold
	sequenceID := state.IncrementGoldSequenceID()
	state.ActivateGoldSequence(sequenceID, constants.GoldSequenceDuration)

	// Simulate typing during gold
	state.SetHeat(50)

	// Advance time but NOT to timeout (only 3 seconds out of 10)
	mockTime.Advance(3 * time.Second)

	// Gold should NOT be timed out
	if state.IsGoldTimedOut() {
		t.Fatal("Gold should not be timed out yet")
	}

	// Simulate user completing the gold sequence early
	// (In real game, ScoreSystem would call this)
	state.DeactivateGoldSequence()
	decayStartTime := mockTime.Now()
	state.StartDecayTimer(
		state.ScreenWidth,
		constants.HeatBarIndicatorWidth,
		constants.DecayIntervalBaseSeconds,
		constants.DecayIntervalRangeSeconds,
	)

	// Verify transition to DecayWait
	if state.GetPhase() != PhaseDecayWait {
		t.Fatalf("Expected PhaseDecayWait after gold completion, got %v", state.GetPhase())
	}

	// Verify decay timer uses heat at completion time (50)
	decaySnapshot := state.ReadDecayState()
	actualInterval := decaySnapshot.NextTime.Sub(decayStartTime).Seconds()

	// With heat=50 out of 94: heatPercentage ≈ 0.53
	// interval = 60 - 50*0.53 ≈ 33 seconds
	if actualInterval < 30 || actualInterval > 36 {
		t.Errorf("Expected interval ~33 seconds (heat=50), got %.1f", actualInterval)
	}

	t.Logf("✓ Gold completion (early) → decay timer correctly uses heat=50 (%.1fs interval)", actualInterval)
}

// TestHeatChangesDuringGoldDontAffectPreviousDecayTimer tests that heat changes
// during a gold sequence don't affect an already-calculated decay timer
func TestHeatChangesDuringGoldDontAffectPreviousDecayTimer(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	state := NewGameState(80, 24, 100, mockTime)

	// This test is not directly applicable since decay timer is only calculated
	// AFTER gold ends. But we can test that heat changes DURING DecayWait don't
	// affect the already-calculated timer.

	// 1. Start with low heat
	state.SetHeat(10)

	// 2. Activate and timeout gold
	sequenceID := state.IncrementGoldSequenceID()
	state.ActivateGoldSequence(sequenceID, constants.GoldSequenceDuration)
	mockTime.Advance(constants.GoldSequenceDuration + 100*time.Millisecond)

	// 3. Start decay timer (will use heat=10)
	state.DeactivateGoldSequence()
	decayStartTime := mockTime.Now()
	state.StartDecayTimer(
		state.ScreenWidth,
		constants.HeatBarIndicatorWidth,
		constants.DecayIntervalBaseSeconds,
		constants.DecayIntervalRangeSeconds,
	)

	decaySnapshot := state.ReadDecayState()
	originalNextTime := decaySnapshot.NextTime
	originalInterval := originalNextTime.Sub(decayStartTime).Seconds()

	// With heat=10 out of 94: interval should be ~55 seconds
	if originalInterval < 50 || originalInterval > 60 {
		t.Errorf("Expected interval ~55 seconds (heat=10), got %.1f", originalInterval)
	}

	// 4. CHANGE heat during DecayWait phase (simulate typing on other sequences)
	state.SetHeat(90)

	// 5. Verify decay timer NextTime did NOT change
	decaySnapshot = state.ReadDecayState()
	if !decaySnapshot.NextTime.Equal(originalNextTime) {
		t.Errorf("Decay timer NextTime changed! Original: %v, New: %v",
			originalNextTime, decaySnapshot.NextTime)
	}

	t.Logf("✓ Heat change during DecayWait did not affect timer (%.1fs interval preserved)", originalInterval)
}

// TestRapidPhaseTransitions tests very fast phase transitions
func TestRapidPhaseTransitions(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	state := NewGameState(80, 24, 100, mockTime)

	// Test 10 rapid cycles (advancing time in large chunks)
	for i := 0; i < 10; i++ {
		// Activate gold
		sequenceID := state.IncrementGoldSequenceID()
		state.ActivateGoldSequence(sequenceID, constants.GoldSequenceDuration)

		// Immediately timeout (advance gold duration)
		mockTime.Advance(constants.GoldSequenceDuration + 50*time.Millisecond)

		// Immediately start decay timer
		state.DeactivateGoldSequence()
		state.StartDecayTimer(
			state.ScreenWidth,
			constants.HeatBarIndicatorWidth,
			constants.DecayIntervalBaseSeconds,
			constants.DecayIntervalRangeSeconds,
		)

		// Get decay interval
		decaySnapshot := state.ReadDecayState()
		interval := decaySnapshot.TimeUntil

		// Immediately trigger decay (advance decay interval)
		mockTime.Advance(time.Duration(interval+1) * time.Second)

		// Immediately start and stop animation
		state.StartDecayAnimation()
		state.StopDecayAnimation()

		// Verify we're back in Normal
		if state.GetPhase() != PhaseNormal {
			t.Fatalf("Cycle %d: Expected PhaseNormal, got %v", i+1, state.GetPhase())
		}
	}

	t.Logf("✓ Completed 10 rapid phase transition cycles successfully")
}

// TestConcurrentPhaseReadsDuringTransitions tests concurrent phase reads during transitions
func TestConcurrentPhaseReadsDuringTransitions(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	state := NewGameState(80, 24, 100, mockTime)

	const numReaders = 20
	const cyclesPerReader = 50

	var wg sync.WaitGroup
	wg.Add(numReaders)

	// Start concurrent readers
	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < cyclesPerReader; j++ {
				// Read phase state
				_ = state.GetPhase()
				_ = state.ReadPhaseState()
				_ = state.GetPhaseDuration()

				// Read gold state
				_ = state.GetGoldActive()
				_ = state.IsGoldTimedOut()
				_ = state.ReadGoldState()

				// Read decay state
				_ = state.GetDecayTimerActive()
				_ = state.GetDecayAnimating()
				_ = state.IsDecayReady()
				_ = state.ReadDecayState()

				// Read heat
				_ = state.GetHeat()
			}
		}()
	}

	// Perform 100 phase transitions while readers are active
	for i := 0; i < 100; i++ {
		// Vary heat each cycle
		state.SetHeat(i % 95)

		// Gold active
		sequenceID := state.IncrementGoldSequenceID()
		state.ActivateGoldSequence(sequenceID, constants.GoldSequenceDuration)

		// Decay wait
		state.DeactivateGoldSequence()
		state.StartDecayTimer(
			state.ScreenWidth,
			constants.HeatBarIndicatorWidth,
			constants.DecayIntervalBaseSeconds,
			constants.DecayIntervalRangeSeconds,
		)

		// Decay animation
		state.StartDecayAnimation()

		// Normal
		state.StopDecayAnimation()
	}

	// Wait for all readers to finish
	wg.Wait()

	// Verify final state is consistent
	if state.GetPhase() != PhaseNormal {
		t.Fatalf("Expected final phase PhaseNormal, got %v", state.GetPhase())
	}

	t.Logf("✓ Concurrent phase access test passed: %d readers × %d cycles with 100 transitions",
		numReaders, cyclesPerReader)
}

// TestPhaseTimestampConsistency tests that phase start times are consistent
func TestPhaseTimestampConsistency(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	state := NewGameState(80, 24, 100, mockTime)

	// Activate gold
	t1 := mockTime.Now()
	sequenceID := state.IncrementGoldSequenceID()
	state.ActivateGoldSequence(sequenceID, constants.GoldSequenceDuration)

	goldSnapshot := state.ReadGoldState()
	if !goldSnapshot.StartTime.Equal(t1) {
		t.Errorf("Gold start time mismatch: expected %v, got %v", t1, goldSnapshot.StartTime)
	}

	// Advance time
	mockTime.Advance(5 * time.Second)

	// Start decay timer
	t2 := mockTime.Now()
	state.DeactivateGoldSequence()
	state.StartDecayTimer(
		state.ScreenWidth,
		constants.HeatBarIndicatorWidth,
		constants.DecayIntervalBaseSeconds,
		constants.DecayIntervalRangeSeconds,
	)

	phaseSnapshot := state.ReadPhaseState()
	if !phaseSnapshot.StartTime.Equal(t2) {
		t.Errorf("DecayWait phase start time mismatch: expected %v, got %v", t2, phaseSnapshot.StartTime)
	}

	// Advance time
	mockTime.Advance(30 * time.Second)

	// Start decay animation
	t3 := mockTime.Now()
	state.StartDecayAnimation()

	decaySnapshot := state.ReadDecayState()
	if !decaySnapshot.StartTime.Equal(t3) {
		t.Errorf("DecayAnimation start time mismatch: expected %v, got %v", t3, decaySnapshot.StartTime)
	}

	t.Logf("✓ Phase timestamp consistency verified across all transitions")
}

// TestPhaseDurationCalculation tests that phase duration is calculated correctly
func TestPhaseDurationCalculation(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	state := NewGameState(80, 24, 100, mockTime)

	// Activate gold
	sequenceID := state.IncrementGoldSequenceID()
	state.ActivateGoldSequence(sequenceID, constants.GoldSequenceDuration)

	// Advance time by 3 seconds
	mockTime.Advance(3 * time.Second)

	// Check phase duration
	duration := state.GetPhaseDuration()
	if duration != 3*time.Second {
		t.Errorf("Expected phase duration 3s, got %v", duration)
	}

	// Check gold remaining time
	goldSnapshot := state.ReadGoldState()
	if goldSnapshot.Remaining != 7*time.Second {
		t.Errorf("Expected gold remaining 7s, got %v", goldSnapshot.Remaining)
	}

	// Advance more time
	mockTime.Advance(2 * time.Second)

	// Check updated duration
	duration = state.GetPhaseDuration()
	if duration != 5*time.Second {
		t.Errorf("Expected phase duration 5s, got %v", duration)
	}

	goldSnapshot = state.ReadGoldState()
	if goldSnapshot.Remaining != 5*time.Second {
		t.Errorf("Expected gold remaining 5s, got %v", goldSnapshot.Remaining)
	}

	t.Logf("✓ Phase duration calculations are accurate")
}

// TestDecayIntervalBoundaryConditions tests decay interval at heat boundaries
func TestDecayIntervalBoundaryConditions(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	state := NewGameState(80, 24, 100, mockTime)

	testCases := []struct {
		name            string
		heat            int
		expectedMin     float64
		expectedMax     float64
	}{
		{"Minimum heat (0)", 0, 58.0, 62.0},
		{"Just above zero", 1, 57.0, 61.0},
		{"One below max", 93, 9.5, 11.0},
		{"Maximum heat (94)", 94, 9.0, 11.0},
		{"Above max (clamped)", 100, 9.0, 11.0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			state.SetHeat(tc.heat)

			startTime := mockTime.Now()
			state.StartDecayTimer(
				100, // screenWidth
				constants.HeatBarIndicatorWidth,
				constants.DecayIntervalBaseSeconds,
				constants.DecayIntervalRangeSeconds,
			)

			nextDecay := state.GetDecayNextTime()
			interval := nextDecay.Sub(startTime).Seconds()

			if interval < tc.expectedMin || interval > tc.expectedMax {
				t.Errorf("Heat=%d: expected interval %.1f-%.1f seconds, got %.1f",
					tc.heat, tc.expectedMin, tc.expectedMax, interval)
			}

			t.Logf("Heat=%d → interval=%.1fs ✓", tc.heat, interval)

			// Reset state for next test
			state.StopDecayAnimation()
		})
	}
}

// TestStateSnapshotConsistency tests that snapshot methods return consistent data
func TestStateSnapshotConsistency(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	state := NewGameState(80, 24, 100, mockTime)

	// Activate gold
	sequenceID := state.IncrementGoldSequenceID()
	state.ActivateGoldSequence(sequenceID, constants.GoldSequenceDuration)

	// Take multiple snapshots in quick succession
	snapshot1 := state.ReadGoldState()
	snapshot2 := state.ReadGoldState()
	snapshot3 := state.ReadPhaseState()

	// Verify snapshots are consistent
	if snapshot1.Active != snapshot2.Active {
		t.Error("Gold snapshots have inconsistent Active state")
	}
	if snapshot1.SequenceID != snapshot2.SequenceID {
		t.Error("Gold snapshots have inconsistent SequenceID")
	}
	if !snapshot1.StartTime.Equal(snapshot2.StartTime) {
		t.Error("Gold snapshots have inconsistent StartTime")
	}
	if snapshot3.Phase != PhaseGoldActive {
		t.Errorf("Phase snapshot shows %v instead of PhaseGoldActive", snapshot3.Phase)
	}

	t.Logf("✓ State snapshot consistency verified")
}

// TestGoldSequenceIDIncrement tests that gold sequence IDs increment correctly
func TestGoldSequenceIDIncrement(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	state := NewGameState(80, 24, 100, mockTime)

	expectedIDs := []int{1, 2, 3, 4, 5}
	actualIDs := make([]int, 0, 5)

	for i := 0; i < 5; i++ {
		id := state.IncrementGoldSequenceID()
		actualIDs = append(actualIDs, id)

		// Activate and immediately deactivate
		state.ActivateGoldSequence(id, constants.GoldSequenceDuration)
		state.DeactivateGoldSequence()
	}

	for i := 0; i < 5; i++ {
		if actualIDs[i] != expectedIDs[i] {
			t.Errorf("Expected ID %d at index %d, got %d", expectedIDs[i], i, actualIDs[i])
		}
	}

	t.Logf("✓ Gold sequence IDs increment correctly: %v", actualIDs)
}
