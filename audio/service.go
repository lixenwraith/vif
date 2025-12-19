package audio

import (
	"sync/atomic"
)

// AudioService wraps AudioEngine as a Service
// Handles graceful degradation when no audio backend is available
type AudioService struct {
	engine   *AudioEngine
	disabled atomic.Bool
}

// NewService creates a new audio service
func NewService() *AudioService {
	return &AudioService{}
}

// Name implements Service
func (s *AudioService) Name() string {
	return "audio"
}

// Dependencies implements Service
func (s *AudioService) Dependencies() []string {
	return nil
}

// Init implements Service
// args[0]: bool - initial mute state (true = muted, false = unmuted, default = muted)
// Detects audio backend; sets disabled flag on failure (no error returned)
func (s *AudioService) Init(args ...any) error {
	config := DefaultAudioConfig()

	// Apply mute arg: true = muted (Enabled=false), false = unmuted (Enabled=true)
	if len(args) > 0 {
		if muted, ok := args[0].(bool); ok {
			config.Enabled = !muted
		}
	}
	// Default: config.Enabled = false (muted)

	engine, err := NewAudioEngine(config)
	if err != nil {
		s.disabled.Store(true)
		return nil
	}
	s.engine = engine
	return nil
}

// Start implements Service
// Launches mixer goroutine; sets disabled on failure (no error returned)
func (s *AudioService) Start() error {
	if s.disabled.Load() || s.engine == nil {
		return nil
	}

	if err := s.engine.Start(); err != nil {
		s.disabled.Store(true)
		s.engine = nil
		return nil
	}
	return nil
}

// Stop implements Service
func (s *AudioService) Stop() error {
	if s.engine != nil && s.engine.IsRunning() {
		s.engine.Stop()
	}
	return nil
}

// IsDisabled returns true if audio is unavailable
func (s *AudioService) IsDisabled() bool {
	return s.disabled.Load()
}

// Engine returns the underlying AudioEngine (may be nil if disabled)
func (s *AudioService) Engine() *AudioEngine {
	if s.disabled.Load() {
		return nil
	}
	return s.engine
}

// Player returns an AudioPlayer interface for game systems
// Returns nil if audio is disabled
func (s *AudioService) Player() AudioPlayer {
	if s.disabled.Load() || s.engine == nil {
		return nil
	}
	return s.engine
}

// AudioPlayer defines the minimal audio interface used by game systems
type AudioPlayer interface {
	Play(SoundType) bool
	ToggleMute() bool
	IsMuted() bool
	IsRunning() bool
}