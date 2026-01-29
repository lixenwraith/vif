package audio

import (
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// AudioConfig holds audio system configuration
type AudioConfig struct {
	Enabled       bool
	MasterVolume  float64
	EffectVolumes map[core.SoundType]float64
	SampleRate    int
}

// DefaultAudioConfig returns default configuration
func DefaultAudioConfig() *AudioConfig {
	return &AudioConfig{
		Enabled:      false,
		MasterVolume: 0.5,
		EffectVolumes: map[core.SoundType]float64{
			core.SoundError:    0.8,
			core.SoundBell:     1.0,
			core.SoundWhoosh:   0.6,
			core.SoundCoin:     0.5,
			core.SoundShield:   0.7,
			core.SoundZap:      0.5,
			core.SoundCrackle:  0.6,
			core.SoundMetalHit: 0.7,
		},
		SampleRate: parameter.AudioSampleRate,
	}
}