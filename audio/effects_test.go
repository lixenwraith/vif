package audio

import (
	"testing"
	"time"

	"github.com/gopxl/beep"
)

// TestOscillatorSine verifies sine wave generation
func TestOscillatorSine(t *testing.T) {
	rate := beep.SampleRate(44100)
	duration := 100 * time.Millisecond
	freq := 440.0

	osc := NewOscillator(freq, duration, WaveSine, rate)

	if osc == nil {
		t.Fatal("Expected non-nil oscillator")
	}

	// Stream some samples
	samples := make([][2]float64, 100)
	n, ok := osc.Stream(samples)

	if !ok {
		t.Error("Expected stream to return ok=true")
	}

	if n != 100 {
		t.Errorf("Expected to stream 100 samples, got %d", n)
	}

	// Verify samples are within valid range [-1, 1]
	for i := 0; i < n; i++ {
		if samples[i][0] < -1.0 || samples[i][0] > 1.0 {
			t.Errorf("Sample %d out of range: %f", i, samples[i][0])
		}
		if samples[i][1] < -1.0 || samples[i][1] > 1.0 {
			t.Errorf("Sample %d out of range: %f", i, samples[i][1])
		}
	}

	// Verify oscillator has no error
	if osc.Err() != nil {
		t.Errorf("Expected no error, got: %v", osc.Err())
	}
}

// TestOscillatorSquare verifies square wave generation
func TestOscillatorSquare(t *testing.T) {
	rate := beep.SampleRate(44100)
	duration := 50 * time.Millisecond
	freq := 220.0

	osc := NewOscillator(freq, duration, WaveSquare, rate)

	samples := make([][2]float64, 50)
	n, ok := osc.Stream(samples)

	if !ok {
		t.Error("Expected stream to return ok=true")
	}

	if n != 50 {
		t.Errorf("Expected to stream 50 samples, got %d", n)
	}

	// Square wave should only have values of -1.0 or 1.0
	for i := 0; i < n; i++ {
		val := samples[i][0]
		if val != -1.0 && val != 1.0 {
			t.Errorf("Square wave sample %d should be -1.0 or 1.0, got %f", i, val)
		}
	}
}

// TestOscillatorSaw verifies sawtooth wave generation
func TestOscillatorSaw(t *testing.T) {
	rate := beep.SampleRate(44100)
	duration := 50 * time.Millisecond
	freq := 110.0

	osc := NewOscillator(freq, duration, WaveSaw, rate)

	samples := make([][2]float64, 50)
	n, ok := osc.Stream(samples)

	if !ok {
		t.Error("Expected stream to return ok=true")
	}

	if n != 50 {
		t.Errorf("Expected to stream 50 samples, got %d", n)
	}

	// Sawtooth should be within [-1, 1]
	for i := 0; i < n; i++ {
		val := samples[i][0]
		if val < -1.0 || val > 1.0 {
			t.Errorf("Sawtooth sample %d out of range: %f", i, val)
		}
	}
}

// TestOscillatorNoise verifies noise generation
func TestOscillatorNoise(t *testing.T) {
	rate := beep.SampleRate(44100)
	duration := 50 * time.Millisecond

	osc := NewOscillator(0, duration, WaveNoise, rate)

	samples := make([][2]float64, 50)
	n, ok := osc.Stream(samples)

	if !ok {
		t.Error("Expected stream to return ok=true")
	}

	if n != 50 {
		t.Errorf("Expected to stream 50 samples, got %d", n)
	}

	// Noise should be within [-1, 1]
	for i := 0; i < n; i++ {
		val := samples[i][0]
		if val < -1.0 || val > 1.0 {
			t.Errorf("Noise sample %d out of range: %f", i, val)
		}
	}

	// Verify samples are not all the same (randomness check)
	allSame := true
	firstVal := samples[0][0]
	for i := 1; i < n; i++ {
		if samples[i][0] != firstVal {
			allSame = false
			break
		}
	}
	if allSame {
		t.Error("Expected noise samples to vary, but all were the same")
	}
}

