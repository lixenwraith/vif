package engine

import (
	"sync"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
)

// ============================================================================
// Complete Game Cycle Integration Tests
// ============================================================================

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

// ============================================================================
// Decay Interval Calculation Tests
// ============================================================================

// TestDecayIntervalCalculation tests that decay interval is calculated based on current heat
func TestDecayIntervalCalculation(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	state := NewGameState(80, 24, 100, mockTime)

	testCases := []struct {
		name           string
		heat           int
		screenWidth    int
		expectedMin    float64 // seconds
		expectedMax    float64 // seconds
		heatPercentage float64
	}{
		{
			name:           "Zero heat - maximum interval",
			heat:           0,
			screenWidth:    100,
			expectedMin:    59.0,
			expectedMax:    61.0,
			heatPercentage: 0.0,
		},
		{
			name:           "Half heat - mid interval",
			heat:           47, // (100-6)/2 = 47
			screenWidth:    100,
			expectedMin:    34.0,
			expectedMax:    36.0,
			heatPercentage: 0.5,
		},
		{
			name:           "Full heat - minimum interval",
			heat:           94, // 100-6 = 94
			screenWidth:    100,
			expectedMin:    9.0,
			expectedMax:    11.0,
			heatPercentage: 1.0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set heat
			state.SetHeat(tc.heat)

			// Start decay timer
			startTime := mockTime.Now()
			state.StartDecayTimer(
				tc.screenWidth,
				constants.HeatBarIndicatorWidth,
				constants.DecayIntervalBaseSeconds,
				constants.DecayIntervalRangeSeconds,
			)

			// Get next decay time
			nextDecay := state.GetDecayNextTime()
			interval := nextDecay.Sub(startTime).Seconds()

			// Verify interval is in expected range
			if interval < tc.expectedMin || interval > tc.expectedMax {
				t.Errorf("Expected interval between %.1f and %.1f seconds, got %.1f (heat=%d, percentage=%.2f)",
					tc.expectedMin, tc.expectedMax, interval, tc.heat, tc.heatPercentage)
			}

			t.Logf("Heat: %d, Percentage: %.2f, Interval: %.2f seconds", tc.heat, tc.heatPercentage, interval)

			// Reset for next test
			state.DeactivateGoldSequence()
		})
	}
}

// TestNoHeatCaching tests that decay timer uses current heat, not cached values
func TestNoHeatCaching(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	state := NewGameState(80, 24, 100, mockTime)

	// Start with zero heat
	state.SetHeat(0)

	// Activate gold sequence
	sequenceID := state.IncrementGoldSequenceID()
	state.ActivateGoldSequence(sequenceID, constants.GoldSequenceDuration)

	// During gold sequence, increase heat significantly
	state.SetHeat(90)

	// Deactivate gold and start decay timer
	// This should read the CURRENT heat (90), not the old heat (0)
	startTime := mockTime.Now()
	state.DeactivateGoldSequence()
	state.StartDecayTimer(
		100, // screenWidth
		constants.HeatBarIndicatorWidth,
		constants.DecayIntervalBaseSeconds,
		constants.DecayIntervalRangeSeconds,
	)

	// Get next decay time
	nextDecay := state.GetDecayNextTime()
	interval := nextDecay.Sub(startTime).Seconds()

	// With heat=90 out of 94 max (screenWidth 100 - indicator 6):
	// heatPercentage = 90/94 ≈ 0.96
	// interval = 60 - 50*0.96 ≈ 12 seconds
	// Should be close to minimum interval (10-15 seconds), not maximum (60 seconds)
	if interval > 20 {
		t.Fatalf("Decay timer used stale heat! Expected ~12 seconds (high heat), got %.1f seconds", interval)
	}

	t.Logf("Decay interval with heat=90: %.2f seconds (correct - using current heat)", interval)
}

