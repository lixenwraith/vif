//go:build vif_headless || vif_noaudio || wasm

package app

import "errors"

const buildHasAudio = false

func validateAudioBuildConfig(cfg Config) error {
	if cfg.AudioBackend != "" {
		return errors.New("audio backend is unavailable in this build")
	}
	if cfg.Resources.Music != "" || cfg.Resources.Sounds != "" {
		return errors.New("audio overrides are unavailable in this build")
	}
	if cfg.MusicWAV != "" {
		return errors.New("music recording is unavailable in this build")
	}
	return nil
}

func (a *App) initAudioService() error { return nil }

func (a *App) reportAudioSpec() {}

func (a *App) recordMusic() {}

func (a *App) holdMixer(bool) {}
