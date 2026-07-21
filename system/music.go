package system

import (
	"time"

	"github.com/lixenwraith/vi-fighter/audio"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
)

const (
	slotRhythm = 0
	slotMelody = 1
)

type arrangement struct {
	beat   audio.PatternID
	melody audio.PatternID
}

var tierArrangements = [core.IntensityPeak + 1]arrangement{
	core.IntensityCalm:     {audio.PatternBeatBasic, audio.PatternMelodyHold},
	core.IntensityNormal:   {audio.PatternBeatDriving, audio.PatternMelodyHold},
	core.IntensityElevated: {audio.PatternBeatDrivingPlus, audio.PatternMelodyArpUp},
	core.IntensityIntense:  {audio.PatternBeatIntense, audio.PatternMelodyArpUp},
	core.IntensityPeak:     {audio.PatternBeatIntense, audio.PatternMelodyGen},
}

func tierForAPM(apm uint64) core.MusicIntensity {
	switch {
	case apm < parameter.TierNormalAPM:
		return core.IntensityCalm
	case apm < parameter.TierElevatedAPM:
		return core.IntensityNormal
	case apm < parameter.TierIntenseAPM:
		return core.IntensityElevated
	case apm < parameter.TierPeakAPM:
		return core.IntensityIntense
	default:
		return core.IntensityPeak
	}
}

// MusicSystem is the conductor: maps game state to arrangement commands
type MusicSystem struct {
	world  *engine.World
	player *audio.AudioEngine

	bpmF       float64 // slewed tempo state; drifts toward APM target
	lastBPM    int
	tier       core.MusicIntensity
	manualTier bool
	arranged   bool // first auto-arrangement applied; slots start silent otherwise

	enabled bool
}

// NewMusicSystem creates a music system
func NewMusicSystem(world *engine.World) engine.System {
	s := &MusicSystem{world: world}
	if world.Resources.Audio != nil {
		s.player = world.Resources.Audio.Engine
	}
	s.Init()
	return s
}

// Init resets session state
func (s *MusicSystem) Init() {
	s.bpmF = float64(parameter.APMToBPM(0))
	s.lastBPM = 0
	s.tier = core.IntensityCalm
	s.manualTier = false
	s.arranged = false
	s.enabled = true
	if s.player != nil {
		s.player.ResetMusic()
	}
}

// Name returns system name
func (s *MusicSystem) Name() string {
	return "music"
}

// Priority returns system priority
func (s *MusicSystem) Priority() int {
	return parameter.PriorityUI + 1
}

// EventTypes returns handled event types
func (s *MusicSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventMusicStart,
		event.EventMusicStop,
		event.EventAudioMuteChanged,
		event.EventBeatPatternRequest,
		event.EventMelodyNoteRequest,
		event.EventMelodyPatternRequest,
		event.EventMusicIntensityChange,
		event.EventMusicTempoChange,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

// HandleEvent processes music events
func (s *MusicSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		wasPlaying := s.player != nil && s.player.IsMusicPlaying()
		s.Init()
		if wasPlaying {
			s.startMusic() // restart after :new; sequencer state was cleared
		}
		return
	}

	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
		return
	}

	if s.player == nil {
		return
	}

	// Device state, not gameplay: applied even when disabled, or
	// ":system music disable" leaves the mask claiming music is audible
	// while the engine stays muted.
	if ev.Type == event.EventAudioMuteChanged {
		if p, ok := ev.Payload.(*event.AudioMuteChangedPayload); ok {
			s.applyMusicAudible(p.Mask&parameter.AudioChanMusic != 0)
		}
		return
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventMusicStart:
		if payload, ok := ev.Payload.(*event.MusicStartPayload); ok {
			if payload.BPM > 0 {
				s.player.SetMusicBPM(payload.BPM)
				s.lastBPM = payload.BPM
				s.bpmF = float64(payload.BPM) // slew departs from manual tempo
			}
			s.player.SetHarmony(parameter.DefaultRootNote, audio.ScalePhrygian, nil)
			if payload.BeatPattern != audio.PatternSilence {
				s.player.SetPattern(slotRhythm, payload.BeatPattern, 0, false)
				s.manualTier = true
			}
			if payload.MelodyPattern != audio.PatternSilence {
				s.player.SetPattern(slotMelody, payload.MelodyPattern, 0, false)
				s.manualTier = true
			}
			s.player.StartMusic()
		}

	case event.EventMusicStop:
		s.player.StopMusic()

	case event.EventAudioMuteChanged:
		if p, ok := ev.Payload.(*event.AudioMuteChangedPayload); ok {
			audible := p.Mask&parameter.AudioChanMusic != 0
			if audible == !s.player.IsMusicMuted() {
				return
			}
			s.player.SetMusicMuted(!audible)
			if audible {
				s.startMusic() // SetMusicMuted(true) issued cmdMusicStop
			}
		}

	case event.EventBeatPatternRequest:
		if payload, ok := ev.Payload.(*event.BeatPatternRequestPayload); ok {
			s.player.SetPattern(slotRhythm, payload.Pattern, s.crossfade(payload.TransitionTime), payload.Quantize)
			s.manualTier = true
		}

	case event.EventMelodyPatternRequest:
		if payload, ok := ev.Payload.(*event.MelodyPatternRequestPayload); ok {
			if payload.RootNote > 0 {
				s.player.SetHarmony(payload.RootNote, -1, nil) // keep scale/progression
			}
			s.player.SetPattern(slotMelody, payload.Pattern, s.crossfade(payload.TransitionTime), payload.Quantize)
			s.manualTier = true
		}

	case event.EventMelodyNoteRequest:
		if payload, ok := ev.Payload.(*event.MelodyNoteRequestPayload); ok {
			duration := int(payload.Duration.Seconds() * float64(audio.AudioSampleRate))
			if duration == 0 {
				duration = audio.SamplesPerStep(audio.DefaultBPM) * 2
			}
			instr := payload.Instrument
			if instr == 0 {
				instr = audio.InstrPiano
			}
			s.player.TriggerMelodyNote(payload.Note, payload.Velocity, duration, instr)
		}

	case event.EventMusicIntensityChange:
		if payload, ok := ev.Payload.(*event.MusicIntensityPayload); ok {
			if payload.Intensity >= 0 && payload.Intensity <= core.IntensityPeak {
				rising := payload.Intensity > s.tier // fall uses default fade, no reveal
				s.tier = payload.Intensity
				s.manualTier = true
				if s.player.IsMusicPlaying() {
					s.applyArrangement(true, rising)
				}
			}
		}

	case event.EventMusicTempoChange:
		if payload, ok := ev.Payload.(*event.MusicTempoPayload); ok {
			s.player.SetMusicBPM(payload.BPM)
			s.lastBPM = payload.BPM
			s.bpmF = float64(payload.BPM)
		}
	}
}

