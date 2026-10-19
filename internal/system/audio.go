//go:build !vif_headless && !vif_noaudio

package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/status"
	"github.com/lixenwraith/vif/pkg/audio"
)

// AudioSystem consumes sound request events and plays audio
// Decouples game systems from direct AudioEngine access
type AudioSystem struct {
	world  *engine.World
	player *audio.AudioEngine

	mask uint8 // Player preference, retained while scenario channels are disabled.
	toggle
	musicEnabled bool
	initialized  bool

	// Cached registry pointers + telemetry
	statBackend *status.AtomicString
	statSilent  *atomic.Bool
	statPlayed  *atomic.Int64
	statDropped *atomic.Int64
	statReject  [audio.RejectCount]*atomic.Int64
	basePlayed  uint64
	baseDropped uint64
	baseReject  [audio.RejectCount]uint64
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
	reg.Ints.Get("audio.mask")
	reg.Bools.Get("audio.effect_muted")
	reg.Bools.Get("audio.music_muted")
	for i, n := range audio.RejectNames() {
		s.statReject[i] = reg.Ints.Get("audio.rej_" + n)
	}

	s.Init()
	return s
}

// Reset preserves the player's preference even when scenario gates muted the engine.
func (s *AudioSystem) Init() {
	s.enabled = true
	s.musicEnabled = true
	s.statBackend.Store("-")
	s.statSilent.Store(true)
	s.statPlayed.Store(0)
	s.statDropped.Store(0)
	for _, stat := range s.statReject {
		stat.Store(0)
	}
	if s.player == nil {
		publishAudioMask(s.world, nil)
		return
	}
	s.basePlayed, s.baseDropped = s.player.Stats()
	s.baseReject = s.player.Rejections()
	if !s.initialized {
		if !s.player.IsEffectMuted() {
			s.mask |= parameter.AudioChanEffects
		}
		if !s.player.IsMusicMuted() {
			s.mask |= parameter.AudioChanMusic
		}
		s.initialized = true
	}
	s.world.Resources.Audio.Pref.Store(int32(s.mask))
	// Initial playback waits for MusicSystem's update after scenario gates settle.
	s.player.SetEffectMuted(s.mask&parameter.AudioChanEffects == 0)
	s.player.SetMusicMuted(s.mask&parameter.AudioChanMusic == 0)
	publishAudioMask(s.world, s.player)
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
	}
}

// HandleEvent processes sound request events
func (s *AudioSystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {

	case event.EventMetaSystemCommandRequest:
		if p, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			switch p.SystemName {
			case s.Name():
				s.enabled = p.Enabled
			case "music":
				s.musicEnabled = p.Enabled
			default:
				return
			}
			s.applyMask(s.mask)
		}
		return
	}

	if s.player == nil {
		return
	}

	// Pause and preferences remain responsive while scenario gates silence playback.
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
			// A configured payload is shared by every emit, so the name resolves into a local
			id := p.ID
			if id == audio.SoundNone && p.Name != "" {
				id = audio.SoundIDByName(p.Name)
			}
			s.player.Play(id)
		}
	}
}

// Apply the preference through scenario gates; MusicSystem owns sequencer control.
func (s *AudioSystem) applyMask(m uint8) {
	s.mask = m
	if s.player == nil {
		return
	}
	s.world.Resources.Audio.Pref.Store(int32(m))
	m = s.gated()
	s.player.SetEffectMuted(m&parameter.AudioChanEffects == 0)
	// Close the music bus immediately; MusicSystem owns sequencer start and resume.
	if m&parameter.AudioChanMusic == 0 {
		s.player.SetMusicMuted(true)
	}
	publishAudioMask(s.world, s.player)
	s.world.PushEvent(event.EventAudioMuteChanged, &event.AudioMuteChangedPayload{Mask: m})
}

// gated is the player's preference through the scenario's system gates.
func (s *AudioSystem) gated() uint8 {
	m := s.mask
	if !s.enabled {
		m = parameter.AudioChanNone
	}
	if !s.musicEnabled {
		m &^= parameter.AudioChanMusic
	}
	return m
}

// takeOver moves the speakers and the viewer's preference from the audio system of
// the world this one replaces, and returns the gated mask the music bus follows;
// ok is false when the old world held no speakers.
func (s *AudioSystem) takeOver(from *AudioSystem) (m uint8, ok bool) {
	if from.player == nil {
		return 0, false
	}
	s.world.Resources.Audio, from.world.Resources.Audio = from.world.Resources.Audio, nil
	s.player, s.mask, s.initialized = from.player, from.mask, from.initialized
	s.basePlayed, s.baseDropped, s.baseReject = from.basePlayed, from.baseDropped, from.baseReject
	from.player = nil
	s.world.Resources.Audio.Pref.Store(int32(s.mask))
	m = s.gated()
	s.player.SetEffectMuted(m&parameter.AudioChanEffects == 0)
	publishAudioMask(s.world, s.player)
	return m, true
}

// Mixer commands settle asynchronously, so refresh the audible mask with telemetry.
func (s *AudioSystem) Update() {
	publishAudioMask(s.world, s.player)
	if s.player == nil {
		return
	}
	p, d := s.player.Stats()
	s.statPlayed.Store(int64(counterDelta(p, s.basePlayed)))
	s.statDropped.Store(int64(counterDelta(d, s.baseDropped)))
	r := s.player.Rejections()
	for i := range r {
		s.statReject[i].Store(int64(counterDelta(r[i], s.baseReject[i])))
	}
	s.statBackend.Store(s.player.BackendName())
	s.statSilent.Store(s.player.IsSilent())
}

// counterDelta reports a session delta and tolerates a backend counter reset.
func counterDelta(current, baseline uint64) uint64 {
	if current < baseline {
		return current
	}
	return current - baseline
}
