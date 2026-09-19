//go:build vif_headless || vif_noaudio || wasm

package system

import (
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// unavailableAudioSystem absorbs local audio events without linking the audio
// engine. Keeping the system identity makes audio-free builds preserve event
// telemetry and runtime system controls.
type unavailableAudioSystem struct {
	world *engine.World
}

func NewAudioSystem(world *engine.World) engine.System {
	s := &unavailableAudioSystem{world: world}
	s.Init()
	return s
}

func (s *unavailableAudioSystem) Init() {
	s.world.Resources.Status.Ints.Get("audio.mask").Store(-1)
}

func (*unavailableAudioSystem) Name() string  { return "audio" }
func (*unavailableAudioSystem) Priority() int { return parameter.PriorityUI }
func (*unavailableAudioSystem) Update()       {}

func (*unavailableAudioSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventSoundRequest,
		event.EventGamePauseChanged,
		event.EventSoundMuteToggle,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

func (s *unavailableAudioSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameResetRequest {
		s.Init()
	}
}

type unavailableMusicSystem struct{}

func NewMusicSystem(*engine.World) engine.System { return &unavailableMusicSystem{} }

func (*unavailableMusicSystem) Init()         {}
func (*unavailableMusicSystem) Name() string  { return "music" }
func (*unavailableMusicSystem) Priority() int { return parameter.PriorityUI + 1 }
func (*unavailableMusicSystem) Update()       {}

func (*unavailableMusicSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventMusicStart,
		event.EventMusicStop,
		event.EventAudioMuteChanged,
		event.EventBeatPatternRequest,
		event.EventMelodyNoteRequest,
		event.EventMelodyPatternRequest,
		event.EventMusicIntensityChange,
		event.EventMusicTempoChange,
		event.EventMusicSeedRequest,
		event.EventMusicSwingRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

func (*unavailableMusicSystem) HandleEvent(event.GameEvent) {}
