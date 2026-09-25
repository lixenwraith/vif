//go:build !vif_headless && !vif_noaudio && !wasm

package app

import (
	"strings"

	"github.com/lixenwraith/vi-fighter/internal/parameter"
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

// reportAudioSpec says in play that a malformed sounds.toml or music.toml fell back to
// the shipped bank, which otherwise only -check reports. Fallback stays non-fatal.
func (a *App) reportAudioSpec() {
	r := a.world.Resources.Audio
	if r == nil || r.Engine == nil || r.Engine.SpecError() == nil {
		return
	}
	first, _, _ := strings.Cut(r.Engine.SpecError().Error(), "\n")
	a.ctx.SetStatusMessage("Audio config: "+first+" (built-in used; -check lists all)",
		parameter.StatusMessageMaxDuration, false)
}

// holdMixer pauses the mixer with a replay viewer's pause, which stops ticks but not
// the music; a pause the recording itself holds keeps it held.
func (a *App) holdMixer(viewer bool) {
	if r := a.world.Resources.Audio; r != nil && r.Engine != nil {
		r.Engine.SetPaused(viewer || a.ctx.TimeCtl.IsPaused())
	}
}
