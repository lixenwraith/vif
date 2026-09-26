//go:build !vif_headless && !vif_noaudio && !wasm

package service

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/audio"
)

type AudioService struct {
	audioEngine *audio.AudioEngine

	initMuted   bool
	initBackend string
	initBuffer  time.Duration
	src         AudioSource
}

func NewAudioService(muted bool, forceBackend string, buffer time.Duration, src AudioSource) *AudioService {
	return &AudioService{
		initMuted:   muted,
		initBackend: forceBackend,
		initBuffer:  buffer,
		src:         src,
	}
}

func (s *AudioService) Name() string           { return "audio" }
func (s *AudioService) Dependencies() []string { return nil }

func (s *AudioService) Init() error {
	config := audio.DefaultAudioConfig()
	config.Enabled = !s.initMuted
	config.ForceBackend = s.initBackend
	config.Buffer = s.initBuffer

	// Inject game-specific parameters, breaking cyclic dependency
	config.EffectVolumes = parameter.GameEffectVolumes
	config.EffectShapes = parameter.GameEffectShapes

	// pkg/audio ships no specs or authored patterns; both banks are embedder data.
	base, err := parameter.BuiltinSounds()
	if err != nil {
		return fmt.Errorf("built-in sounds: %w", err)
	}
	config.BaseSounds = base
	if config.BasePatterns, err = parameter.BuiltinPatterns(); err != nil {
		return fmt.Errorf("built-in patterns: %w", err)
	}

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
		return fmt.Errorf("audio: %w", err) // a configuration the player wrote
	}
	s.audioEngine = eng
	return nil
}

func (s *AudioService) Start() error {
	if s.audioEngine == nil {
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
	// Fatal by design: a missing name means soundTable and the shipped bank
	// disagree, and every Play of it would be a bad-ID rejection.
	if err := parameter.ResolveSounds(); err != nil {
		return fmt.Errorf("audio service: %w", err)
	}

	return nil
}

func (s *AudioService) Stop() error {
	if s.audioEngine != nil {
		s.audioEngine.FadeOut()
		s.audioEngine.Stop()
	}
	return nil
}

func (s *AudioService) Contribute(r *engine.Resource) {
	if s.audioEngine == nil {
		return
	}
	r.Audio = &engine.AudioResource{Engine: s.audioEngine}
}
