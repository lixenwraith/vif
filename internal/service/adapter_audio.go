//go:build !vif_headless && !vif_noaudio

package service

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	sink        io.WriteCloser // the host's speakers; nil finds a backend by name
}

func NewAudioService(muted bool, forceBackend string, buffer time.Duration, src AudioSource, sink io.WriteCloser) *AudioService {
	return &AudioService{
		initMuted:   muted,
		initBackend: forceBackend,
		initBuffer:  buffer,
		src:         src,
		sink:        sink,
	}
}

func (s *AudioService) Name() string           { return "audio" }
func (s *AudioService) Dependencies() []string { return nil }

func (s *AudioService) Init() error {
	config := audio.DefaultAudioConfig()
	config.Enabled = !s.initMuted
	config.ForceBackend = s.initBackend
	config.Buffer = s.initBuffer
	config.Sink = s.sink

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
	if p := s.src.SoundPath; p != "" {
		// Includes resolve inside the document's directory, as soundlab's do
		config.SoundFS, config.SoundFile = os.DirFS(filepath.Dir(p)), filepath.Base(p)
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

	// Registration, freeze and preload completed inside audioEngine.Start, on the
	// ErrNoAudioBackend path too, so the table resolves whether or not a device was
	// found, and Hub.StartAll precedes the first tick, so no sound was requested yet.
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
