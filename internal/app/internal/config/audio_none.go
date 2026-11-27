//go:build vif_headless || vif_noaudio

package config

import "errors"

const buildHasAudio = false

func validateAudioBuildConfig(cfg Config) error {
	if cfg.AudioBackend != "" {
		return errors.New("audio backend unavailable in this build")
	}
	if cfg.Resources.Music != "" || cfg.Resources.Sounds != "" {
		return errors.New("audio overrides unavailable in this build")
	}
	if cfg.MusicWAV != "" {
		return errors.New("music recording unavailable in this build")
	}
	return nil
}
