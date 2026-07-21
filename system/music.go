package system

import (
	"time"

	"github.com/lixenwraith/vi-fighter/audio"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
)

const (
	slotRhythm = 0
	slotMelody = 1
)

var tierArrangements = [audio.IntensityCount]audio.Arrangement{
	audio.IntensityCalm:     {Rhythm: audio.PatternBeatBasic, Melody: audio.PatternMelodyHold},
	audio.IntensityNormal:   {Rhythm: audio.PatternBeatDriving, Melody: audio.PatternMelodyHold},
	audio.IntensityElevated: {Rhythm: audio.PatternBeatDrivingPlus, Melody: audio.PatternMelodyArpUp},
	audio.IntensityIntense:  {Rhythm: audio.PatternBeatIntense, Melody: audio.PatternMelodyArpUp},
	audio.IntensityPeak:     {Rhythm: audio.PatternBeatIntense, Melody: audio.PatternMelodyGen},
}

func tierForAPM(apm uint64) audio.Intensity {
	switch {
	case apm < parameter.TierNormalAPM:
		return audio.IntensityCalm
	case apm < parameter.TierElevatedAPM:
		return audio.IntensityNormal
	case apm < parameter.TierIntenseAPM:
		return audio.IntensityElevated
	case apm < parameter.TierPeakAPM:
		return audio.IntensityIntense
	default:
		return audio.IntensityPeak
	}
}

// MusicSystem is the conductor: maps game state to arrangement commands
type MusicSystem struct {
	world  *engine.World
	player *audio.AudioEngine

	bpmF       float64 // slewed tempo state; drifts toward APM target
	lastBPM    int
	tier       audio.Intensity
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
	s.tier = audio.IntensityCalm
	s.manualTier = false
	s.arranged = false
	s.enabled = true
	if s.player != nil {
		for t, a := range tierArrangements {
			s.player.SetArrangement(audio.Intensity(t), a)
		}
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
		event.EventMusicSeedRequest,
		event.EventMusicSwingRequest,
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
		if p, ok := ev.Payload.(*event.MusicStartPayload); ok && p != nil {
			if p.BPM > 0 {
				s.player.SetMusicBPM(p.BPM)
				s.lastBPM = p.BPM
				s.bpmF = float64(p.BPM) // slew departs from manual tempo
			}
			// > 0 only: IntensityCalm is indistinguishable from the zero value
			if p.Intensity > 0 && p.Intensity < audio.IntensityCount {
				s.tier = p.Intensity
				s.manualTier = true
			}
			s.startMusic()
			// CHANGED: explicit slots applied after the tier, not before
			if p.BeatPattern != audio.PatternSilence {
				s.player.SetPattern(slotRhythm, p.BeatPattern, 0, false)
				s.manualTier = true
			}
			if p.MelodyPattern != audio.PatternSilence {
				s.player.SetPattern(slotMelody, p.MelodyPattern, 0, false)
				s.manualTier = true
			}
			return
		}
		s.startMusic()

	case event.EventMusicStop:
		s.player.StopMusic()

	case event.EventBeatPatternRequest:
		if payload, ok := ev.Payload.(*event.BeatPatternRequestPayload); ok {
			s.player.SetPattern(slotRhythm, payload.Pattern, s.fadeSamples(payload.TransitionTime, false), payload.Quantize)
			s.manualTier = true
		}

	case event.EventMelodyPatternRequest:
		if payload, ok := ev.Payload.(*event.MelodyPatternRequestPayload); ok {
			if payload.RootNote > 0 {
				s.player.SetHarmony(payload.RootNote, -1, nil) // keep scale/progression
			}
			s.player.SetPattern(slotMelody, payload.Pattern, s.fadeSamples(payload.TransitionTime, false), payload.Quantize)
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
		if p, ok := ev.Payload.(*event.MusicIntensityPayload); ok {
			// negative tier releases the manual hold, resuming APM tracking
			if p.Intensity < 0 {
				s.manualTier = false
				return
			}
			if p.Intensity < audio.IntensityCount {
				rising := p.Intensity > s.tier
				s.tier = p.Intensity
				s.manualTier = true
				if s.player.IsMusicPlaying() {
					// TODO: test reveal (last arg) toggle impact on music
					s.applyArrangement(true, s.fadeSamples(p.TransitionTime, rising), true)
				}
			}
		}

	case event.EventMusicTempoChange:
		if payload, ok := ev.Payload.(*event.MusicTempoPayload); ok {
			s.player.SetMusicBPM(payload.BPM)
			s.lastBPM = payload.BPM
			s.bpmF = float64(payload.BPM)
		}

	case event.EventMusicSeedRequest:
		if p, ok := ev.Payload.(*event.MusicSeedPayload); ok {
			seed := p.Seed
			if seed == 0 {
				seed = s.world.Resources.Time.RealTimeNano()
			}
			s.player.SetMusicSeed(seed)
		}

	case event.EventMusicSwingRequest:
		if p, ok := ev.Payload.(*event.MusicSwingPayload); ok {
			s.player.SetMusicSwing(p.Amount) // sequencer clamps to [0, MaxSwing]
		}
	}
}

// applyMusicAudible gates the music bus. The sequencer is frozen, not stopped,
// so position and phrase survive the mute. Start only covers a session that
// began muted and never ran.
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
	// Skip the slew while muted — the sequencer is frozen, commands
	if !s.enabled || s.player == nil || s.player.IsMusicMuted() || !s.player.IsMusicPlaying() {
		return
	}
	s.syncToAPM()
}

// applyArrangement applies the tier's registered pattern set
// reveal requests the sequencer's per-bar track build-up
func (s *MusicSystem) applyArrangement(quantize bool, fade int, reveal bool) {
	s.player.SetIntensity(s.tier, fade, quantize, reveal)
}

// fadeSamples resolves a transition length: explicit request wins, else the
// rise/fall preset. Falling tiers swap without a build-up by design
func (s *MusicSystem) fadeSamples(t time.Duration, rising bool) int {
	if t == 0 {
		t = parameter.PatternTransitionDefault
		if rising {
			t = parameter.PatternTransitionRise
		}
	}
	return int(t.Seconds() * float64(audio.AudioSampleRate))
}

// startMusic seeds harmony and tempo, applies the current tier immediately,
// then runs the sequencer. The tier must not go through syncToAPM's
// bar-quantized path: the sequencer is stopped, so a pending transition would
// not resolve until bar 1 and the first bar would render silence
func (s *MusicSystem) startMusic() {
	s.player.SetHarmony(parameter.DefaultRootNote, audio.ScalePhrygian, nil)
	apm := s.world.Resources.Game.State.GetMusicAPM()
	s.syncTempo(apm)
	if !s.manualTier {
		s.tier = tierForAPM(apm)
	}
	s.arranged = true
	s.applyArrangement(false, 0, false) // silent source: immediate, no build-up
	s.player.StartMusic()
}

// syncToAPM slews tempo toward the APM target and applies auto-tier shifts
func (s *MusicSystem) syncToAPM() {
	apm := s.world.Resources.Game.State.GetMusicAPM()
	s.syncTempo(apm)
	if s.manualTier {
		return
	}
	tier := tierForAPM(apm)
	if tier == s.tier && s.arranged {
		return
	}
	rising := s.arranged && tier > s.tier // first arrangement is not a build-up
	s.tier = tier
	s.arranged = true
	s.applyArrangement(true, s.fadeSamples(0, rising), rising)
}

// syncTempo drifts the slewed tempo toward the APM target under hysteresis
func (s *MusicSystem) syncTempo(apm uint64) {
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
}
