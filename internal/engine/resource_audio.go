//go:build !vif_headless && !vif_noaudio && !wasm

package engine

import "github.com/lixenwraith/vi-fighter/pkg/audio"

type audioResources struct {
	Audio *AudioResource
}

// AudioResource exposes the audio engine contributed by AudioService.
type AudioResource struct {
	Engine *audio.AudioEngine
}
