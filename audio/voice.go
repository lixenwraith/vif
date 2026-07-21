package audio

import (
	"math"
)

// Voice is the common interface for sound generators
type Voice interface {
	Sample() float64
	Active() bool
	Trigger(params VoiceParams)
	Release()
	Reset()
}

// VoiceParams contains trigger parameters
type VoiceParams struct {
	Note       int     // MIDI note (ignored for drums)
	Velocity   float64 // 0.0-1.0
	Duration   int     // Samples, 0 = instrument default
	Instrument InstrumentType
}

// --- DrumVoice: One-shot percussion ---

// DrumVoice plays from a pre-rendered variant set; no allocation on trigger
type DrumVoice struct {
	variants []floatBuffer
	buffer   floatBuffer
	pos      int
	velocity float64
	nextVar  uint32 // rotation counter; retrigger picks a different variant
	active   bool
}

// NewDrumVoice creates a drum voice over a variant set
func NewDrumVoice(variants []floatBuffer) *DrumVoice {
	return &DrumVoice{variants: variants}
}

func (v *DrumVoice) Trigger(params VoiceParams) {
	if len(v.variants) == 0 {
		return
	}
	// rotate pre-rendered variants instead of synthesizing per hit
	// on the audio path; rotation avoids the machine-gun effect
	v.buffer = v.variants[v.nextVar%uint32(len(v.variants))]
	v.nextVar++
	v.velocity = params.Velocity
	v.pos = 0
	v.active = true
}

func (v *DrumVoice) Sample() float64 {
	if !v.active || v.pos >= len(v.buffer) {
		v.active = false
		return 0
	}
	s := v.buffer[v.pos] * v.velocity
	v.pos++
	return s
}

func (v *DrumVoice) Active() bool {
	return v.active
}

func (v *DrumVoice) Release() {
	// Drums don't respond to release, they complete naturally
}

func (v *DrumVoice) Reset() {
	v.active = false
	v.pos = 0
}

// --- TonalVoice: Pitched instrument with ADSR ---

// ADSRState tracks envelope phase
type ADSRState int

const (
	ADSRIdle ADSRState = iota
	ADSRAttack
	ADSRDecay
	ADSRSustain
	ADSRRelease
)

// TonalVoice generates pitched sounds with envelope
type TonalVoice struct {
	instrument InstrumentType
	note       int
	freq       float64
	velocity   float64
	phase      float64 // Oscillator phase 0-1
	serial     uint64  // Trigger order; StealOldest ranks on this

	// ADSR envelope
	envState ADSRState
	envLevel float64
	envPos   int     // Samples into current phase
	attack   int     // Samples
	decay    int     // Samples
	sustain  float64 // Level 0-1
	release  int     // Samples
	durLeft  int     // samples until auto-release; 0 = hold until Release()
	relFrom  float64 // envelope level at release start

	// Instrument-specific state
	filterState float64 // For filter sweep
	modPhase    float64 // For FM/chorus

	active    bool
	releasing bool
}

// NewTonalVoice creates a tonal voice
func NewTonalVoice() *TonalVoice {
	return &TonalVoice{}
}

func (v *TonalVoice) Sample() float64 {
	if !v.active {
		return 0
	}

	// Generate raw oscillator sample based on instrument
	var raw float64
	switch v.instrument {
	case InstrBass:
		raw = v.generateBass()
	case InstrPiano:
		raw = v.generatePiano()
	case InstrPad:
		raw = v.generatePad()
	default:
		raw = math.Sin(2 * math.Pi * v.phase)
	}

	// Advance phase
	v.phase += v.freq / float64(AudioSampleRate)
	if v.phase >= 1.0 {
		v.phase -= 1.0
	}

	// Apply envelope
	env := v.processEnvelope()
	if v.envState == ADSRIdle {
		v.active = false
		return 0
	}
	// Scheduled note-off
	if v.durLeft > 0 {
		v.durLeft--
		if v.durLeft == 0 {
			v.Release()
		}
	}

	return raw * env * v.velocity
}

