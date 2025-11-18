package engine

import (
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/constants"
)

// TestGoldToDecayPhaseTransition tests the Gold → DecayWait → DecayAnimation → Normal cycle
func TestGoldToDecayPhaseTransition(t *testing.T) {
	// Create mock time provider
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	state := NewGameState(80, 24, 100, mockTime)

	// Start in Normal phase
	if phase := state.GetPhase(); phase != PhaseNormal {
		t.Fatalf("Expected PhaseNormal, got %v", phase)
	}

	// Activate gold sequence (simulate gold spawn)
	sequenceID := state.IncrementGoldSequenceID()
	state.ActivateGoldSequence(sequenceID, constants.GoldSequenceDuration)

	// Verify transition to PhaseGoldActive
	if phase := state.GetPhase(); phase != PhaseGoldActive {
		t.Fatalf("Expected PhaseGoldActive after activation, got %v", phase)
	}

	// Verify gold state
	goldSnapshot := state.ReadGoldState()
	if !goldSnapshot.Active {
		t.Fatal("Gold should be active")
	}
	if goldSnapshot.SequenceID != sequenceID {
		t.Fatalf("Expected sequence ID %d, got %d", sequenceID, goldSnapshot.SequenceID)
	}

	// Advance time past gold timeout
	mockTime.Advance(constants.GoldSequenceDuration + time.Second)

	// Verify gold is timed out
	if !state.IsGoldTimedOut() {
		t.Fatal("Gold should be timed out")
	}

	// Deactivate gold and start decay timer (simulating clock tick)
	state.DeactivateGoldSequence()
	state.StartDecayTimer(
		state.ScreenWidth,
		constants.HeatBarIndicatorWidth,
		constants.DecayIntervalBaseSeconds,
		constants.DecayIntervalRangeSeconds,
	)

	// Verify transition to PhaseDecayWait
	if phase := state.GetPhase(); phase != PhaseDecayWait {
		t.Fatalf("Expected PhaseDecayWait after decay timer start, got %v", phase)
	}

	// Verify decay timer is active
	decaySnapshot := state.ReadDecayState()
	if !decaySnapshot.TimerActive {
		t.Fatal("Decay timer should be active")
	}
	if decaySnapshot.Animating {
		t.Fatal("Decay animation should not be running yet")
	}

	// Advance time past decay interval
	// At zero heat, interval should be 60 seconds
	mockTime.Advance(61 * time.Second)

	// Verify decay is ready
	if !state.IsDecayReady() {
		t.Fatal("Decay should be ready")
	}

	// Start decay animation (simulating clock tick)
	state.StartDecayAnimation()

	// Verify transition to PhaseDecayAnimation
	if phase := state.GetPhase(); phase != PhaseDecayAnimation {
		t.Fatalf("Expected PhaseDecayAnimation, got %v", phase)
	}

	// Verify decay animation is running
	decaySnapshot = state.ReadDecayState()
	if !decaySnapshot.Animating {
		t.Fatal("Decay animation should be running")
	}
	if decaySnapshot.TimerActive {
		t.Fatal("Decay timer should not be active during animation")
	}

	// Stop decay animation (simulating animation completion)
	state.StopDecayAnimation()

	// Verify transition back to PhaseNormal
	if phase := state.GetPhase(); phase != PhaseNormal {
		t.Fatalf("Expected PhaseNormal after animation stop, got %v", phase)
	}

	// Verify decay state is clean
	decaySnapshot = state.ReadDecayState()
	if decaySnapshot.Animating {
		t.Fatal("Decay animation should not be running")
	}
	if decaySnapshot.TimerActive {
		t.Fatal("Decay timer should not be active")
	}
}

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

// TestConcurrentPhaseAccess tests concurrent reads during phase transitions
func TestConcurrentPhaseAccess(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	state := NewGameState(80, 24, 100, mockTime)

	// Number of concurrent readers
	const numReaders = 10
	const iterations = 100

	done := make(chan bool, numReaders)

	// Start concurrent readers
	for i := 0; i < numReaders; i++ {
		go func() {
			for j := 0; j < iterations; j++ {
				// Read phase state
				_ = state.GetPhase()
				_ = state.ReadPhaseState()

				// Read gold state
				_ = state.GetGoldActive()
				_ = state.ReadGoldState()

				// Read decay state
				_ = state.GetDecayAnimating()
				_ = state.ReadDecayState()
			}
			done <- true
		}()
	}

	// Perform phase transitions while readers are active
	for i := 0; i < 10; i++ {
		// Activate gold
		sequenceID := state.IncrementGoldSequenceID()
		state.ActivateGoldSequence(sequenceID, constants.GoldSequenceDuration)

		// Deactivate and start decay
		state.DeactivateGoldSequence()
		state.StartDecayTimer(
			state.ScreenWidth,
			constants.HeatBarIndicatorWidth,
			constants.DecayIntervalBaseSeconds,
			constants.DecayIntervalRangeSeconds,
		)

		// Start decay animation
		state.StartDecayAnimation()

		// Stop decay animation
		state.StopDecayAnimation()
	}

	// Wait for all readers to finish
	for i := 0; i < numReaders; i++ {
		<-done
	}

	// Verify final state is consistent
	if phase := state.GetPhase(); phase != PhaseNormal {
		t.Fatalf("Expected final phase to be PhaseNormal, got %v", phase)
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
