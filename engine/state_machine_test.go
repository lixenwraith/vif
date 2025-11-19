package engine

import (
	"testing"
	"time"
)

// TestCanTransition tests the phase transition validation logic
func TestCanTransition(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	state := NewGameState(80, 24, 100, mockTime)

	// Define all valid transitions according to the state machine
	validTransitions := map[GamePhase][]GamePhase{
		PhaseNormal:         {PhaseGoldActive, PhaseCleanerPending},
		PhaseGoldActive:     {PhaseGoldComplete, PhaseCleanerPending},
		PhaseGoldComplete:   {PhaseDecayWait, PhaseCleanerPending},
		PhaseDecayWait:      {PhaseDecayAnimation},
		PhaseDecayAnimation: {PhaseNormal},
		PhaseCleanerPending: {PhaseCleanerActive},
		PhaseCleanerActive:  {PhaseDecayWait},
	}

	// Test all valid transitions
	for from, validTos := range validTransitions {
		for _, to := range validTos {
			if !state.CanTransition(from, to) {
				t.Errorf("Expected transition %s -> %s to be valid, but it was rejected", from, to)
			} else {
				t.Logf("✓ Valid transition: %s -> %s", from, to)
			}
		}
	}

	// Test some invalid transitions
	invalidTransitions := []struct {
		from GamePhase
		to   GamePhase
		desc string
	}{
		{PhaseNormal, PhaseDecayWait, "Normal -> DecayWait (must go through Gold)"},
		{PhaseNormal, PhaseDecayAnimation, "Normal -> DecayAnimation (must go through Gold and DecayWait)"},
		{PhaseGoldActive, PhaseNormal, "GoldActive -> Normal (must go through GoldComplete)"},
		{PhaseGoldActive, PhaseDecayWait, "GoldActive -> DecayWait (must go through GoldComplete)"},
		{PhaseGoldComplete, PhaseNormal, "GoldComplete -> Normal (must go through Decay)"},
		{PhaseDecayWait, PhaseNormal, "DecayWait -> Normal (must go through DecayAnimation)"},
		{PhaseDecayAnimation, PhaseDecayWait, "DecayAnimation -> DecayWait (can't go backwards)"},
		{PhaseDecayAnimation, PhaseGoldActive, "DecayAnimation -> GoldActive (must go through Normal)"},
		{PhaseCleanerPending, PhaseNormal, "CleanerPending -> Normal (must go through CleanerActive)"},
		{PhaseCleanerActive, PhaseCleanerPending, "CleanerActive -> CleanerPending (can't go backwards)"},
	}

	for _, tc := range invalidTransitions {
		if state.CanTransition(tc.from, tc.to) {
			t.Errorf("Expected transition %s -> %s to be invalid, but it was allowed (%s)", tc.from, tc.to, tc.desc)
		} else {
			t.Logf("✓ Correctly rejected invalid transition: %s", tc.desc)
		}
	}
}

// TestTransitionPhase tests the TransitionPhase method
func TestTransitionPhase(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	state := NewGameState(80, 24, 100, mockTime)

	// Verify initial state
	if state.GetPhase() != PhaseNormal {
		t.Fatalf("Expected initial phase to be Normal, got %s", state.GetPhase())
	}

	// Test valid transition sequence: Normal -> GoldActive -> GoldComplete -> DecayWait -> DecayAnimation -> Normal
	if !state.TransitionPhase(PhaseGoldActive) {
		t.Fatal("Failed to transition Normal -> GoldActive")
	}
	if state.GetPhase() != PhaseGoldActive {
		t.Errorf("Expected phase GoldActive, got %s", state.GetPhase())
	}
	t.Logf("✓ Transitioned Normal -> GoldActive")

	if !state.TransitionPhase(PhaseGoldComplete) {
		t.Fatal("Failed to transition GoldActive -> GoldComplete")
	}
	if state.GetPhase() != PhaseGoldComplete {
		t.Errorf("Expected phase GoldComplete, got %s", state.GetPhase())
	}
	t.Logf("✓ Transitioned GoldActive -> GoldComplete")

	if !state.TransitionPhase(PhaseDecayWait) {
		t.Fatal("Failed to transition GoldComplete -> DecayWait")
	}
	if state.GetPhase() != PhaseDecayWait {
		t.Errorf("Expected phase DecayWait, got %s", state.GetPhase())
	}
	t.Logf("✓ Transitioned GoldComplete -> DecayWait")

	if !state.TransitionPhase(PhaseDecayAnimation) {
		t.Fatal("Failed to transition DecayWait -> DecayAnimation")
	}
	if state.GetPhase() != PhaseDecayAnimation {
		t.Errorf("Expected phase DecayAnimation, got %s", state.GetPhase())
	}
	t.Logf("✓ Transitioned DecayWait -> DecayAnimation")

	if !state.TransitionPhase(PhaseNormal) {
		t.Fatal("Failed to transition DecayAnimation -> Normal")
	}
	if state.GetPhase() != PhaseNormal {
		t.Errorf("Expected phase Normal, got %s", state.GetPhase())
	}
	t.Logf("✓ Transitioned DecayAnimation -> Normal")

	// Test invalid transition
	if state.TransitionPhase(PhaseDecayWait) {
		t.Error("Should not be able to transition Normal -> DecayWait directly")
	} else {
		t.Logf("✓ Correctly rejected invalid transition Normal -> DecayWait")
	}
}

