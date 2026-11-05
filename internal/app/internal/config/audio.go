//go:build !vif_headless && !vif_noaudio

package config

const buildHasAudio = true

func validateAudioBuildConfig(Config) error { return nil }
