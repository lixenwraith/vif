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
	wave    int     // waveSine, waveSaw, waveSquare or waveString
	unison  int     // oscillators, each detuned from the last by detune
	detune  float64 // frequency ratio between neighbouring oscillators
	sub     float64 // level of a sine an octave below, added after the drive
	fmRatio float64 // 2-operator FM when fmIndex > 0
	fmIndex float64
	fmDecay float64 // the index's own decay; 0 rides the amplitude envelope
	bright  float64 // waveString: 0 a dull string, 1 a wiry one
	ring    float64 // waveString: seconds a plucked string takes to die away

	// waveModal, a struck or plucked string as its partials: partials caps the
	// bank, strikePos is where along the string the hammer or finger lands and
	// tilt the spectral slope (1 struck, 2 plucked); inharm is the stretch B of a
	// middle-C string, growing with pitch by inharmTrack
	partials    int
	strikePos   float64
	tilt        float64
	inharm      float64
	inharmTrack float64
	// decayT is the prompt decay at middle C, growing toward the bass by
	// decayTrack; a partial at dampHz decays twice as fast. The coupled pair is a
	// second string pair cents away whose aftersound sits pairLevel under the
	// prompt sound and decays pairSlow times as fast. hammerHz low-passes the
	// excitation at full velocity; knock is the level of the contact noise.
	decayT     float64
	decayTrack float64
	dampHz     float64
	pair       float64
	pairLevel  float64
	pairSlow   float64
	hammerHz   float64
	knock      float64

	cutoff   float64 // Hz at rest
	keyTrack float64 // Hz added per Hz of the note
	envAmt   float64 // Hz added at the filter envelope's peak
	envDecay float64 // filter envelope decay; 0 rides the amplitude envelope
	q        float64 // resonance; 0 bypasses the filter

	attack, decay, sustain, release float64
	drive                           float64 // tanh drive after the filter; 0 = clean
	level                           float64
}

// voiceSpecs is the instrument table; drums have none. The bass and the piano are
// strings as their partials: a finger-plucked bass through a pickup resonance and
// an amp, a hammered piano with a grand's stretch and double decay. The pluck is
// a string a noise burst sets ringing.
var voiceSpecs = [InstrumentCount]voiceSpec{
	InstrBass: {wave: waveModal, partials: 40, strikePos: 0.2, tilt: 1.7, inharm: 0.00008, inharmTrack: 1,
		decayT: 0.7, decayTrack: 0.5, dampHz: 900, pair: 1.2, pairLevel: 0.3, pairSlow: 0.4, hammerHz: 3000, knock: 0.08,
		cutoff: 2200, q: 1.3, attack: 0.003, sustain: 1, release: 0.045, drive: 1.4, level: 1.0},
	InstrPiano: {wave: waveModal, partials: 40, strikePos: 0.12, tilt: 1.0, inharm: 0.0004, inharmTrack: 1.3,
		decayT: 0.9, decayTrack: 0.6, dampHz: 2600, pair: 1.6, pairLevel: 0.28, pairSlow: 0.3, hammerHz: 2600, knock: 0.12,
		cutoff: 1200, keyTrack: 6, q: 0.5, attack: 0.002, sustain: 1, release: 0.09, level: 0.8},
	InstrPad: {wave: waveSaw, unison: 3, detune: 1.006, cutoff: 200, keyTrack: 2.2, q: 0.6,
		attack: 0.35, decay: 0.3, sustain: 0.8, release: 1.3, level: 1.0},
	InstrLead: {wave: waveSaw, unison: 2, detune: 1.004, cutoff: 300, keyTrack: 3.0, envAmt: 1600, envDecay: 0.18, q: 0.7,
		attack: 0.005, decay: 0.2, sustain: 0.55, release: 0.25, drive: 1.0, level: 1.1},
	InstrPluck: {wave: waveString, unison: 1, bright: 0.3, ring: 0.7, cutoff: 900, keyTrack: 2.5, q: 0.5,
		attack: 0.001, decay: 0.1, sustain: 1, release: 0.12, level: 1.0},
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
	// waveString is the plucked string, waveModal the partial bank, after
	// generator.go's waveforms
	waveString = waveNoise + 1
	waveModal  = waveNoise + 2
	// stringMaxLen is the delay line for the lowest note a string plays (E1)
	stringMaxLen = AudioSampleRate / 40
	// maxPartials caps a modal bank, pairPartials of which carry the coupled
	// pair's second string; the series stops at modalTopHz
	maxPartials  = 48
	pairPartials = 8
	modalTopHz   = 9000.0
	modalRefHz   = 261.63
	modalCheck   = 1024 // samples between checks that a bank has rung out
)

