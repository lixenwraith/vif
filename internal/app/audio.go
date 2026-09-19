//go:build !vif_headless && !vif_noaudio && !wasm

package app

import (
	"github.com/lixenwraith/vi-fighter/internal/resource"
	"github.com/lixenwraith/vi-fighter/internal/service"
)

const buildHasAudio = true

func validateAudioBuildConfig(Config) error { return nil }

func (a *App) initAudioService() error {
	if !a.cfg.Mode.Audio() {
		return nil
	}
	src, err := resource.Audio(a.cfg.Resources)
	if err != nil {
		return err
	}
	return a.hub.Register(service.NewAudioService(a.cfg.AudioMuted, a.cfg.AudioBackend, src))
}
