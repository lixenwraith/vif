//go:build !vif_headless && !vif_noaudio && !wasm

package engine

import "github.com/lixenwraith/vif/pkg/audio"

type audioResources struct {
	Audio *AudioResource
}

// AudioResource exposes the audio engine contributed by AudioService.
type AudioResource struct {
	Engine *audio.AudioEngine
}

func (r *Resource) EffectsVolume() float64 {
	if r.Audio == nil || r.Audio.Engine == nil {
		return 0
	}
	return r.Audio.Engine.Volume()
}

func (r *Resource) SetEffectsVolume(volume float64) {
	if r.Audio != nil && r.Audio.Engine != nil {
		r.Audio.Engine.SetVolume(volume)
	}
}
