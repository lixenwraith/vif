package audio

import (
	"testing"
	"time"
)

// TestSoundTypeValues verifies sound type constants
func TestSoundTypeValues(t *testing.T) {
	// Verify sound types have expected values
	if SoundError != 0 {
		t.Errorf("Expected SoundError=0, got %d", SoundError)
	}
	if SoundBell != 1 {
		t.Errorf("Expected SoundBell=1, got %d", SoundBell)
	}
	if SoundWhoosh != 2 {
		t.Errorf("Expected SoundWhoosh=2, got %d", SoundWhoosh)
	}
	if SoundCoin != 3 {
		t.Errorf("Expected SoundCoin=3, got %d", SoundCoin)
	}
}

// TestAudioCommandCreation verifies audio command struct
func TestAudioCommandCreation(t *testing.T) {
	now := time.Now()
	cmd := AudioCommand{
		Type:       SoundError,
		Priority:   5,
		Generation: 123,
		Timestamp:  now,
	}

	if cmd.Type != SoundError {
		t.Errorf("Expected Type=SoundError, got %d", cmd.Type)
	}
	if cmd.Priority != 5 {
		t.Errorf("Expected Priority=5, got %d", cmd.Priority)
	}
	if cmd.Generation != 123 {
		t.Errorf("Expected Generation=123, got %d", cmd.Generation)
	}
	if cmd.Timestamp != now {
		t.Errorf("Expected Timestamp=%v, got %v", now, cmd.Timestamp)
	}
}

// TestAudioConfigCreation verifies audio config struct
func TestAudioConfigCreation(t *testing.T) {
	cfg := &AudioConfig{
		Enabled:      true,
		MasterVolume: 0.8,
		EffectVolumes: map[SoundType]float64{
			SoundError: 0.5,
			SoundBell:  1.0,
		},
		MinSoundGap: 100 * time.Millisecond,
		SampleRate:  48000,
	}

	if !cfg.Enabled {
		t.Error("Expected Enabled=true")
	}
	if cfg.MasterVolume != 0.8 {
		t.Errorf("Expected MasterVolume=0.8, got %f", cfg.MasterVolume)
	}
	if cfg.EffectVolumes[SoundError] != 0.5 {
		t.Errorf("Expected SoundError volume=0.5, got %f", cfg.EffectVolumes[SoundError])
	}
	if cfg.EffectVolumes[SoundBell] != 1.0 {
		t.Errorf("Expected SoundBell volume=1.0, got %f", cfg.EffectVolumes[SoundBell])
	}
	if cfg.MinSoundGap != 100*time.Millisecond {
		t.Errorf("Expected MinSoundGap=100ms, got %v", cfg.MinSoundGap)
	}
	if cfg.SampleRate != 48000 {
		t.Errorf("Expected SampleRate=48000, got %d", cfg.SampleRate)
	}
}

// TestDefaultAudioConfigMinSoundGap verifies default minimum sound gap
func TestDefaultAudioConfigMinSoundGap(t *testing.T) {
	cfg := DefaultAudioConfig()

	expectedGap := 50 * time.Millisecond
	if cfg.MinSoundGap != expectedGap {
		t.Errorf("Expected MinSoundGap=%v, got %v", expectedGap, cfg.MinSoundGap)
	}
}

// TestAudioCommandPriority verifies priority field usage
func TestAudioCommandPriority(t *testing.T) {
	// Create commands with different priorities
	lowPriority := AudioCommand{
		Type:     SoundBell,
		Priority: 1,
	}

	highPriority := AudioCommand{
		Type:     SoundError,
		Priority: 10,
	}

	if lowPriority.Priority >= highPriority.Priority {
		t.Error("Expected high priority command to have higher priority value")
	}
}

// TestAudioCommandGeneration verifies generation counter
func TestAudioCommandGeneration(t *testing.T) {
	// Simulate generation counter incrementing
	cmd1 := AudioCommand{Type: SoundError, Generation: 1}
	cmd2 := AudioCommand{Type: SoundError, Generation: 2}
	cmd3 := AudioCommand{Type: SoundError, Generation: 3}

	if cmd1.Generation >= cmd2.Generation {
		t.Error("Expected generation to increment")
	}
	if cmd2.Generation >= cmd3.Generation {
		t.Error("Expected generation to increment")
	}
}

// TestAudioConfigEffectVolumesMap verifies effect volumes map
func TestAudioConfigEffectVolumesMap(t *testing.T) {
	cfg := DefaultAudioConfig()

	// Verify all sound types have volumes
	soundTypes := []SoundType{SoundError, SoundBell, SoundWhoosh, SoundCoin}

	for _, st := range soundTypes {
		if _, ok := cfg.EffectVolumes[st]; !ok {
			t.Errorf("Expected effect volume for sound type %d", st)
		}
	}

	// Verify volumes are in valid range
	for st, vol := range cfg.EffectVolumes {
		if vol < 0.0 || vol > 1.0 {
			t.Errorf("Effect volume for sound type %d out of range [0,1]: %f", st, vol)
		}
	}
}

// TestAudioConfigModification verifies config can be modified
func TestAudioConfigModification(t *testing.T) {
	cfg := DefaultAudioConfig()

	// Modify config
	cfg.Enabled = false
	cfg.MasterVolume = 0.3
	cfg.EffectVolumes[SoundError] = 0.1
	cfg.MinSoundGap = 200 * time.Millisecond
	cfg.SampleRate = 22050

	// Verify modifications
	if cfg.Enabled {
		t.Error("Expected Enabled=false after modification")
	}
	if cfg.MasterVolume != 0.3 {
		t.Errorf("Expected MasterVolume=0.3 after modification, got %f", cfg.MasterVolume)
	}
	if cfg.EffectVolumes[SoundError] != 0.1 {
		t.Errorf("Expected SoundError volume=0.1 after modification, got %f", cfg.EffectVolumes[SoundError])
	}
	if cfg.MinSoundGap != 200*time.Millisecond {
		t.Errorf("Expected MinSoundGap=200ms after modification, got %v", cfg.MinSoundGap)
	}
	if cfg.SampleRate != 22050 {
		t.Errorf("Expected SampleRate=22050 after modification, got %d", cfg.SampleRate)
	}
}

// TestAudioCommandTimestamp verifies timestamp field
func TestAudioCommandTimestamp(t *testing.T) {
	before := time.Now()
	time.Sleep(10 * time.Millisecond)

	cmd := AudioCommand{
		Type:      SoundError,
		Timestamp: time.Now(),
	}

	time.Sleep(10 * time.Millisecond)
	after := time.Now()

	if cmd.Timestamp.Before(before) {
		t.Error("Expected timestamp to be after 'before' time")
	}
	if cmd.Timestamp.After(after) {
		t.Error("Expected timestamp to be before 'after' time")
	}
}