// TestOscillatorDuration verifies oscillator respects duration
func TestOscillatorDuration(t *testing.T) {
	rate := beep.SampleRate(44100)
	duration := 10 * time.Millisecond
	expectedSamples := rate.N(duration)

	osc := NewOscillator(440.0, duration, WaveSine, rate)

	// Request more samples than duration
	samples := make([][2]float64, expectedSamples*2)
	n, ok := osc.Stream(samples)

	// Should only stream up to duration
	if n > expectedSamples {
		t.Errorf("Expected at most %d samples, got %d", expectedSamples, n)
	}

	// Second stream should return ok=false (finished)
	samples2 := make([][2]float64, 10)
	n2, ok2 := osc.Stream(samples2)

	if ok2 {
		t.Error("Expected second stream to return ok=false after duration exceeded")
	}

	if n2 != 0 {
		t.Errorf("Expected 0 samples after duration, got %d", n2)
	}
}

// TestEnvelopeBasic verifies envelope shaping
func TestEnvelopeBasic(t *testing.T) {
	rate := beep.SampleRate(44100)
	duration := 100 * time.Millisecond
	attack := 20 * time.Millisecond
	release := 20 * time.Millisecond

	osc := NewOscillator(440.0, duration, WaveSine, rate)
	env := NewEnvelope(osc, duration, attack, release, rate)

	if env == nil {
		t.Fatal("Expected non-nil envelope")
	}

	samples := make([][2]float64, rate.N(duration))
	n, ok := env.Stream(samples)

	if !ok {
		t.Error("Expected envelope to stream successfully")
	}

	if n != len(samples) {
		t.Errorf("Expected %d samples, got %d", len(samples), n)
	}

	// Verify envelope has no error
	if env.Err() != nil {
		t.Errorf("Expected no error, got: %v", env.Err())
	}
}

// TestEnvelopeAttackPhase verifies attack ramp-up
func TestEnvelopeAttackPhase(t *testing.T) {
	rate := beep.SampleRate(44100)
	duration := 100 * time.Millisecond
	attack := 50 * time.Millisecond
	release := 10 * time.Millisecond

	// Use square wave for consistent amplitude
	osc := NewOscillator(100.0, duration, WaveSquare, rate)
	env := NewEnvelope(osc, duration, attack, release, rate)

	attackSamples := rate.N(attack)
	samples := make([][2]float64, attackSamples)
	n, ok := env.Stream(samples)

	if !ok {
		t.Error("Expected envelope to stream successfully")
	}

	// First sample should have lower amplitude than last
	firstAmp := abs(samples[0][0])
	lastAmp := abs(samples[n-1][0])

	if firstAmp >= lastAmp {
		t.Errorf("Expected attack phase to ramp up, but first=%f >= last=%f", firstAmp, lastAmp)
	}
}

// TestCreateErrorSound verifies error sound generation
func TestCreateErrorSound(t *testing.T) {
	cfg := DefaultAudioConfig()
	sound := CreateErrorSound(cfg)

	if sound == nil {
		t.Fatal("Expected non-nil error sound")
	}

	// Stream some samples to verify it works
	samples := make([][2]float64, 100)
	n, ok := sound.Stream(samples)

	if !ok {
		t.Error("Expected error sound to stream successfully")
	}

	if n == 0 {
		t.Error("Expected error sound to produce samples")
	}
}

// TestCreateBellSound verifies bell sound generation
func TestCreateBellSound(t *testing.T) {
	cfg := DefaultAudioConfig()
	sound := CreateBellSound(cfg)

	if sound == nil {
		t.Fatal("Expected non-nil bell sound")
	}

	// Stream some samples to verify it works
	samples := make([][2]float64, 1000)
	n, ok := sound.Stream(samples)

	if !ok {
		t.Error("Expected bell sound to stream successfully")
	}

	if n == 0 {
		t.Error("Expected bell sound to produce samples")
	}
}

