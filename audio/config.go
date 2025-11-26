package audio

import (
	"github.com/lixenwraith/vi-fighter/constants"
)

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
		MinSoundGap: constants.MinSoundGap, // One clock tick gap
		SampleRate:  44100,
	}
}