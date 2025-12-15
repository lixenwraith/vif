package systems

import (
	"time"

	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/events"
)

// AudioSystem consumes sound request events and plays audio
// Decouples game systems from direct AudioEngine access
type AudioSystem struct {
	player engine.AudioPlayer
}

// NewAudioSystem creates an audio system with the given player
// player may be nil if audio is disabled
func NewAudioSystem(player engine.AudioPlayer) *AudioSystem {
	return &AudioSystem{player: player}
}

// Priority returns the system's priority (runs with UI systems)
func (s *AudioSystem) Priority() int {
	return 50 // Same as PriorityUI
}

// EventTypes returns the event types AudioSystem handles
func (s *AudioSystem) EventTypes() []events.EventType {
	return []events.EventType{
		events.EventSoundRequest,
	}
}

// HandleEvent processes sound request events
func (s *AudioSystem) HandleEvent(world *engine.World, event events.GameEvent) {
	if s.player == nil {
		return
	}
	if event.Type == events.EventSoundRequest {
		if payload, ok := event.Payload.(*events.SoundRequestPayload); ok {
			s.player.Play(payload.SoundType)
		}
	}
}

// Update implements System interface (no tick-based logic)
func (s *AudioSystem) Update(world *engine.World, dt time.Duration) {}