// applyMusicAudible mutes or unmutes the music bus. SetMusicMuted(true) issues
// a sequencer stop, so unmuting must restart; startMusic resyncs tempo and
// arrangement to current APM before StartMusic. Same polarity the old
// EventMusicPause path relied on, driven by the mask instead of a toggle.
func (s *MusicSystem) applyMusicAudible(audible bool) {
	if audible == !s.player.IsMusicMuted() {
		return
	}
	s.player.SetMusicMuted(!audible)
	if audible {
		s.startMusic()
	}
}

// Update implements System interface
func (s *MusicSystem) Update() {
	if !s.enabled || s.player == nil || !s.player.IsMusicPlaying() {
		return
	}
	s.syncToAPM()
}

// applyArrangement: rising tier shifts use the slow crossfade — spans ≥1 bar
// at ≥120 BPM, which triggers the sequencer's per-bar track reveal (build-up)
func (s *MusicSystem) applyArrangement(quantize, rising bool) {
	arr := tierArrangements[s.tier]
	ft := parameter.PatternTransitionDefault
	if rising {
		ft = parameter.PatternTransitionSlow
	}
	fade := int(ft.Seconds() * float64(audio.AudioSampleRate))
	s.player.SetHarmony(parameter.DefaultRootNote, audio.ScalePhrygian, nil)
	s.player.SetPattern(slotRhythm, arr.beat, fade, quantize)
	s.player.SetPattern(slotMelody, arr.melody, fade, quantize)
}

func (s *MusicSystem) startMusic() {
	s.syncToAPM()
	s.player.StartMusic()
}

// syncToAPM slews tempo toward the APM target and applies auto-tier shifts
// Single path for startMusic and Update — Update previously bypassed hysteresis
func (s *MusicSystem) syncToAPM() {
	apm := s.world.Resources.Game.State.GetMusicAPM()

	target := float64(parameter.APMToBPM(apm))
	dt := s.world.Resources.Time.DeltaTime.Seconds()
	if target > s.bpmF {
		s.bpmF = min(target, s.bpmF+parameter.BPMRiseRate*dt)
	} else if target < s.bpmF {
		s.bpmF = max(target, s.bpmF-parameter.BPMFallRate*dt)
	}
	bpm := int(s.bpmF + 0.5)
	if d := bpm - s.lastBPM; d >= parameter.BPMHysteresis || -d >= parameter.BPMHysteresis {
		s.player.SetMusicBPM(bpm) // bar-quantized at the sequencer
		s.lastBPM = bpm
	}

	if !s.manualTier {
		tier := tierForAPM(apm)
		if tier != s.tier || !s.arranged {
			rising := s.arranged && tier > s.tier // computed before overwrite; first arrangement is not a build-up
			s.tier = tier
			s.arranged = true
			s.applyArrangement(true, rising)
		}
	}
}

// crossfade converts a transition duration to samples; 0 = default
func (s *MusicSystem) crossfade(t time.Duration) int {
	if t == 0 {
		t = parameter.PatternTransitionDefault
	}
	return int(t.Seconds() * float64(audio.AudioSampleRate))
}
