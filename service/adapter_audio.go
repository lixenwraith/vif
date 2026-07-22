package service

import (
	"errors"
	"os"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/audio"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
)

type AudioService struct {
	audioEngine *audio.AudioEngine
	disabled    atomic.Bool

	initMuted   bool
	initBackend string
}

func NewAudioService(muted bool, forceBackend string) *AudioService {
	return &AudioService{
		initMuted:   muted,
		initBackend: forceBackend,
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

	if data, err := os.ReadFile(parameter.MusicConfigFile); err == nil {
		config.PatternTOML = data
	}
	if data, err := os.ReadFile(parameter.SoundConfigFile); err == nil {
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
	// Registration completes inside Start before backend probing, so IDs are
	// valid even when the engine falls through to silent mode.
	startErr := s.audioEngine.Start()
	startErr = errors.Join(startErr, s.audioEngine.SpecError(), parameter.ResolveSounds())
	return startErr
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
