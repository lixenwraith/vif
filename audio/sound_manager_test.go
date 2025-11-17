package audio

import (
	"testing"
)

// TestSoundManagerGracefulDegradation verifies audio operations don't panic when not initialized
func TestSoundManagerGracefulDegradation(t *testing.T) {
	sm := NewSoundManager()

	// All operations should be safe to call without initialization
	// These should not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Sound operations panicked without initialization: %v", r)
		}
	}()

	sm.PlayTrail()
	sm.StopTrail()
	sm.PlayError()
	sm.PlayMaxHeat()
	sm.StopMaxHeat()
	sm.PlayDecay()
	sm.Cleanup()
}

// TestSoundManagerInitialization verifies sound manager can be initialized and cleaned up
func TestSoundManagerInitialization(t *testing.T) {
	sm := NewSoundManager()

	// Note: Speaker initialization may fail in CI/test environments without audio devices
	// This is expected behavior - the game should work without audio
	err := sm.Initialize()
	if err != nil {
		t.Logf("Sound initialization failed (expected in test environment): %v", err)
		// Not a test failure - audio is optional
		return
	}

	// If initialization succeeded, cleanup should work
	sm.Cleanup()
}

// TestSoundManagerDoubleInitialization verifies double initialization is safe
func TestSoundManagerDoubleInitialization(t *testing.T) {
	sm := NewSoundManager()

	err1 := sm.Initialize()
	if err1 != nil {
		t.Logf("First initialization failed (expected in test environment): %v", err1)
		return
	}

	// Second initialization should be a no-op
	err2 := sm.Initialize()
	if err2 != nil {
		t.Errorf("Second initialization should succeed as no-op, got error: %v", err2)
	}

	sm.Cleanup()
}

// TestSoundManagerCleanupWithoutInit verifies cleanup without initialization is safe
func TestSoundManagerCleanupWithoutInit(t *testing.T) {
	sm := NewSoundManager()

	// Should not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Cleanup panicked without initialization: %v", r)
		}
	}()

	sm.Cleanup()
}

// TestSoundManagerOperationsAfterCleanup verifies operations after cleanup are safe
func TestSoundManagerOperationsAfterCleanup(t *testing.T) {
	sm := NewSoundManager()

	err := sm.Initialize()
	if err != nil {
		t.Logf("Initialization failed (expected in test environment): %v", err)
		// Continue test - operations after cleanup should still be safe
	}

	sm.Cleanup()

	// All operations should be safe after cleanup
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Sound operations panicked after cleanup: %v", r)
		}
	}()

	sm.PlayTrail()
	sm.StopTrail()
	sm.PlayError()
	sm.PlayMaxHeat()
	sm.StopMaxHeat()
	sm.PlayDecay()
}

// TestAudioConstants verifies audio constants are reasonable
func TestAudioConstants(t *testing.T) {
	if sampleRate != 48000 {
		t.Errorf("Expected sample rate 48000, got %d", sampleRate)
	}

	if speakerBufferDurationMs <= 0 {
		t.Error("Speaker buffer duration must be positive")
	}

	if errorBuzzDurationMs <= 0 {
		t.Error("Error buzz duration must be positive")
	}

	if decaySoundDurationMs <= 0 {
		t.Error("Decay sound duration must be positive")
	}

	if whroomCycleDurationS <= 0 {
		t.Error("Whroom cycle duration must be positive")
	}

	if synthwaveBeatIntervalMs <= 0 {
		t.Error("Synthwave beat interval must be positive")
	}
}

// TestAudioAmplitudes verifies audio amplitudes are in reasonable range
func TestAudioAmplitudes(t *testing.T) {
	amplitudes := []struct {
		name  string
		value float64
	}{
		{"errorBuzzAmplitude", errorBuzzAmplitude},
		{"whroomBaseAmplitude", whroomBaseAmplitude},
		{"synthwaveBassAmplitude", synthwaveBassAmplitude},
		{"synthwaveKickAmplitude", synthwaveKickAmplitude},
		{"decayNoiseAmplitude", decayNoiseAmplitude},
		{"decayRumbleAmplitude", decayRumbleAmplitude},
	}

	for _, amp := range amplitudes {
		if amp.value < 0 || amp.value > 1.0 {
			t.Errorf("%s should be between 0 and 1.0, got %f", amp.name, amp.value)
		}
	}
}

// TestAudioFrequencies verifies audio frequencies are in audible range
func TestAudioFrequencies(t *testing.T) {
	frequencies := []struct {
		name  string
		value float64
	}{
		{"errorBuzzFrequencyHz", errorBuzzFrequencyHz},
		{"whroomFreqMinHz", whroomFreqMinHz},
		{"whroomFreqMaxHz", whroomFreqMaxHz},
		{"synthwaveBassFrequencyHz", synthwaveBassFrequencyHz},
		{"synthwaveKickFrequencyHz", synthwaveKickFrequencyHz},
		{"decayRumbleFrequencyHz", decayRumbleFrequencyHz},
	}

	for _, freq := range frequencies {
		// Human hearing range is roughly 20Hz to 20kHz
		// We expect low frequencies for game audio (20Hz to 500Hz)
		if freq.value < 20 || freq.value > 500 {
			t.Errorf("%s should be between 20 and 500 Hz for game audio, got %f", freq.name, freq.value)
		}
	}
}
