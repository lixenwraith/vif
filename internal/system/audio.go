//go:build !vif_headless && !vif_noaudio && !wasm

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
		event.EventGameResetRequest,
	}
}

// HandleEvent processes sound request events
func (s *AudioSystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
	case event.EventGameResetRequest:
		s.Init()
		return

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
			s.player.Play(p.ID) // payload carries audio.SoundType
		}
	}
}

// Apply the preference through scenario gates; MusicSystem owns sequencer control.
func (s *AudioSystem) applyMask(m uint8) {
	s.mask = m
	if s.player == nil {
		return
	}
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

// HandOverSound moves the speakers from a presented world to a copy rebuilt to
// replace it, which replayed without them. The engine plays on rather than
// rewinding, so the viewer's preference and the conductor's state go with it; the
// gates the copy replayed then decide what sounds.
func HandOverSound(to, from *engine.World) {
	ta, fa := systemOf[*AudioSystem](to), systemOf[*AudioSystem](from)
	if ta == nil || fa == nil || fa.player == nil {
		return
	}
	to.Resources.Audio, from.Resources.Audio = from.Resources.Audio, nil
	ta.player, ta.mask, ta.initialized = fa.player, fa.mask, fa.initialized
	ta.basePlayed, ta.baseDropped, ta.baseReject = fa.basePlayed, fa.baseDropped, fa.baseReject
	fa.player = nil
	m := ta.gated()
	ta.player.SetEffectMuted(m&parameter.AudioChanEffects == 0)
	publishAudioMask(to, ta.player)

	tm, fm := systemOf[*MusicSystem](to), systemOf[*MusicSystem](from)
	if tm == nil || fm == nil {
		return
	}
	tm.player, tm.bpmF, tm.lastBPM, tm.tier = fm.player, fm.bpmF, fm.lastBPM, fm.tier
	tm.manualTier, tm.arranged, tm.stopped, tm.startPending = fm.manualTier, fm.arranged, fm.stopped, fm.startPending
	fm.player = nil
	tm.applyMusicAudible(m&parameter.AudioChanMusic != 0)
}

// systemOf finds a world's system of type T, nil when it has none.
func systemOf[T engine.System](w *engine.World) T {
	var none T
	for _, s := range w.Systems() {
		if t, ok := s.(T); ok {
			return t
		}
	}
	return none
}

// Both controllers publish after device changes, including while gameplay is paused.
func publishAudioMask(world *engine.World, player *audio.AudioEngine) {
	m := parameter.AudioChanNone
	if player != nil && player.IsEnabled() {
		if !player.IsEffectMuted() {
			m |= parameter.AudioChanEffects
		}
		if !player.IsMusicMuted() && player.IsMusicPlaying() {
			m |= parameter.AudioChanMusic
		}
	}
	reg := world.Resources.Status
	mask := int64(m)
	if player == nil {
		mask = -1
	}
	reg.Ints.Get("audio.mask").Store(mask)
	reg.Bools.Get("audio.effect_muted").Store(m&parameter.AudioChanEffects == 0)
	reg.Bools.Get("audio.music_muted").Store(m&parameter.AudioChanMusic == 0)
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
