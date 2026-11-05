//go:build vif_headless || vif_noaudio

package app

import "github.com/lixenwraith/vif/internal/paths"

func (a *App) initAudioService() error { return nil }

func (a *App) reportAudioSpec() {}

func (a *App) recordMusic() {}

func (a *App) holdMixer(bool) {}

func (a *App) audioPlay(*paths.Play) {}
