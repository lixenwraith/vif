package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/status"
)

// AudioSystem consumes sound request events and plays audio
// Decouples game systems from direct AudioEngine access
type AudioSystem struct {
	world  *engine.World
	player engine.AudioPlayer

	enabled bool

	// Cached registry pointers + telemetry
	telemetry   engine.AudioTelemetry
	statBackend *status.AtomicString
	statSilent  *atomic.Bool
	statPlayed  *atomic.Int64
	statDropped *atomic.Int64
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

	if world.Resources.Audio != nil {
		s.telemetry = world.Resources.Audio.Telemetry
	}
	reg := world.Resources.Status
	s.statBackend = reg.Strings.Get("audio.backend")
	s.statSilent = reg.Bools.Get("audio.silent")
	s.statPlayed = reg.Ints.Get("audio.played")
	s.statDropped = reg.Ints.Get("audio.dropped")

	s.Init()
	return s
}

// Init resets session state for new game
func (s *AudioSystem) Init() {
	s.enabled = true
}

// Name returns system's name
func (s *AudioSystem) Name() string {
	return "audio"
}

// Priority returns the system's priority
func (s *AudioSystem) Priority() int {
	return parameter.PriorityUI
}

// EventTypes returns the event types AudioSystem handles
func (s *AudioSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventSoundRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

// HandleEvent processes sound request events
func (s *AudioSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
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

// Update implements System interface
func (s *AudioSystem) Update() {
	if !s.enabled || s.telemetry == nil {
		return
	}
	p, d := s.telemetry.Stats()
	s.statPlayed.Store(int64(p))
	s.statDropped.Store(int64(d))
	s.statBackend.Store(s.telemetry.BackendName())
	s.statSilent.Store(s.telemetry.IsSilent())
}

