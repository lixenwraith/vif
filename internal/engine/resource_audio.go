//go:build !vif_headless && !vif_noaudio

package engine

import (
	"sync/atomic"

	"github.com/lixenwraith/vif/pkg/audio"
)

type audioResources struct {
	Audio *AudioResource
}

// AudioResource exposes the audio engine contributed by AudioService. Pref is the
// player's channel mask as the audio system keeps it, before the scenario's gates.
type AudioResource struct {
	Engine *audio.AudioEngine
	Pref   atomic.Int32
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
