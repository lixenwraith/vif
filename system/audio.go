package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/audio"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/status"
)

// AudioSystem consumes sound request events and plays audio
// Decouples game systems from direct AudioEngine access
type AudioSystem struct {
	world  *engine.World
	player *audio.AudioEngine

	mask    uint8 // parameter.AudioChan* bits; set = audible
	enabled bool

	// Cached registry pointers + telemetry
	statBackend *status.AtomicString
	statSilent  *atomic.Bool
	statPlayed  *atomic.Int64
	statDropped *atomic.Int64
	statMask    *atomic.Int64 // -1 = audio unavailable
	statEffMute *atomic.Bool  // derived from mask; debug-overlay readability
	statMusMute *atomic.Bool
}

// NewAudioSystem creates an audio system with the given player
// player may be nil if audio is disabled
func NewAudioSystem(world *engine.World) engine.System {
	s := &AudioSystem{world: world}
	if r := world.Resources.Audio; r != nil {
		s.player = r.Engine // nil resource or nil engine = audio unavailable
	}

	reg := world.Resources.Status
	s.statBackend = reg.Strings.Get("audio.backend")
	s.statSilent = reg.Bools.Get("audio.silent")
	s.statPlayed = reg.Ints.Get("audio.played")
	s.statDropped = reg.Ints.Get("audio.dropped")
	s.statMask = reg.Ints.Get("audio.mask")
	s.statEffMute = reg.Bools.Get("audio.effect_muted")
	s.statMusMute = reg.Bools.Get("audio.music_muted")

	s.Init()
	return s
}

// Init re-seeds from the engine rather than forcing a default, so :new
// preserves the player's mute choice.
func (s *AudioSystem) Init() {
	s.enabled = true
	if s.player == nil {
		s.statMask.Store(-1) // audio unavailable; renderers skip the indicator
		return
	}
	s.mask = parameter.AudioChanNone
	if !s.player.IsEffectMuted() {
		s.mask |= parameter.AudioChanEffects
	}
	if !s.player.IsMusicMuted() {
		s.mask |= parameter.AudioChanMusic
	}
	s.publishMask()
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
		event.EventGamePauseChanged,
		event.EventSoundMuteToggle,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

// HandleEvent processes sound request events
func (s *AudioSystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
	case event.EventGameReset:
		s.Init()
		return

	case event.EventMetaSystemCommandRequest:
		if p, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok && p.SystemName == s.Name() {
			s.enabled = p.Enabled
		}
		return
	}

	if s.player == nil {
		return
	}

	// Device state, not gameplay: pause and mute apply regardless of enabled.
	// ":system audio disable" silences gameplay sound; it must not detach the
	// mixer from pause or leave the mask lying to the status bar.
	switch ev.Type {
	case event.EventGamePauseChanged:
		if p, ok := ev.Payload.(*event.GamePausePayload); ok {
			s.player.SetPaused(p.Paused)
		}
		return

	case event.EventSoundMuteToggle:
		next := parameter.AudioMaskCycle(s.mask)
		if p, ok := ev.Payload.(*event.SoundMuteTogglePayload); ok {
			switch p.Mode {
			case event.MuteToggle:
				next = s.mask ^ (p.Mask & parameter.AudioChanAll)
			case event.MuteSet:
				next = p.Mask & parameter.AudioChanAll
			}
		}
		s.applyMask(next)
		return
	}

	if !s.enabled {
		return
	}

	if ev.Type == event.EventSoundRequest {
		if p, ok := ev.Payload.(*event.SoundRequestPayload); ok {
			s.player.Play(p.SoundType) // payload carries audio.SoundType
		}
	}
}

// applyMask applies the effects channel and announces the composed result.
// The music bit is applied by MusicSystem so sequencer start/stop stays in one
// place; this system never calls StartMusic/StopMusic. The effects store is
// idempotent, so it runs unconditionally rather than branching per bit.
func (s *AudioSystem) applyMask(m uint8) {
	if m == s.mask {
		return
	}
	s.mask = m
	s.player.SetEffectMuted(m&parameter.AudioChanEffects == 0)
	s.publishMask()
	s.world.PushEvent(event.EventAudioMuteChanged, &event.AudioMuteChangedPayload{Mask: m})
}

// publishMask writes on state change, not per tick: mute changes while paused,
// when Update does not run.
func (s *AudioSystem) publishMask() {
	s.statMask.Store(int64(s.mask))
	s.statEffMute.Store(s.mask&parameter.AudioChanEffects == 0)
	s.statMusMute.Store(s.mask&parameter.AudioChanMusic == 0)
}

// Update publishes engine telemetry; mask state is event-driven
func (s *AudioSystem) Update() {
	// Dropped disabled early return to report telemetry
	if s.player == nil {
		return
	}
	p, d := s.player.Stats()
	s.statPlayed.Store(int64(p))
	s.statDropped.Store(int64(d))
	s.statBackend.Store(s.player.BackendName())
	s.statSilent.Store(s.player.IsSilent())
}
