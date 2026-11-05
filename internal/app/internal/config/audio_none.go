//go:build vif_headless || vif_noaudio

package config

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