// TestCreateWhooshSound verifies whoosh sound generation
func TestCreateWhooshSound(t *testing.T) {
	cfg := DefaultAudioConfig()
	sound := CreateWhooshSound(cfg)

	if sound == nil {
		t.Fatal("Expected non-nil whoosh sound")
	}

	// Stream some samples to verify it works
	samples := make([][2]float64, 500)
	n, ok := sound.Stream(samples)

	if !ok {
		t.Error("Expected whoosh sound to stream successfully")
	}

	if n == 0 {
		t.Error("Expected whoosh sound to produce samples")
	}
}

// TestCreateCoinSound verifies coin sound generation
func TestCreateCoinSound(t *testing.T) {
	cfg := DefaultAudioConfig()
	sound := CreateCoinSound(cfg)

	if sound == nil {
		t.Fatal("Expected non-nil coin sound")
	}

	// Stream some samples to verify it works
	samples := make([][2]float64, 2000)
	n, ok := sound.Stream(samples)

	if !ok {
		t.Error("Expected coin sound to stream successfully")
	}

	if n == 0 {
		t.Error("Expected coin sound to produce samples")
	}
}

// TestGetSoundEffect verifies sound effect retrieval
func TestGetSoundEffect(t *testing.T) {
	cfg := DefaultAudioConfig()

	testCases := []struct {
		soundType SoundType
		name      string
	}{
		{SoundError, "Error"},
		{SoundBell, "Bell"},
		{SoundWhoosh, "Whoosh"},
		{SoundCoin, "Coin"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sound := GetSoundEffect(tc.soundType, cfg)
			if sound == nil {
				t.Errorf("Expected non-nil sound for %s", tc.name)
			}

			// Verify sound produces samples
			samples := make([][2]float64, 100)
			n, ok := sound.Stream(samples)
			if !ok {
				t.Errorf("Expected %s sound to stream successfully", tc.name)
			}
			if n == 0 {
				t.Errorf("Expected %s sound to produce samples", tc.name)
			}
		})
	}
}

// TestGetSoundEffectInvalid verifies handling of invalid sound type
func TestGetSoundEffectInvalid(t *testing.T) {
	cfg := DefaultAudioConfig()
	sound := GetSoundEffect(SoundType(999), cfg)

	if sound != nil {
		t.Error("Expected nil for invalid sound type")
	}
}

// TestSoundEffectVolume verifies volume scaling
func TestSoundEffectVolume(t *testing.T) {
	cfg := DefaultAudioConfig()

	// Test with different volumes
	testVolumes := []float64{0.0, 0.5, 1.0}

	for _, vol := range testVolumes {
		cfg.MasterVolume = vol
		sound := CreateErrorSound(cfg)

		if sound == nil {
			t.Fatalf("Expected non-nil sound for volume %f", vol)
		}

		// Stream samples
		samples := make([][2]float64, 100)
		n, ok := sound.Stream(samples)

		if !ok {
			t.Errorf("Expected sound to stream at volume %f", vol)
		}

		if n == 0 {
			t.Errorf("Expected samples at volume %f", vol)
		}

		// For zero volume, samples should be very small or zero
		if vol == 0.0 {
			maxAmp := 0.0
			for i := 0; i < n; i++ {
				amp := abs(samples[i][0])
				if amp > maxAmp {
					maxAmp = amp
				}
			}
			// Zero volume should produce near-zero amplitude
			if maxAmp > 0.01 {
				t.Errorf("Expected near-zero amplitude for zero volume, got max %f", maxAmp)
			}
		}
	}
}

// TestNewVolumeZero verifies zero volume handling
func TestNewVolumeZero(t *testing.T) {
	rate := beep.SampleRate(44100)
	osc := NewOscillator(440.0, 50*time.Millisecond, WaveSine, rate)

	// Create volume effect with zero volume
	vol := newVolume(osc, 0.0)

	if vol == nil {
		t.Fatal("Expected non-nil volume effect")
	}

	samples := make([][2]float64, 100)
	n, ok := vol.Stream(samples)

	if !ok {
		t.Error("Expected volume effect to stream")
	}

	if n == 0 {
		t.Error("Expected volume effect to produce samples")
	}
}

// Helper function for absolute value
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