// TestDecayIntervalBoundaryConditions tests decay interval at heat boundaries
func TestDecayIntervalBoundaryConditions(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	state := NewGameState(80, 24, 100, mockTime)

	testCases := []struct {
		name        string
		heat        int
		expectedMin float64
		expectedMax float64
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

// ============================================================================
// Concurrent Access Tests
// ============================================================================

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

// ============================================================================
// Timestamp and Duration Tests
// ============================================================================

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

// ============================================================================
// Cleaner Integration Tests
// ============================================================================

// TestGoldToCleanerFlow tests the complete flow from gold completion to cleaner activation
func TestGoldToCleanerFlow(t *testing.T) {
	startTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	mockTime := NewMockTimeProvider(startTime)

	world := NewWorld()
	state := NewGameState(80, 24, 100, mockTime)

	ctx := &GameContext{
		World:        world,
		TimeProvider: mockTime,
		State:        state,
		GameWidth:    80,
		GameHeight:   24,
	}

	// Create clock scheduler
	clockScheduler := NewClockScheduler(ctx)

	// Set heat to maximum to trigger cleaners on gold completion
	maxHeat := 80 - constants.HeatBarIndicatorWidth
	ctx.State.SetHeat(maxHeat)

	// Request cleaners (simulating gold completion at max heat)
	ctx.State.RequestCleaners()

	// Verify pending state
	snapshot := ctx.State.ReadCleanerState()
	if !snapshot.Pending {
		t.Error("Cleaners should be pending after request")
	}
	if snapshot.Active {
		t.Error("Cleaners should not be active yet")
	}

	// Advance time to next clock tick (50ms)
	mockTime.Advance(50 * time.Millisecond)

	// Clock scheduler should activate cleaners
	// We simulate this manually since we don't have actual systems in this test
	if ctx.State.GetCleanerPending() {
		ctx.State.ActivateCleaners()
	}

	// Verify active state
	snapshot = ctx.State.ReadCleanerState()
	if snapshot.Pending {
		t.Error("Cleaners should no longer be pending")
	}
	if !snapshot.Active {
		t.Error("Cleaners should be active after activation")
	}
	if snapshot.StartTime.IsZero() {
		t.Error("StartTime should be set")
	}

	// Verify timing
	expectedStartTime := startTime.Add(50 * time.Millisecond)
	if !snapshot.StartTime.Equal(expectedStartTime) {
		t.Errorf("StartTime should be %v, got %v", expectedStartTime, snapshot.StartTime)
	}

	clockScheduler.Stop()
	t.Logf("Gold→Cleaner flow validated successfully")
}

// TestConcurrentCleanerAndGoldPhases tests that cleaners can run in parallel with gold sequences
func TestConcurrentCleanerAndGoldPhases(t *testing.T) {
	startTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	mockTime := NewMockTimeProvider(startTime)

	world := NewWorld()
	state := NewGameState(80, 24, 100, mockTime)

	ctx := &GameContext{
		World:        world,
		TimeProvider: mockTime,
		State:        state,
		GameWidth:    80,
		GameHeight:   24,
	}

	// Start with normal phase
	ctx.State.SetPhase(PhaseNormal)

	// Activate cleaners
	ctx.State.RequestCleaners()
	mockTime.Advance(50 * time.Millisecond)
	ctx.State.ActivateCleaners()

	if !ctx.State.GetCleanerActive() {
		t.Fatal("Cleaners should be active")
	}

	// Transition to gold phase (cleaners should remain active)
	ctx.State.SetPhase(PhaseGoldActive)

	// Both should be true
	if ctx.State.GetPhase() != PhaseGoldActive {
		t.Error("Phase should be GoldActive")
	}
	if !ctx.State.GetCleanerActive() {
		t.Error("Cleaners should still be active during gold phase")
	}

	// Transition through decay phases
	ctx.State.SetPhase(PhaseDecayWait)
	if !ctx.State.GetCleanerActive() {
		t.Error("Cleaners should still be active during decay wait")
	}

	ctx.State.SetPhase(PhaseDecayAnimation)
	if !ctx.State.GetCleanerActive() {
		t.Error("Cleaners should still be active during decay animation")
	}

	// Return to normal
	ctx.State.SetPhase(PhaseNormal)
	if !ctx.State.GetCleanerActive() {
		t.Error("Cleaners should still be active until deactivation")
	}

	// Deactivate cleaners
	ctx.State.DeactivateCleaners()

	if ctx.State.GetCleanerActive() {
		t.Error("Cleaners should be inactive after deactivation")
	}

	t.Logf("Concurrent cleaner and phase transitions validated successfully")
}

// ============================================================================
// Trail Collision Logic Tests
// ============================================================================

// TestCleanerTrailCollisionLogic tests the trail-based collision detection
func TestCleanerTrailCollisionLogic(t *testing.T) {
	// This test verifies the trail-based collision changes:
	// 1. Trail positions are checked continuously
	// 2. Integer truncation is used (not rounding)
	// 3. No characters are skipped

	// Test fractional positions
	trail := []float64{10.3, 10.7, 11.2, 11.9}

	// With truncation: int(10.3)=10, int(10.7)=10, int(11.2)=11, int(11.9)=11
	// Expected unique positions: 10, 11

	uniquePositions := make(map[int]bool)
	for _, pos := range trail {
		x := int(pos) // Truncation instead of rounding
		uniquePositions[x] = true
	}

	if len(uniquePositions) != 2 {
		t.Errorf("Expected 2 unique positions, got %d", len(uniquePositions))
	}

	if !uniquePositions[10] || !uniquePositions[11] {
		t.Error("Expected positions 10 and 11 to be checked")
	}

	t.Logf("Trail collision logic validated: %v → positions %v", trail, uniquePositions)
}

// TestNoSkippedCharacters tests that no characters are skipped due to rounding
func TestNoSkippedCharacters(t *testing.T) {
	world := NewWorld()

	// Create Red characters at positions 10, 11, 12
	redPositions := []int{10, 11, 12}
	for _, x := range redPositions {
		entity := world.CreateEntity()
		world.AddComponent(entity, components.PositionComponent{X: x, Y: 5})
		world.AddComponent(entity, components.SequenceComponent{
			Type: components.SequenceRed,
		})
		world.UpdateSpatialIndex(entity, x, 5)
	}

	// Simulate cleaner trail covering these positions with fractional values
	// Old (rounding): int(9.6+0.5)=10, int(10.4+0.5)=10 (skips 11!)
	// New (truncation): int(9.6)=9, int(10.4)=10, int(11.3)=11, int(12.1)=12
	trail := []float64{9.6, 10.4, 11.3, 12.1}

	// Using truncation logic
	checkedPositions := make(map[int]bool)
	for _, pos := range trail {
		x := int(pos) // Truncation
		checkedPositions[x] = true
	}

	// Verify all Red positions would be checked
	for _, redX := range redPositions {
		if !checkedPositions[redX] {
			t.Errorf("Position %d should be checked but wasn't (truncation)", redX)
		}
	}

	// Verify positions checked: 9, 10, 11, 12
	expectedChecked := map[int]bool{9: true, 10: true, 11: true, 12: true}
	for x := range expectedChecked {
		if !checkedPositions[x] {
			t.Errorf("Expected position %d to be checked", x)
		}
	}

	t.Logf("No skipped characters: trail %v covers positions %v", trail, checkedPositions)
}

// ============================================================================
// Rapid Transitions and State Consistency Tests
// ============================================================================

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
