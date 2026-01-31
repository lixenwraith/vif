package system

import (
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// MusicSystem handles music events and drives the sequencer
type MusicSystem struct {
	world  *engine.World
	player engine.AudioPlayer

	lastBPM int // Tracking changes

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

// HandleEvent processes music events
func (s *MusicSystem) HandleEvent(ev event.GameEvent) {
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
		return
	}

	if !s.enabled || s.player == nil {
		return
	}

	switch ev.Type {
	case event.EventMusicStart:
		if payload, ok := ev.Payload.(*event.MusicStartPayload); ok {
			s.player.SetMusicBPM(payload.BPM)
			if payload.BeatPattern != core.PatternSilence {
				s.player.SetBeatPattern(payload.BeatPattern, 0, false)
			}
			if payload.MelodyPattern != core.PatternSilence {
				s.player.SetMelodyPattern(payload.MelodyPattern, parameter.MIDINote(parameter.NoteC, parameter.OctaveMid), 0, false)
			}
			s.player.StartMusic()
		}

	case event.EventMusicStop:
		s.player.StopMusic()

	case event.EventMusicPause:
		if s.player.IsMusicPlaying() {
			s.player.StopMusic()
		} else {
			s.player.StartMusic()
		}

	case event.EventBeatPatternRequest:
		if payload, ok := ev.Payload.(*event.BeatPatternRequestPayload); ok {
			crossfade := int(payload.TransitionTime.Seconds() * float64(parameter.AudioSampleRate))
			if crossfade == 0 {
				crossfade = int(parameter.PatternTransitionDefault.Seconds() * float64(parameter.AudioSampleRate))
			}
			s.player.SetBeatPattern(payload.Pattern, crossfade, payload.Quantize)
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

	case event.EventMelodyPatternRequest:
		if payload, ok := ev.Payload.(*event.MelodyPatternRequestPayload); ok {
			crossfade := int(payload.TransitionTime.Seconds() * float64(parameter.AudioSampleRate))
			if crossfade == 0 {
				crossfade = int(parameter.PatternTransitionDefault.Seconds() * float64(parameter.AudioSampleRate))
			}
			s.player.SetMelodyPattern(payload.Pattern, payload.RootNote, crossfade, payload.Quantize)
		}

	case event.EventMusicTempoChange:
		if payload, ok := ev.Payload.(*event.MusicTempoPayload); ok {
			s.player.SetMusicBPM(payload.BPM)
		}
	}
}

// Update implements System interface
func (s *MusicSystem) Update() {
	if !s.enabled || s.player == nil {
		return
	}

	if !s.player.IsMusicPlaying() {
		return
	}

	apm := s.world.Resources.Game.State.GetMusicAPM()
	bpm := parameter.APMToBPM(apm)

	if bpm != s.lastBPM {
		s.player.SetMusicBPM(bpm)
		s.lastBPM = bpm
	}
}