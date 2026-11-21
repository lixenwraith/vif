//go:build !vif_headless && !vif_noaudio

package engine

import (
	"sync/atomic"

	"github.com/lixenwraith/audio"
	"github.com/lixenwraith/vif/internal/parameter"
)

type audioResources struct {
	Audio *AudioResource
}

// AudioResource exposes the audio engine contributed by AudioService. Pref is the
// player's channel mask as the audio system keeps it, before the scenario's gates,
// and Bed their binaural bed, a mode and above its byte the preset (PackBinaural).
type AudioResource struct {
	Engine *audio.AudioEngine
	Pref   atomic.Int32
	Bed    atomic.Uint32
}

// PackBinaural is a bed's mode and preset as AudioResource.Bed holds them
func PackBinaural(mode parameter.BinauralMode, preset parameter.BinauralPreset) uint32 {
	return uint32(mode) | uint32(preset)<<8
}

// Binaural is the player's binaural bed, its mode and the preset a fixed one
// plays: this machine's own setting, which no world's capture holds
func (r *Resource) Binaural() (parameter.BinauralMode, parameter.BinauralPreset) {
	if r.Audio == nil {
		return parameter.BinauralOff, 0
	}
	bed := r.Audio.Bed.Load()
	return parameter.BinauralMode(bed & 0xff), parameter.BinauralPreset(bed >> 8)
}

func (r *Resource) SfxVolume() float64 {
	if r.Audio == nil || r.Audio.Engine == nil {
		return 0
	}
	return r.Audio.Engine.SfxVolume()
}

func (r *Resource) SetSfxVolume(volume float64) {
	if r.Audio != nil && r.Audio.Engine != nil {
		r.Audio.Engine.SetSfxVolume(volume)
	}
}