// TonalVoice generates pitched sounds with envelope
type TonalVoice struct {
	spec     *voiceSpec
	note     int
	freq     float64
	velocity float64
	phase    [maxUnison]float64 // oscillator phases 0-1
	modPhase [maxUnison]float64 // FM modulator phases
	ratio    [maxUnison]float64 // unison detune ratios
	subPhase float64
	serial   uint64 // Trigger order; StealOldest ranks on this

	// The plucked string: a delay line one period long, its loss per period
	str     []float64
	strLen  int
	strPos  int
	strGain float64

	// The modal bank: a decaying phasor per partial (and per pair string), the
	// sum's scale, and the contact noise through a one-pole
	mre, mim, mcr, mci []float64
	mscale             float64
	mcheck             int
	knockEnv, knockLP  float64
	knockSeed          uint32

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

	// Filter and FM index envelopes and the state-variable low-pass the first drives
	fenv, fenvCoef float64
	ienv, ienvCoef float64
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
	switch {
	case sp.wave == waveModal:
		raw = v.modal()
	case sp.wave == waveString:
		raw = v.pluck()
	case sp.fmIndex > 0:
		idx := sp.fmIndex * v.envLevel
		if sp.fmDecay > 0 {
			v.ienv *= v.ienvCoef
			idx = sp.fmIndex * v.ienv
		}
		for i := range sp.unison {
			d := dt * v.ratio[i]
			mod := math.Sin(2 * math.Pi * v.modPhase[i])
			raw += math.Sin(2*math.Pi*v.phase[i] + idx*mod)
			v.phase[i] = wrap(v.phase[i] + d)
			v.modPhase[i] = wrap(v.modPhase[i] + d*sp.fmRatio)
		}
		raw /= float64(sp.unison)
	default:
		for i := range sp.unison {
			d := dt * v.ratio[i]
			raw += blepWave(sp.wave, v.phase[i], d)
			v.phase[i] = wrap(v.phase[i] + d)
		}
		raw /= float64(sp.unison)
	}
	if sp.q > 0 {
		raw = v.filter(raw)
	}
	if sp.drive > 0 {
		raw = math.Tanh(raw * sp.drive)
	}
	if sp.sub > 0 {
		raw += sp.sub * math.Sin(2*math.Pi*v.subPhase)
		v.subPhase = wrap(v.subPhase + dt/2)
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

// pluck reads the string and feeds back the average of its last two samples, which
// loses the highs a little each period as a string does; bright keeps some of them
func (v *TonalVoice) pluck() float64 {
	out := v.str[v.strPos]
	next := v.str[(v.strPos+1)%v.strLen]
	sp := v.spec
	v.str[v.strPos] = v.strGain * ((1-sp.bright)*(out+next)*0.5 + sp.bright*out)
	v.strPos = (v.strPos + 1) % v.strLen
	return out
}

// strike fills the string with a noise burst from a small generator seeded by the
// note and the voice's trigger order, so a replay plucks the same string
func (v *TonalVoice) strike(sp *voiceSpec) {
	v.strLen = min(max(int(float64(AudioSampleRate)/v.freq+0.5), 2), stringMaxLen)
	if cap(v.str) < stringMaxLen {
		v.str = make([]float64, stringMaxLen)
	}
	v.str = v.str[:v.strLen]
	seed := uint32(v.note)*2654435761 + uint32(v.serial)*40503 + 1
	for i := range v.str {
		seed ^= seed << 13
		seed ^= seed >> 17
		seed ^= seed << 5
		v.str[i] = float64(seed)/float64(1<<32)*2 - 1
	}
	v.strPos = 0
	// the loss per period that takes the string to a thousandth over ring seconds
	v.strGain = math.Pow(0.001, 1/(max(sp.ring, 0.05)*v.freq))
}

// modal advances every partial's phasor and sums them, adds the contact noise
// while it lasts, and ends the note once the bank has rung out
func (v *TonalVoice) modal() float64 {
	var s float64
	for i := range v.mre {
		re, im := v.mre[i], v.mim[i]
		v.mre[i] = re*v.mcr[i] - im*v.mci[i]
		v.mim[i] = re*v.mci[i] + im*v.mcr[i]
		s += v.mim[i]
	}
	s *= v.mscale
	if v.knockEnv > 1e-4 {
		v.knockSeed ^= v.knockSeed << 13
		v.knockSeed ^= v.knockSeed >> 17
		v.knockSeed ^= v.knockSeed << 5
		n := float64(v.knockSeed)/float64(1<<32)*2 - 1
		v.knockLP += 0.25 * (n - v.knockLP) // a 2 kHz one-pole
		s += v.knockLP * v.knockEnv
		v.knockEnv *= 0.9943 // 4 ms
	}
	if v.mcheck++; v.mcheck >= modalCheck {
		v.mcheck = 0
		var e float64
		for i := range v.mre {
			e += v.mre[i]*v.mre[i] + v.mim[i]*v.mim[i]
		}
		if e*v.mscale*v.mscale < 1e-10 {
			v.Release()
		}
	}
	return s
}

// excite builds the modal bank for the note: partial k sits at k·f0 stretched by
// the inharmonicity, at the amplitude the strike position's comb, the spectral
// tilt and the hammer's low-pass leave it, decaying faster the higher it is; the
// lowest partials get the coupled pair's slower, slightly detuned second string
func (v *TonalVoice) excite(sp *voiceSpec) {
	n := max(min(sp.partials, maxPartials, int(modalTopHz/v.freq)), 4)
	if cap(v.mre) < maxPartials+pairPartials {
		c := maxPartials + pairPartials
		v.mre, v.mim, v.mcr, v.mci = make([]float64, 0, c), make([]float64, 0, c), make([]float64, 0, c), make([]float64, 0, c)
	}
	v.mre, v.mim, v.mcr, v.mci = v.mre[:0], v.mim[:0], v.mcr[:0], v.mci[:0]
	h := sp.strikePos
	if h <= 0 {
		h = 0.12
	}
	tilt := sp.tilt
	if tilt <= 0 {
		tilt = 1
	}
	vel := max(v.velocity, 0.05)
	b := sp.inharm * math.Pow(v.freq/modalRefHz, sp.inharmTrack)
	tau := max(sp.decayT, 0.05) * math.Pow(modalRefHz/v.freq, sp.decayTrack)
	fc := sp.hammerHz * (0.3 + 0.7*vel)
	slow := sp.pairSlow
	if slow <= 0 {
		slow = 0.35
	}
	detune := math.Pow(2, sp.pair/1200)
	comb0 := math.Abs(math.Sin(math.Pi * h))
	var sum float64
	add := func(f, a, sigma float64) {
		if f >= 0.45*AudioSampleRate {
			return
		}
		d := math.Exp(-sigma / AudioSampleRate)
		w := 2 * math.Pi * f / AudioSampleRate
		v.mre, v.mim = append(v.mre, a), append(v.mim, 0)
		v.mcr, v.mci = append(v.mcr, d*math.Cos(w)), append(v.mci, d*math.Sin(w))
		sum += a
	}
	for k := 1; k <= n; k++ {
		fk := float64(k)
		f := fk * v.freq * math.Sqrt(1+b*fk*fk)
		a := math.Abs(math.Sin(math.Pi*fk*h)) / comb0 / math.Pow(fk, tilt)
		if fc > 0 {
			a /= 1 + (f/fc)*(f/fc)
		}
		sigma := 1 / tau
		if sp.dampHz > 0 {
			sigma *= 1 + (f/sp.dampHz)*(f/sp.dampHz)
		}
		if k <= pairPartials && sp.pairLevel > 0 {
			add(f, a/(1+sp.pairLevel), sigma)
			add(f*detune, a*sp.pairLevel/(1+sp.pairLevel), sigma*slow)
		} else {
			add(f, a, sigma)
		}
	}
	v.mscale = 1 / max(sum, 1e-9)
	v.mcheck = 0
	v.knockEnv, v.knockLP = sp.knock*vel, 0
	v.knockSeed = uint32(v.note)*2654435761 + uint32(v.serial)*40503 + 1
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
		v.modPhase[i] = 0
	}
	v.subPhase = 0
	v.ic1, v.ic2, v.coefLeft = 0, 0, 0
	v.fenv, v.ienv = 1, 1
	if sp.envDecay > 0 {
		v.fenvCoef = math.Exp(-1 / (sp.envDecay * AudioSampleRate))
	}
	if sp.fmDecay > 0 {
		v.ienvCoef = math.Exp(-1 / (sp.fmDecay * AudioSampleRate))
	}
	switch sp.wave {
	case waveString:
		v.strike(sp)
	case waveModal:
		v.excite(sp)
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
