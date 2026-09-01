package service

import (
	"errors"
	"fmt"
	"os"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/audio"
)

// AudioSource names optional external overrides. Empty paths retain pkg/audio's
// embedded sounds and patterns.
type AudioSource struct {
	MusicPath string
	SoundPath string
}

type AudioService struct {
	audioEngine *audio.AudioEngine
	disabled    atomic.Bool

	initMuted   bool
	initBackend string
	src         AudioSource
}

func NewAudioService(muted bool, forceBackend string, src AudioSource) *AudioService {
	return &AudioService{
		initMuted:   muted,
		initBackend: forceBackend,
		src:         src,
	}
}

func (s *AudioService) Name() string           { return "audio" }
func (s *AudioService) Dependencies() []string { return nil }

func (s *AudioService) Init() error {
	config := audio.DefaultAudioConfig()
	config.Enabled = !s.initMuted
	config.ForceBackend = s.initBackend

	// Inject game-specific parameters, breaking cyclic dependency
	config.EffectVolumes = parameter.GameEffectVolumes
	config.EffectShapes = parameter.GameEffectShapes

	if s.src.MusicPath != "" {
		data, err := os.ReadFile(s.src.MusicPath)
		if err != nil {
			return fmt.Errorf("audio music %s: %w", s.src.MusicPath, err)
		}
		config.PatternTOML = data
	}
	if s.src.SoundPath != "" {
		data, err := os.ReadFile(s.src.SoundPath)
		if err != nil {
			return fmt.Errorf("audio sounds %s: %w", s.src.SoundPath, err)
		}
		config.SoundTOML = data
	}

	eng, err := audio.NewAudioEngine(config)
	if err != nil {
		s.disabled.Store(true)
		return nil // error discarded; no telemetry, no surface
	}
	s.audioEngine = eng
	return nil
}

func (s *AudioService) Start() error {
	if s.disabled.Load() || s.audioEngine == nil {
		return nil // Sfx stays SoundNone: every Play is a no-op
	}

	// No backend is a degradation, not a failure. The engine has already
	// latched silent mode, Play is a no-op, and the AudioResource bound in
	// Contribute stays valid. Only a broken subsystem (built-in sound
	// registry) aborts startup.
	if err := s.audioEngine.Start(); err != nil && !errors.Is(err, audio.ErrNoAudioBackend) {
		return err
	}

	// Registration, freeze and preload completed inside audioEngine.Start,
	// including on the ErrNoAudioBackend path, so the table resolves whether
	// or not a device was found. Hub.StartAll precedes scheduler.Start in
	// App.Loop, so no system has emitted EventSoundRequest yet.
	//
	// Fatal by design: a missing name means soundTable and the built-in TOML
	// disagree. Degrading would reinstate the failure this call fixes — every
	// Play discarded on the SoundNone guard, with no counter and no log.
	if err := parameter.ResolveSounds(); err != nil {
		return fmt.Errorf("audio service: %w", err)
	}

	// TODO: audioEngine.SpecError() still has no in-game surface — malformed
	// user specs degrade to built-ins during play. The -check startup mode does
	// validate and report the same resolved documents.
	return nil
}

func (s *AudioService) Stop() error {
	if s.audioEngine != nil {
		s.audioEngine.Stop()
	}
	return nil
}

func (s *AudioService) Contribute(r *engine.Resource) {
	if s.disabled.Load() || s.audioEngine == nil {
		return
	}
	r.Audio = &engine.AudioResource{Engine: s.audioEngine}
}
