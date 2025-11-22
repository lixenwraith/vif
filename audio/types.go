package audio

import "time"

// SoundType represents different sound effects in the game
type SoundType int

const (
	SoundError  SoundType = iota // Typing error buzz
	SoundBell                    // Nugget collection
	SoundWhoosh                  // Cleaner activation
	SoundCoin                    // Gold sequence complete
)

// AudioCommand represents a sound playback request
type AudioCommand struct {
	Type       SoundType
	Priority   int       // Higher priority overrides current sound
	Generation uint64    // Generation counter for stale command detection
	Timestamp  time.Time // When command was created
}

// AudioConfig holds audio system configuration
type AudioConfig struct {
	Enabled       bool                  // Global audio enable/disable
	MasterVolume  float64               // 0.0 to 1.0
	EffectVolumes map[SoundType]float64 // Per-effect volume multipliers
	MinSoundGap   time.Duration         // Minimum gap between sounds
	SampleRate    int                   // Audio sample rate
}

// DefaultAudioConfig returns default audio configuration
func DefaultAudioConfig() *AudioConfig {
	return &AudioConfig{
		Enabled:      true,
		MasterVolume: 0.5,
		EffectVolumes: map[SoundType]float64{
			SoundError:  0.8,
			SoundBell:   1.0,
			SoundWhoosh: 0.6,
			SoundCoin:   0.5,
		},
		MinSoundGap: 50 * time.Millisecond, // One clock tick gap
		SampleRate:  44100,
	}
}