func (v *TonalVoice) processEnvelope() float64 {
	switch v.envState {
	case ADSRAttack:
		if v.attack > 0 {
			v.envLevel = float64(v.envPos) / float64(v.attack)
		} else {
			v.envLevel = 1.0
		}
		v.envPos++
		if v.envPos >= v.attack {
			v.envState = ADSRDecay
			v.envPos = 0
		}

	case ADSRDecay:
		if v.decay > 0 {
			t := float64(v.envPos) / float64(v.decay)
			v.envLevel = 1.0 - t*(1.0-v.sustain)
		} else {
			v.envLevel = v.sustain
		}
		v.envPos++
		if v.envPos >= v.decay {
			if v.sustain > 0 {
				v.envState = ADSRSustain
			} else {
				v.relFrom = v.envLevel
				v.envState = ADSRRelease
				v.envPos = 0
			}
		}

	case ADSRSustain:
		v.envLevel = v.sustain
		// Stay here until Release() called

	case ADSRRelease:
		if v.release > 0 {
			t := float64(v.envPos) / float64(v.release)
			v.envLevel = v.relFrom * (1.0 - t)
		} else {
			v.envLevel = 0
		}
		v.envPos++
		if v.envPos >= v.release || v.envLevel <= 0.001 {
			v.envState = ADSRIdle
			v.envLevel = 0
		}
	}

	return v.envLevel
}

func (v *TonalVoice) generateBass() float64 {
	// Saw wave with low-pass filter
	saw := 2.0*v.phase - 1.0
	// Simple one-pole filter
	cutoff := BassFilterBase - BassFilterTrack*v.envLevel // Filter closes as note decays
	v.filterState += cutoff * (saw - v.filterState)
	return v.filterState
}

func (v *TonalVoice) generatePiano() float64 {
	// FM synthesis: carrier + modulator for bell-like tone
	modIndex := PianoModIndex * v.envLevel // Index decreases with envelope
	modFreq := v.freq * PianoModRatio      // Harmonic modulator

	v.modPhase += modFreq / float64(AudioSampleRate)
	if v.modPhase >= 1.0 {
		v.modPhase -= 1.0
	}

	mod := math.Sin(2 * math.Pi * v.modPhase)
	return math.Sin(2*math.Pi*v.phase + modIndex*mod)
}

func (v *TonalVoice) generatePad() float64 {
	// Detuned oscillators for thick sound
	phase2 := v.phase * (1.0 + PadDetune)
	phase3 := v.phase * (1.0 - PadDetune)

	osc1 := math.Sin(2 * math.Pi * v.phase)
	osc2 := math.Sin(2 * math.Pi * phase2)
	osc3 := math.Sin(2 * math.Pi * phase3)

	return (osc1 + osc2 + osc3) / 3.0
}

func (v *TonalVoice) Active() bool {
	return v.active
}

func (v *TonalVoice) Trigger(params VoiceParams) {
	v.instrument = params.Instrument
	v.note = params.Note
	v.freq = NoteFreq(params.Note)
	v.velocity = params.Velocity
	v.phase = 0
	v.modPhase = 0
	v.filterState = 0

	// Set ADSR based on instrument
	sr := float64(AudioSampleRate)
	switch params.Instrument {
	case InstrBass:
		v.attack = int(BassAttack * sr)
		v.decay = int(BassDecay * sr)
		v.sustain = BassSustain
		v.release = int(BassRelease * sr)
	case InstrPiano:
		v.attack = int(PianoAttack * sr)
		v.decay = int(PianoDecay * sr)
		v.sustain = PianoSustain
		v.release = int(PianoRelease * sr)
	case InstrPad:
		v.attack = int(PadAttack * sr)
		v.decay = int(PadDecay * sr)
		v.sustain = PadSustain
		v.release = int(PadRelease * sr)
	default:
		v.attack = int(VoiceDefaultAttack * sr)
		v.decay = int(VoiceDefaultDecay * sr)
		v.sustain = VoiceDefaultSustain
		v.release = int(VoiceDefaultRelease * sr)
	}

	v.envState = ADSRAttack
	v.envPos = 0
	v.envLevel = 0
	v.durLeft = params.Duration
	v.relFrom = 0
	v.active = true
	v.releasing = false
}

func (v *TonalVoice) Release() {
	if v.active && !v.releasing {
		v.releasing = true
		v.relFrom = v.envLevel
		v.envState = ADSRRelease
		v.envPos = 0
	}
}

func (v *TonalVoice) Reset() {
	v.active = false
	v.releasing = false
	v.envState = ADSRIdle
	v.envLevel = 0
	v.durLeft = 0
}

func (v *TonalVoice) Note() int {
	return v.note
}

func (v *TonalVoice) EnvLevel() float64 {
	return v.envLevel
}
