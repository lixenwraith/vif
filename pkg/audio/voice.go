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
	kit      *drumKit
	instr    InstrumentType
	buffer   floatBuffer
	pos      int
	velocity float64
	nextVar  uint32 // rotation counter; retrigger picks a different variant
	active   bool
}

// NewDrumVoice creates a drum voice over a variant set
func NewDrumVoice(kit *drumKit, instr InstrumentType) *DrumVoice {
	return &DrumVoice{kit: kit, instr: instr}
}

func (v *DrumVoice) Trigger(params VoiceParams) {
	// Read through the kit rather than caching the slice header: a cached
	// header would pin the superseded set across a hot-reload.
	vars := v.kit.variants[v.instr]
	if len(vars) == 0 {
		return
	}
	v.buffer = vars[v.nextVar%uint32(len(vars))]
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

// voiceSpec is one tonal instrument: an oscillator stack through a resonant
// low-pass under a filter envelope, into an amplitude ADSR. Times are seconds,
// frequencies Hz, sustain and levels linear.
type voiceSpec struct {
	wave    int     // waveSine, waveSaw or waveSquare
	unison  int     // oscillators, each detuned from the last by detune
	detune  float64 // frequency ratio between neighbouring oscillators
	sub     float64 // level of a sine an octave below
	fmRatio float64 // 2-operator FM when fmIndex > 0; the index falls with the envelope
	fmIndex float64

	cutoff   float64 // Hz at rest
	keyTrack float64 // Hz added per Hz of the note
	envAmt   float64 // Hz added at the filter envelope's peak
	envDecay float64 // filter envelope decay; 0 rides the amplitude envelope
	q        float64 // resonance; 0 bypasses the filter

	attack, decay, sustain, release float64
	drive                           float64 // tanh drive after the filter; 0 = clean
	level                           float64
}

// voiceSpecs is the instrument table; drums have none. The bass is the psytrance
// rolling bass: a dark saw whose filter snaps shut under a sub, so sixteenths read
// as weight rather than buzz.
var voiceSpecs = [InstrumentCount]voiceSpec{
	InstrBass: {wave: waveSaw, unison: 1, sub: 0.55, cutoff: 70, keyTrack: 1.2, envAmt: 700, envDecay: 0.08, q: 1.3,
		attack: 0.003, decay: 0.12, sustain: 0.55, release: 0.09, drive: 1.5, level: 0.95},
	InstrPiano: {wave: waveSine, unison: 1, fmRatio: 2, fmIndex: 2.0,
		attack: 0.002, decay: 0.35, sustain: 0, release: 0.3, level: 0.8},
	InstrPad: {wave: waveSaw, unison: 3, detune: 1.006, cutoff: 200, keyTrack: 2.2, q: 0.6,
		attack: 0.35, decay: 0.3, sustain: 0.8, release: 1.3, level: 1.0},
	InstrLead: {wave: waveSaw, unison: 2, detune: 1.004, cutoff: 300, keyTrack: 3.0, envAmt: 2200, envDecay: 0.18, q: 0.9,
		attack: 0.005, decay: 0.2, sustain: 0.55, release: 0.25, drive: 1.2, level: 1.1},
	InstrPluck: {wave: waveSaw, unison: 1, cutoff: 150, keyTrack: 2.0, envAmt: 3500, envDecay: 0.06, q: 1.1,
		attack: 0.001, decay: 0.3, sustain: 0, release: 0.15, level: 0.9},
}

var defaultVoiceSpec = voiceSpec{wave: waveSine, unison: 1, attack: 0.01, decay: 0.1, sustain: 0.5, release: 0.2, level: 0.6}

// ADSRState tracks envelope phase
type ADSRState int

const (
	ADSRIdle ADSRState = iota
	ADSRAttack
	ADSRDecay
	ADSRSustain
	ADSRRelease
)

const (
	maxUnison      = 3
	filterCoefHold = 16 // samples between filter coefficient updates
)

// TonalVoice generates pitched sounds with envelope
type TonalVoice struct {
	spec     *voiceSpec
	note     int
	freq     float64
	velocity float64
	phase    [maxUnison]float64 // oscillator phases 0-1
	ratio    [maxUnison]float64 // unison detune ratios
	subPhase float64
	modPhase float64
	serial   uint64 // Trigger order; StealOldest ranks on this

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

	// Filter envelope and the state-variable low-pass it drives
	fenv, fenvCoef float64
	ic1, ic2       float64
	fa1, fa2, fa3  float64
	coefLeft       int

	active    bool
	releasing bool
}

// NewTonalVoice creates a tonal voice
func NewTonalVoice() *TonalVoice {
	return &TonalVoice{spec: &defaultVoiceSpec}
}

func (v *TonalVoice) Sample() float64 {
	if !v.active {
		return 0
	}
	sp := v.spec
	dt := v.freq / float64(AudioSampleRate)
	var raw float64
	if sp.fmIndex > 0 {
		mod := math.Sin(2 * math.Pi * v.modPhase)
		raw = math.Sin(2*math.Pi*v.phase[0] + sp.fmIndex*v.envLevel*mod)
		v.phase[0] = wrap(v.phase[0] + dt)
		v.modPhase = wrap(v.modPhase + dt*sp.fmRatio)
	} else {
		for i := range sp.unison {
			d := dt * v.ratio[i]
			raw += blepWave(sp.wave, v.phase[i], d)
			v.phase[i] = wrap(v.phase[i] + d)
		}
		raw /= float64(sp.unison)
	}
	if sp.sub > 0 {
		raw += sp.sub * math.Sin(2*math.Pi*v.subPhase)
		v.subPhase = wrap(v.subPhase + dt/2)
	}
	if sp.q > 0 {
		raw = v.filter(raw)
	}
	if sp.drive > 0 {
		raw = math.Tanh(raw * sp.drive)
	}

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
	return raw * env * v.velocity * sp.level
}

// filter is a trapezoidal state-variable low-pass whose cutoff follows the key and
// the filter envelope; coefficients refresh every filterCoefHold samples
func (v *TonalVoice) filter(x float64) float64 {
	sp := v.spec
	if sp.envDecay > 0 {
		v.fenv *= v.fenvCoef
	} else {
		v.fenv = v.envLevel
	}
	if v.coefLeft--; v.coefLeft <= 0 {
		v.coefLeft = filterCoefHold
		fc := sp.cutoff + sp.keyTrack*v.freq + sp.envAmt*v.fenv
		fc = min(max(fc, 20), 0.45*AudioSampleRate)
		g := math.Tan(math.Pi * fc / AudioSampleRate)
		k := 1 / sp.q
		v.fa1 = 1 / (1 + g*(g+k))
		v.fa2 = g * v.fa1
		v.fa3 = g * v.fa2
	}
	v3 := x - v.ic2
	v1 := v.fa1*v.ic1 + v.fa2*v3
	v2 := v.ic2 + v.fa2*v.ic1 + v.fa3*v3
	v.ic1 = 2*v1 - v.ic1
	v.ic2 = 2*v2 - v.ic2
	return v2
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

func (v *TonalVoice) Active() bool {
	return v.active
}

func (v *TonalVoice) Trigger(params VoiceParams) {
	sp := &defaultVoiceSpec
	if i := params.Instrument; i >= 0 && i < InstrumentCount && voiceSpecs[i].level > 0 {
		sp = &voiceSpecs[i]
	}
	v.spec = sp
	v.note = params.Note
	v.freq = NoteFreq(params.Note)
	v.velocity = params.Velocity
	// Unison spreads symmetrically about the note; phases stagger so the stack
	// does not start as one in-phase saw
	n := max(min(sp.unison, maxUnison), 1)
	for i := range n {
		v.ratio[i] = math.Pow(sp.detune, float64(i)-float64(n-1)/2)
		v.phase[i] = float64(i) / float64(n)
	}
	v.subPhase, v.modPhase = 0, 0
	v.ic1, v.ic2, v.coefLeft = 0, 0, 0
	v.fenv = 1
	if sp.envDecay > 0 {
		v.fenvCoef = math.Exp(-1 / (sp.envDecay * AudioSampleRate))
	}

	sr := float64(AudioSampleRate)
	v.attack = int(sp.attack * sr)
	v.decay = int(sp.decay * sr)
	v.sustain = sp.sustain
	v.release = int(sp.release * sr)

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

func wrap(p float64) float64 {
	if p >= 1 {
		return p - 1
	}
	return p
}

// polyBLEP is the band-limiting correction at a waveform's step: subtracted at a
// falling edge, added at a rising one, within one sample of the discontinuity
func polyBLEP(t, dt float64) float64 {
	switch {
	case t < dt:
		t /= dt
		return t + t - t*t - 1
	case t > 1-dt:
		t = (t - 1) / dt
		return t*t + t + t + 1
	}
	return 0
}

// blepWave is one band-limited oscillator sample at phase p advancing dt a sample
func blepWave(wave int, p, dt float64) float64 {
	switch wave {
	case waveSaw:
		return 2*p - 1 - polyBLEP(p, dt)
	case waveSquare:
		s := 1.0
		if p >= 0.5 {
			s = -1
		}
		return s + polyBLEP(p, dt) - polyBLEP(wrap(p+0.5), dt)
	}
	return math.Sin(2 * math.Pi * p)
}
