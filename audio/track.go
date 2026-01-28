package audio

import (
	"sync"

	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
)

// BeatTrack manages drum pattern playback
type BeatTrack struct {
	mu sync.RWMutex

	pattern     core.PatternID
	patternData *BeatPattern

	// Crossfade state
	oldPattern   *BeatPattern
	crossfadePos int
	crossfadeLen int

	// Voices
	kick  *DrumVoice
	hihat *DrumVoice
	snare *DrumVoice
	clap  *DrumVoice
}

// BeatPattern defines drum triggers per step
type BeatPattern struct {
	ID     core.PatternID
	Length int           // Steps in pattern (default 16)
	Kick   []StepTrigger // Step indices where kick plays
	Hihat  []StepTrigger
	Snare  []StepTrigger
	Clap   []StepTrigger
}

// StepTrigger defines a trigger at a step with velocity
type StepTrigger struct {
	Step     int
	Velocity float64
}

// NewBeatTrack creates beat track with voices
func NewBeatTrack() *BeatTrack {
	return &BeatTrack{
		kick:  NewDrumVoice(core.InstrKick),
		hihat: NewDrumVoice(core.InstrHihat),
		snare: NewDrumVoice(core.InstrSnare),
		clap:  NewDrumVoice(core.InstrClap),
	}
}

// TriggerStep triggers instruments based on pattern
func (t *BeatTrack) TriggerStep(step int) {
	t.mu.RLock()
	pattern := t.patternData
	t.mu.RUnlock()

	if pattern == nil {
		return
	}

	localStep := step % pattern.Length

	for _, trig := range pattern.Kick {
		if trig.Step == localStep {
			t.kick.Trigger(VoiceParams{Velocity: trig.Velocity, Instrument: core.InstrKick})
		}
	}
	for _, trig := range pattern.Hihat {
		if trig.Step == localStep {
			t.hihat.Trigger(VoiceParams{Velocity: trig.Velocity, Instrument: core.InstrHihat})
		}
	}
	for _, trig := range pattern.Snare {
		if trig.Step == localStep {
			t.snare.Trigger(VoiceParams{Velocity: trig.Velocity, Instrument: core.InstrSnare})
		}
	}
	for _, trig := range pattern.Clap {
		if trig.Step == localStep {
			t.clap.Trigger(VoiceParams{Velocity: trig.Velocity, Instrument: core.InstrClap})
		}
	}
}

// Sample returns mixed drum output
func (t *BeatTrack) Sample() float64 {
	sample := t.kick.Sample() + t.hihat.Sample() + t.snare.Sample() + t.clap.Sample()

	// Apply crossfade if active
	t.mu.RLock()
	if t.crossfadeLen > 0 && t.crossfadePos < t.crossfadeLen {
		fade := float64(t.crossfadePos) / float64(t.crossfadeLen)
		sample *= fade
		t.crossfadePos++
	}
	t.mu.RUnlock()

	return sample * 0.7 // Drum mix level
}

// SetPattern initiates pattern change
func (t *BeatTrack) SetPattern(p core.PatternID, crossfadeSamples int) {
	pattern := GetBeatPattern(p)

	t.mu.Lock()
	defer t.mu.Unlock()

	if crossfadeSamples > 0 {
		t.oldPattern = t.patternData
		t.crossfadePos = 0
		t.crossfadeLen = crossfadeSamples
	}

	t.pattern = p
	t.patternData = pattern
}

// Reset clears track state
func (t *BeatTrack) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.pattern = core.PatternSilence
	t.patternData = nil
	t.oldPattern = nil
	t.crossfadePos = 0
	t.crossfadeLen = 0

	t.kick.Reset()
	t.hihat.Reset()
	t.snare.Reset()
	t.clap.Reset()
}

// --- MelodyTrack ---

// MelodyTrack manages pitched instrument playback
type MelodyTrack struct {
	mu sync.RWMutex

	pattern     core.PatternID
	patternData *MelodyPattern
	rootNote    int

	// Voice pool
	voices        [constant.MaxPolyphony]*TonalVoice
	stealStrategy core.VoiceStealStrategy

	// Crossfade
	crossfadePos int
	crossfadeLen int
}

