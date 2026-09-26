//go:build !vif_headless && !vif_noaudio && !wasm

package app

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/service"
	"github.com/lixenwraith/vif/internal/vlog"
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
	return a.hub.Register(service.NewAudioService(a.cfg.AudioMuted, a.cfg.AudioBackend, a.cfg.AudioBuffer, src))
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

// recordMusic starts the MusicWAV recording once the mixer runs. A failure costs
// the recording, not the run, so it is reported rather than returned.
func (a *App) recordMusic() {
	r := a.world.Resources.Audio
	if a.cfg.MusicWAV == "" || r == nil || r.Engine == nil {
		return
	}
	path := filepath.Join(a.cfg.MusicWAV, "vif-mus-"+vlog.FileStamp()+".wav")
	err := os.MkdirAll(a.cfg.MusicWAV, 0o755)
	if err == nil {
		err = r.Engine.RecordMusic(path)
	}
	if err != nil {
		vlog.Warn("app", "msg", "music not recorded", "path", path, "error", err.Error())
		a.ctx.SetStatusMessage("Music not recorded: "+err.Error(), parameter.StatusMessageMaxDuration, false)
		return
	}
	vlog.Info("app", "msg", "music recording", "path", path)
}

// holdMixer pauses the mixer with a replay viewer's pause, which stops ticks but not
// the music; a pause the recording itself holds keeps it held.
func (a *App) holdMixer(viewer bool) {
	if r := a.world.Resources.Audio; r != nil && r.Engine != nil {
		r.Engine.SetPaused(viewer || a.ctx.TimeCtl.IsPaused())
	}
}
