package audio

import (
	"math"
	"math/rand"

	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
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
	Instrument core.InstrumentType
}

// --- DrumVoice: One-shot percussion ---

// DrumVoice generates single percussion hits
type DrumVoice struct {
	instrument core.InstrumentType
	buffer     floatBuffer // Pre-generated or generated on trigger
	pos        int
	velocity   float64
	active     bool
}

// NewDrumVoice creates a drum voice for the given instrument
func NewDrumVoice(instr core.InstrumentType) *DrumVoice {
	return &DrumVoice{
		instrument: instr,
	}
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

func (v *DrumVoice) Trigger(params VoiceParams) {
	v.velocity = params.Velocity
	v.buffer = generateDrumSound(v.instrument)
	v.pos = 0
	v.active = true
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
	instrument core.InstrumentType
	note       int
	freq       float64
	velocity   float64
	phase      float64 // Oscillator phase 0-1

	// ADSR envelope
	envState ADSRState
	envLevel float64
	envPos   int     // Samples into current phase
	attack   int     // Samples
	decay    int     // Samples
	sustain  float64 // Level 0-1
	release  int     // Samples

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
	case core.InstrBass:
		raw = v.generateBass()
	case core.InstrPiano:
		raw = v.generatePiano()
	case core.InstrPad:
		raw = v.generatePad()
	default:
		raw = math.Sin(2 * math.Pi * v.phase)
	}

	// Advance phase
	v.phase += v.freq / float64(constant.AudioSampleRate)
	if v.phase >= 1.0 {
		v.phase -= 1.0
	}

	// Apply envelope
	env := v.processEnvelope()
	if v.envState == ADSRIdle {
		v.active = false
		return 0
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
			v.envLevel = v.sustain * (1.0 - t)
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
	cutoff := 0.3 - 0.2*v.envLevel // Filter closes as note decays
	v.filterState += cutoff * (saw - v.filterState)
	return v.filterState
}

func (v *TonalVoice) generatePiano() float64 {
	// FM synthesis: carrier + modulator for bell-like tone
	modRatio := 2.0              // Harmonic modulator
	modIndex := 3.0 * v.envLevel // Index decreases with envelope

	modFreq := v.freq * modRatio
	v.modPhase += modFreq / float64(constant.AudioSampleRate)
	if v.modPhase >= 1.0 {
		v.modPhase -= 1.0
	}

	mod := math.Sin(2 * math.Pi * v.modPhase)
	return math.Sin(2*math.Pi*v.phase + modIndex*mod)
}

func (v *TonalVoice) generatePad() float64 {
	// Detuned oscillators for thick sound
	detune := 0.003 // 3 cents
	phase2 := v.phase * (1.0 + detune)
	phase3 := v.phase * (1.0 - detune)

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
	sr := float64(constant.AudioSampleRate)
	switch params.Instrument {
	case core.InstrBass:
		v.attack = int(constant.BassAttack * sr)
		v.decay = int(constant.BassDecay * sr)
		v.sustain = constant.BassSustain
		v.release = int(constant.BassRelease * sr)
	case core.InstrPiano:
		v.attack = int(constant.PianoAttack * sr)
		v.decay = int(constant.PianoDecay * sr)
		v.sustain = constant.PianoSustain
		v.release = int(constant.PianoRelease * sr)
	case core.InstrPad:
		v.attack = int(constant.PadAttack * sr)
		v.decay = int(constant.PadDecay * sr)
		v.sustain = constant.PadSustain
		v.release = int(constant.PadRelease * sr)
	default:
		v.attack = int(0.01 * sr)
		v.decay = int(0.1 * sr)
		v.sustain = 0.5
		v.release = int(0.2 * sr)
	}

	v.envState = ADSRAttack
	v.envPos = 0
	v.envLevel = 0
	v.active = true
	v.releasing = false
}

func (v *TonalVoice) Release() {
	if v.active && !v.releasing {
		v.releasing = true
		v.envState = ADSRRelease
		v.envPos = 0
	}
}

func (v *TonalVoice) Reset() {
	v.active = false
	v.releasing = false
	v.envState = ADSRIdle
	v.envLevel = 0
}

func (v *TonalVoice) Note() int {
	return v.note
}

func (v *TonalVoice) EnvLevel() float64 {
	return v.envLevel
}

// --- Drum Sound Generation ---

func generateDrumSound(instr core.InstrumentType) floatBuffer {
	switch instr {
	case core.InstrKick:
		return generateKick()
	case core.InstrHihat:
		return generateHihat()
	case core.InstrSnare:
		return generateSnare()
	case core.InstrClap:
		return generateClap()
	default:
		return nil
	}
}

func generateKick() floatBuffer {
	sr := constant.AudioSampleRate
	duration := int(float64(sr) * constant.KickDecay)
	buf := make(floatBuffer, duration)

	startFreq := 150.0
	endFreq := 40.0

	phase := 0.0
	for i := 0; i < duration; i++ {
		t := float64(i) / float64(duration)
		// Exponential pitch drop
		freq := endFreq + (startFreq-endFreq)*math.Exp(-8*t)
		// Exponential amplitude decay
		amp := math.Exp(-5 * t)

		buf[i] = math.Sin(2*math.Pi*phase) * amp
		phase += freq / float64(sr)
	}

	// Soft saturation for punch
	for i := range buf {
		buf[i] = math.Tanh(buf[i] * 2.0)
	}

	return buf
}

func generateHihat() floatBuffer {
	sr := constant.AudioSampleRate
	duration := int(float64(sr) * constant.HihatDecay)
	buf := make(floatBuffer, duration)

	for i := 0; i < duration; i++ {
		t := float64(i) / float64(duration)
		// Filtered noise with sharp decay
		noise := rand.Float64()*2 - 1
		amp := math.Exp(-15 * t)
		buf[i] = noise * amp
	}

	// High-pass filter
	filterBiquadHP(buf, 7000, 0.707)
	normalizePeak(buf, 0.9)

	return buf
}

func generateSnare() floatBuffer {
	sr := constant.AudioSampleRate
	duration := int(float64(sr) * constant.SnareDecay)
	buf := make(floatBuffer, duration)

	// Tone component (200Hz body)
	tonePhase := 0.0
	for i := 0; i < duration; i++ {
		t := float64(i) / float64(duration)
		toneAmp := math.Exp(-10 * t)
		buf[i] = math.Sin(2*math.Pi*tonePhase) * toneAmp * 0.5
		tonePhase += 200.0 / float64(sr)
	}

	// Noise component (snare wires)
	for i := 0; i < duration; i++ {
		t := float64(i) / float64(duration)
		noise := rand.Float64()*2 - 1
		noiseAmp := math.Exp(-8 * t)
		buf[i] += noise * noiseAmp * 0.5
	}

	// Band-pass the noise
	filterBiquadBP(buf, 2000, 1.5)
	normalizePeak(buf, 0.9)

	return buf
}

func generateClap() floatBuffer {
	sr := constant.AudioSampleRate
	duration := int(float64(sr) * constant.ClapDecay)
	buf := make(floatBuffer, duration)

	// Multiple short noise bursts
	burstLen := sr / 100 // 10ms bursts
	burstGap := sr / 200 // 5ms gaps
	numBursts := 4

	pos := 0
	for b := 0; b < numBursts && pos < duration; b++ {
		burstAmp := 1.0 - float64(b)*0.15 // Decreasing amplitude
		for i := 0; i < burstLen && pos < duration; i++ {
			t := float64(i) / float64(burstLen)
			noise := rand.Float64()*2 - 1
			env := math.Exp(-5 * t)
			buf[pos] = noise * env * burstAmp
			pos++
		}
		pos += burstGap // Gap between bursts
	}

	// Tail
	tailStart := pos
	for i := tailStart; i < duration; i++ {
		t := float64(i-tailStart) / float64(duration-tailStart)
		noise := rand.Float64()*2 - 1
		buf[i] = noise * math.Exp(-8*t) * 0.3
	}

	filterBiquadBP(buf, 1500, 2.0)
	normalizePeak(buf, 0.9)

	return buf
}