// MelodyPattern defines note triggers per step
type MelodyPattern struct {
	ID         core.PatternID
	Length     int // Steps
	Instrument core.InstrumentType
	Notes      []NoteTrigger
}

// NoteTrigger defines a note event
type NoteTrigger struct {
	Step       int
	NoteOffset int // Relative to root note
	Velocity   float64
	Duration   int // Steps, 0 = tie to next
}

// NewMelodyTrack creates melody track with voice pool
func NewMelodyTrack() *MelodyTrack {
	t := &MelodyTrack{
		stealStrategy: core.StealOldest,
	}
	for i := range t.voices {
		t.voices[i] = NewTonalVoice()
	}
	return t
}

// TriggerStep triggers notes based on pattern
func (t *MelodyTrack) TriggerStep(step int) {
	t.mu.RLock()
	pattern := t.patternData
	root := t.rootNote
	t.mu.RUnlock()

	if pattern == nil {
		return
	}

	localStep := step % pattern.Length

	for _, trig := range pattern.Notes {
		if trig.Step == localStep {
			note := root + trig.NoteOffset
			duration := trig.Duration * constant.SamplesPerStep(constant.DefaultBPM)
			t.TriggerNote(note, trig.Velocity, duration, pattern.Instrument)
		}
	}
}

// TriggerNote triggers immediate note with voice allocation
func (t *MelodyTrack) TriggerNote(note int, velocity float64, durationSamples int, instr core.InstrumentType) {
	t.mu.Lock()
	defer t.mu.Unlock()

	voice := t.allocateVoice(note)
	if voice == nil {
		return
	}

	voice.Trigger(VoiceParams{
		Note:       note,
		Velocity:   velocity,
		Duration:   durationSamples,
		Instrument: instr,
	})
}

func (t *MelodyTrack) allocateVoice(note int) *TonalVoice {
	// First pass: find free voice
	for _, v := range t.voices {
		if !v.Active() {
			return v
		}
	}

	// Voice stealing
	switch t.stealStrategy {
	case core.StealOldest:
		// Find voice with lowest envelope level (furthest in decay)
		var oldest *TonalVoice
		lowestEnv := 2.0
		for _, v := range t.voices {
			if v.EnvLevel() < lowestEnv {
				lowestEnv = v.EnvLevel()
				oldest = v
			}
		}
		return oldest

	case core.StealQuietest:
		var quietest *TonalVoice
		lowestVol := 2.0
		for _, v := range t.voices {
			vol := v.EnvLevel() * v.velocity
			if vol < lowestVol {
				lowestVol = vol
				quietest = v
			}
		}
		return quietest

	case core.StealSameNote:
		for _, v := range t.voices {
			if v.Note() == note {
				return v
			}
		}
		return nil

	default:
		return nil
	}
}

// Sample returns mixed voice output
func (t *MelodyTrack) Sample() float64 {
	var sum float64
	for _, v := range t.voices {
		if v.Active() {
			sum += v.Sample()
		}
	}

	// Apply crossfade
	t.mu.RLock()
	if t.crossfadeLen > 0 && t.crossfadePos < t.crossfadeLen {
		fade := float64(t.crossfadePos) / float64(t.crossfadeLen)
		sum *= fade
		t.crossfadePos++
	}
	t.mu.RUnlock()

	return sum * 0.5 // Melody mix level
}

// SetPattern changes melody pattern
func (t *MelodyTrack) SetPattern(p core.PatternID, root int, crossfadeSamples int) {
	pattern := GetMelodyPattern(p)

	t.mu.Lock()
	defer t.mu.Unlock()

	if crossfadeSamples > 0 {
		t.crossfadePos = 0
		t.crossfadeLen = crossfadeSamples
	}

	t.pattern = p
	t.patternData = pattern
	t.rootNote = root
}

// SetStealStrategy changes voice allocation strategy
func (t *MelodyTrack) SetStealStrategy(s core.VoiceStealStrategy) {
	t.mu.Lock()
	t.stealStrategy = s
	t.mu.Unlock()
}

// Reset clears track state
func (t *MelodyTrack) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.pattern = core.PatternSilence
	t.patternData = nil
	t.crossfadePos = 0
	t.crossfadeLen = 0

	for _, v := range t.voices {
		v.Reset()
	}
}