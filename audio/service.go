package audio

import (
	"os"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/service"
)

// AudioService wraps AudioEngine as a Service
// Handles graceful degradation when no audio backend is available
type AudioService struct {
	audioEngine *AudioEngine
	disabled    atomic.Bool
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
// args[0]: bool - initial mute state (true = effectMuted, false = unmuted, default = effectMuted)
// Detects audio backend; sets disabled flag on failure (no error returned)
func (s *AudioService) Init(args ...any) error {
	config := DefaultAudioConfig()

	// Apply mute arg: true = effectMuted (Enabled=false), false = unmuted (Enabled=true)
	if len(args) > 0 {
		if muted, ok := args[0].(bool); ok {
			config.Enabled = !muted
		}
	}
	// Default: config.Enabled = false (effectMuted)

	// -ab backend override
	if len(args) > 1 {
		if name, ok := args[1].(string); ok {
			config.ForceBackend = name
		}
	}

	if data, err := os.ReadFile(parameter.MusicConfigFile); err == nil {
		config.PatternTOML = data
	}

	audioEngine, err := NewAudioEngine(config)
	if err != nil {
		s.disabled.Store(true)
		return nil
	}
	s.audioEngine = audioEngine
	return nil
}

// Start launches the engine; backend absence is degradation, not failure
func (s *AudioService) Start() error {
	if s.disabled.Load() || s.audioEngine == nil {
		return nil
	}
	// Error deliberately dropped until logging lands; engine stays
	// alive in silent mode — published AudioPlayer remains valid
	_ = s.audioEngine.Start()
	return nil
}

// Stop implements Service
func (s *AudioService) Stop() error {
	if s.audioEngine != nil && s.audioEngine.IsRunning() {
		s.audioEngine.Stop()
	}
	return nil
}

// Contribute implements service.ResourceContributor
// Publishes AudioResource if initialization succeeded
func (s *AudioService) Contribute(publish service.ResourcePublisher) {
	if player := s.Player(); player != nil {
		publish(&engine.AudioResource{Player: player, Telemetry: s.audioEngine})
	}
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
	return s.audioEngine
}

// Player returns an AudioPlayer interface for game systems
// Returns nil if audio is disabled
func (s *AudioService) Player() engine.AudioPlayer {
	if s.disabled.Load() || s.audioEngine == nil {
		return nil
	}
	return s.audioEngine
}

// Resource returns AudioResource for ECS bridge (nil if disabled)
func (s *AudioService) Resource() *engine.AudioResource {
	if s.disabled.Load() || s.audioEngine == nil {
		return nil
	}
	return &engine.AudioResource{Player: s.audioEngine}
}
