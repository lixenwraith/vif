// @lixen: #focus{sys[audio,config],audio[config],conf[audio]}
// @lixen: #interact{state[audio(config)]}
package audio

import (
	"github.com/lixenwraith/vi-fighter/constants"
)

// AudioConfig holds audio system configuration
type AudioConfig struct {
	Enabled       bool
	MasterVolume  float64
	EffectVolumes map[SoundType]float64
	SampleRate    int
}

// DefaultAudioConfig returns default configuration
func DefaultAudioConfig() *AudioConfig {
	return &AudioConfig{
		Enabled:      false,
		MasterVolume: 0.5,
		EffectVolumes: map[SoundType]float64{
			SoundError:  0.8,
			SoundBell:   1.0,
			SoundWhoosh: 0.6,
			SoundCoin:   0.5,
		},
		SampleRate: constants.AudioSampleRate,
	}
}