// TestTransitionPhaseWithCleaners tests the cleaner phase transitions
func TestTransitionPhaseWithCleaners(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	state := NewGameState(80, 24, 100, mockTime)

	// Test cleaner transition from Normal
	if !state.TransitionPhase(PhaseCleanerPending) {
		t.Fatal("Failed to transition Normal -> CleanerPending")
	}
	if state.GetPhase() != PhaseCleanerPending {
		t.Errorf("Expected phase CleanerPending, got %s", state.GetPhase())
	}
	t.Logf("✓ Transitioned Normal -> CleanerPending")

	if !state.TransitionPhase(PhaseCleanerActive) {
		t.Fatal("Failed to transition CleanerPending -> CleanerActive")
	}
	if state.GetPhase() != PhaseCleanerActive {
		t.Errorf("Expected phase CleanerActive, got %s", state.GetPhase())
	}
	t.Logf("✓ Transitioned CleanerPending -> CleanerActive")

	if !state.TransitionPhase(PhaseDecayWait) {
		t.Fatal("Failed to transition CleanerActive -> DecayWait")
	}
	if state.GetPhase() != PhaseDecayWait {
		t.Errorf("Expected phase DecayWait, got %s", state.GetPhase())
	}
	t.Logf("✓ Transitioned CleanerActive -> DecayWait")

	// Reset to Normal phase for next test
	state.TransitionPhase(PhaseDecayAnimation)
	state.TransitionPhase(PhaseNormal)

	// Test cleaner transition from GoldActive
	state.TransitionPhase(PhaseGoldActive)
	if !state.TransitionPhase(PhaseCleanerPending) {
		t.Fatal("Failed to transition GoldActive -> CleanerPending")
	}
	if state.GetPhase() != PhaseCleanerPending {
		t.Errorf("Expected phase CleanerPending, got %s", state.GetPhase())
	}
	t.Logf("✓ Transitioned GoldActive -> CleanerPending")

	state.TransitionPhase(PhaseCleanerActive)
	state.TransitionPhase(PhaseDecayWait)
	state.TransitionPhase(PhaseDecayAnimation)
	state.TransitionPhase(PhaseNormal)

	// Test cleaner transition from GoldComplete
	state.TransitionPhase(PhaseGoldActive)
	state.TransitionPhase(PhaseGoldComplete)
	if !state.TransitionPhase(PhaseCleanerPending) {
		t.Fatal("Failed to transition GoldComplete -> CleanerPending")
	}
	if state.GetPhase() != PhaseCleanerPending {
		t.Errorf("Expected phase CleanerPending, got %s", state.GetPhase())
	}
	t.Logf("✓ Transitioned GoldComplete -> CleanerPending")
}

// TestPhaseTransitionRace tests concurrent phase transitions (should not race with -race flag)
func TestPhaseTransitionRace(t *testing.T) {
	mockTime := NewMockTimeProvider(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	state := NewGameState(80, 24, 100, mockTime)

	// Start in GoldActive to have multiple valid transitions
	state.TransitionPhase(PhaseGoldActive)

	// Try to transition to different phases concurrently
	// Only one should succeed (the mutex should prevent races)
	done := make(chan bool, 2)

	go func() {
		state.TransitionPhase(PhaseGoldComplete)
		done <- true
	}()

	go func() {
		state.TransitionPhase(PhaseCleanerPending)
		done <- true
	}()

	<-done
	<-done

	// Check that we ended up in a valid phase
	phase := state.GetPhase()
	if phase != PhaseGoldComplete && phase != PhaseCleanerPending {
		t.Errorf("Expected phase to be GoldComplete or CleanerPending, got %s", phase)
	} else {
		t.Logf("✓ Concurrent transitions handled correctly, ended in %s", phase)
	}
}
