package system

import (
	"time"

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
	beat   core.PatternID
	melody core.PatternID
}

var tierArrangements = [core.IntensityPeak + 1]arrangement{
	core.IntensityCalm:     {core.PatternBeatBasic, core.PatternMelodyHold},
	core.IntensityNormal:   {core.PatternBeatDriving, core.PatternMelodyHold},
	core.IntensityElevated: {core.PatternBeatDriving, core.PatternMelodyArpUp},
	core.IntensityIntense:  {core.PatternBeatIntense, core.PatternMelodyArpUp},
	core.IntensityPeak:     {core.PatternBeatIntense, core.PatternMelodyChord},
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
	player engine.AudioPlayer

	lastBPM    int
	tier       core.MusicIntensity
	manualTier bool // explicit intensity/pattern events suspend APM auto-tier

	enabled bool
}

// NewMusicSystem creates a music system
func NewMusicSystem(world *engine.World) engine.System {
	var player engine.AudioPlayer
	if world.Resources.Audio != nil {
		player = world.Resources.Audio.Player
	}

	s := &MusicSystem{
		world:  world,
		player: player,
	}
	s.Init()
	return s
}

// Init resets session state
func (s *MusicSystem) Init() {
	s.lastBPM = 0
	s.tier = core.IntensityCalm
	s.manualTier = false
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
		event.EventMusicPause,
		event.EventBeatPatternRequest,
		event.EventMelodyNoteRequest,
		event.EventMelodyPatternRequest,
		event.EventMusicIntensityChange,
		event.EventMusicTempoChange,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

// applyArrangement pushes the current tier's patterns and default harmony
func (s *MusicSystem) applyArrangement(quantize bool) {
	arr := tierArrangements[s.tier]
	fade := int(parameter.PatternTransitionDefault.Seconds() * float64(parameter.AudioSampleRate))
	s.player.SetHarmony(parameter.DefaultRootNote, core.ScalePhrygian, nil)
	s.player.SetPattern(slotRhythm, arr.beat, fade, quantize)
	s.player.SetPattern(slotMelody, arr.melody, fade, quantize)
}

func (s *MusicSystem) startMusic() {
	apm := s.world.Resources.Game.State.GetMusicAPM()
	bpm := parameter.APMToBPM(apm)
	s.player.SetMusicBPM(bpm)
	s.lastBPM = bpm
	s.applyArrangement(false)
	s.player.StartMusic()
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

	if !s.enabled || s.player == nil {
		return
	}

	switch ev.Type {
	case event.EventMusicStart:
		if payload, ok := ev.Payload.(*event.MusicStartPayload); ok {
			if payload.BPM > 0 {
				s.player.SetMusicBPM(payload.BPM)
				s.lastBPM = payload.BPM
			}
			s.player.SetHarmony(parameter.DefaultRootNote, core.ScalePhrygian, nil)
			if payload.BeatPattern != core.PatternSilence {
				s.player.SetPattern(slotRhythm, payload.BeatPattern, 0, false)
				s.manualTier = true
			}
			if payload.MelodyPattern != core.PatternSilence {
				s.player.SetPattern(slotMelody, payload.MelodyPattern, 0, false)
				s.manualTier = true
			}
			s.player.StartMusic()
		}

	case event.EventMusicStop:
		s.player.StopMusic()

	case event.EventMusicPause:
		// Router mute toggle routes here; MusicSystem owns start/stop logic
		if s.player.ToggleMusicMute() {
			s.startMusic()
		}
		// mute path: SetMusicMuted already issued music stop

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
			duration := int(payload.Duration.Seconds() * float64(parameter.AudioSampleRate))
			if duration == 0 {
				duration = parameter.SamplesPerStep(parameter.DefaultBPM) * 2
			}
			instr := payload.Instrument
			if instr == 0 {
				instr = core.InstrPiano
			}
			s.player.TriggerMelodyNote(payload.Note, payload.Velocity, duration, instr)
		}

	case event.EventMusicIntensityChange:
		if payload, ok := ev.Payload.(*event.MusicIntensityPayload); ok {
			if payload.Intensity >= 0 && payload.Intensity <= core.IntensityPeak {
				s.tier = payload.Intensity
				s.manualTier = true
				if s.player.IsMusicPlaying() {
					s.applyArrangement(true)
				}
			}
		}

	case event.EventMusicTempoChange:
		if payload, ok := ev.Payload.(*event.MusicTempoPayload); ok {
			s.player.SetMusicBPM(payload.BPM)
			s.lastBPM = payload.BPM
		}
	}
}

// crossfade converts a transition duration to samples; 0 = default
func (s *MusicSystem) crossfade(t time.Duration) int {
	if t == 0 {
		t = parameter.PatternTransitionDefault
	}
	return int(t.Seconds() * float64(parameter.AudioSampleRate))
}

// Update implements System interface
func (s *MusicSystem) Update() {
	if !s.enabled || s.player == nil || !s.player.IsMusicPlaying() {
		return
	}

	apm := s.world.Resources.Game.State.GetMusicAPM()

	bpm := parameter.APMToBPM(apm)
	if bpm != s.lastBPM {
		s.player.SetMusicBPM(bpm)
		s.lastBPM = bpm
	}

	if !s.manualTier {
		if tier := tierForAPM(apm); tier != s.tier {
			s.tier = tier
			s.applyArrangement(true) // bar-quantized tier shift
		}
	}
}
