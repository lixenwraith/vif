package system

import (
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
)

// AudioSystem consumes sound request events and plays audio
// Decouples game systems from direct AudioEngine access
type AudioSystem struct {
	world  *engine.World
	player engine.AudioPlayer

	enabled bool
}

// NewAudioSystem creates an audio system with the given player
// player may be nil if audio is disabled
func NewAudioSystem(world *engine.World) engine.System {
	var player engine.AudioPlayer
	if world.Resources.Audio != nil {
		player = world.Resources.Audio.Player
	}

	s := &AudioSystem{
		world:  world,
		player: player,
	}
	s.Init()
	return s
}

// Init resets session state for new game
func (s *AudioSystem) Init() {
	s.enabled = true
}

// Priority returns the system's priority
func (s *AudioSystem) Priority() int {
	return constant.PriorityUI
}

// EventTypes returns the event types AudioSystem handles
func (s *AudioSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventGameReset,
		event.EventSoundRequest,
	}
}

// HandleEvent processes sound request events
func (s *AudioSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if !s.enabled {
		return
	}

	if s.player == nil {
		return
	}
	if ev.Type == event.EventSoundRequest {
		if payload, ok := ev.Payload.(*event.SoundRequestPayload); ok {
			s.player.Play(payload.SoundType)
		}
	}
}

// Update implements System interface (no tick-based logic)
func (s *AudioSystem) Update() {
	if !s.enabled {
		return
	}
}