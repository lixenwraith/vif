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
	kit      *kit
	instr    InstrumentType
	buffer   floatBuffer
	pos      int
	velocity float64
	nextVar  uint32 // rotation counter; retrigger picks a different variant
	active   bool
}

// NewDrumVoice creates a drum voice over a variant set
func NewDrumVoice(k *kit, instr InstrumentType) *DrumVoice {
	return &DrumVoice{kit: k, instr: instr}
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

// RenderNote plays one note of a voice offline, midi at velocity vel, held for
// dur seconds and let ring for tail more or until it has died away, as a
// TonalVoice sounds it: what a candidate voice is compared with a recording's
// note by.
func RenderNote(v *VoiceDef, midi int, vel float64, dur, tail float64) []float64 {
	return RenderNoteAt(v, midi, 0, vel, dur, tail)
}

// RenderNoteAt is RenderNote with the note cents off A440, as a voice fitted to
// a recording at that tuning is compared with it.
func RenderNoteAt(v *VoiceDef, midi int, cents, vel float64, dur, tail float64) []float64 {
	k := &kit{voices: make([]*VoiceDef, InstrLead+1), tuning: math.Pow(2, min(max(cents, -MaxTuningCents), MaxTuningCents)/1200)}
	k.voices[InstrLead] = v
	tv := NewTonalVoice(k)
	tv.Trigger(VoiceParams{Note: midi, Velocity: vel, Duration: max(int(dur*AudioSampleRate), 1), Instrument: InstrLead})
	out := make([]float64, max(int((dur+tail)*AudioSampleRate), 1))
	for i := range out {
		if !tv.Active() {
			return out[:i]
		}
		out[i] = tv.Sample()
	}
	return out
}

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

// TonalVoice generates pitched sounds with envelope; the voice it plays is the
// kit's for the instrument at trigger time, so a redefined voice reaches the next
// note while a sounding one keeps its own
type TonalVoice struct {
	kit      *kit
	spec     *VoiceDef
	wave     int     // the spec's wave, resolved at trigger
	level    float64 // the spec's level, 1 when unset
	note     int
	freq     float64
	velocity float64
	phase    [maxUnison]float64 // oscillator phases 0-1
	modPhase [maxUnison]float64 // FM modulator phases
	ratio    [maxUnison]float64 // unison detune ratios
	subPhase float64
	serial   uint64 // Trigger order; StealOldest ranks on this

	// vibrato's phase and the note's age, and the vowel's formant bank
	vibPhase float64
	age      int
	formants [3]biquad

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

// NewTonalVoice creates a tonal voice playing k's voices
func NewTonalVoice(k *kit) *TonalVoice {
	return &TonalVoice{kit: k, spec: &defaultVoice, level: defaultVoice.Level}
}

func (v *TonalVoice) Sample() float64 {
	if !v.active {
		return 0
	}
	sp := v.spec
	dt := v.freq / float64(AudioSampleRate)
	if sp.VibratoHz > 0 && sp.VibratoCents > 0 {
		depth := 1.0
		if sp.VibratoDelay > 0 {
			depth = min(float64(v.age)/(sp.VibratoDelay*AudioSampleRate), 1)
		}
		dt *= 1 + sp.VibratoCents*depth*math.Ln2/1200*math.Sin(2*math.Pi*v.vibPhase)
		v.vibPhase = wrap(v.vibPhase + sp.VibratoHz/AudioSampleRate)
	}
	v.age++
	n := max(min(sp.Unison, maxUnison), 1)
	var raw float64
	switch {
	case v.wave == waveModal:
		raw = v.modal()
	case v.wave == waveString:
		raw = v.pluck()
	case sp.FMIndex > 0:
		idx := sp.FMIndex * v.envLevel
		if sp.FMDecay > 0 {
			v.ienv *= v.ienvCoef
			idx = sp.FMIndex * v.ienv
		}
		for i := range n {
			d := dt * v.ratio[i]
			mod := math.Sin(2 * math.Pi * v.modPhase[i])
			raw += math.Sin(2*math.Pi*v.phase[i] + idx*mod)
			v.phase[i] = wrap(v.phase[i] + d)
			v.modPhase[i] = wrap(v.modPhase[i] + d*sp.FMRatio)
		}
		raw /= float64(n)
	default:
		for i := range n {
			d := dt * v.ratio[i]
			raw += blepWave(v.wave, v.phase[i], d)
			v.phase[i] = wrap(v.phase[i] + d)
		}
		raw /= float64(n)
	}
	if sp.Vowel != "" && v.wave != waveModal && v.wave != waveString {
		raw = 1.2*v.formants[0].process(raw) + 0.7*v.formants[1].process(raw) + 0.4*v.formants[2].process(raw)
	}
	if sp.Q > 0 {
		raw = v.filter(raw)
	}
	if sp.Drive > 0 {
		raw = math.Tanh(raw * sp.Drive)
	}
	if sp.Sub > 0 {
		raw += sp.Sub * math.Sin(2*math.Pi*v.subPhase)
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
	return raw * env * v.velocity * v.level
}

// pluck reads the string and feeds back the average of its last two samples, which
// loses the highs a little each period as a string does; bright keeps some of them
func (v *TonalVoice) pluck() float64 {
	out := v.str[v.strPos]
	next := v.str[(v.strPos+1)%v.strLen]
	sp := v.spec
	v.str[v.strPos] = v.strGain * ((1-sp.Bright)*(out+next)*0.5 + sp.Bright*out)
	v.strPos = (v.strPos + 1) % v.strLen
	return out
}

// strike fills the string with a noise burst from a small generator seeded by the
// note and the voice's trigger order, so a replay plucks the same string
func (v *TonalVoice) strike(sp *VoiceDef) {
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
	v.strGain = math.Pow(0.001, 1/(max(sp.Ring, 0.05)*v.freq))
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
func (v *TonalVoice) excite(sp *VoiceDef) {
	n := max(min(sp.Partials, maxPartials, int(modalTopHz/v.freq)), 4)
	if cap(v.mre) < maxPartials+pairPartials {
		c := maxPartials + pairPartials
		v.mre, v.mim, v.mcr, v.mci = make([]float64, 0, c), make([]float64, 0, c), make([]float64, 0, c), make([]float64, 0, c)
	}
	v.mre, v.mim, v.mcr, v.mci = v.mre[:0], v.mim[:0], v.mcr[:0], v.mci[:0]
	h := sp.StrikePos
	if h <= 0 {
		h = 0.12
	}
	tilt := sp.Tilt
	if tilt <= 0 {
		tilt = 1
	}
	vel := max(v.velocity, 0.05)
	b := sp.Inharm * math.Pow(v.freq/modalRefHz, sp.InharmTrack)
	tau := max(sp.DecayTime, 0.05) * math.Pow(modalRefHz/v.freq, sp.DecayTrack)
	fc := sp.HammerHz * (0.3 + 0.7*vel)
	slow := sp.PairSlow
	if slow <= 0 {
		slow = 0.35
	}
	detune := math.Pow(2, sp.Pair/1200)
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
		if sp.DampHz > 0 {
			sigma *= 1 + (f/sp.DampHz)*(f/sp.DampHz)
		}
		if k <= pairPartials && sp.PairLevel > 0 {
			add(f, a/(1+sp.PairLevel), sigma)
			add(f*detune, a*sp.PairLevel/(1+sp.PairLevel), sigma*slow)
		} else {
			add(f, a, sigma)
		}
	}
	v.mscale = 1 / max(sum, 1e-9)
	v.mcheck = 0
	v.knockEnv, v.knockLP = sp.Knock*vel, 0
	v.knockSeed = uint32(v.note)*2654435761 + uint32(v.serial)*40503 + 1
}

// filter is a trapezoidal state-variable low-pass whose cutoff follows the key and
// the filter envelope; coefficients refresh every filterCoefHold samples
func (v *TonalVoice) filter(x float64) float64 {
	sp := v.spec
	if sp.EnvDecay > 0 {
		v.fenv *= v.fenvCoef
	} else {
		v.fenv = v.envLevel
	}
	if v.coefLeft--; v.coefLeft <= 0 {
		v.coefLeft = filterCoefHold
		fc := sp.Cutoff + sp.KeyTrack*v.freq + sp.EnvAmount*v.fenv
		fc = min(max(fc, 20), 0.45*AudioSampleRate)
		g := math.Tan(math.Pi * fc / AudioSampleRate)
		k := 1 / sp.Q
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
	sp := v.kit.voice(params.Instrument)
	v.spec = sp
	v.wave = waveOf(sp.Wave)
	v.level = sp.Level
	if v.level <= 0 {
		v.level = 1
	}
	v.note = params.Note
	v.freq = NoteFreq(params.Note) * v.kit.ratio()
	v.velocity = params.Velocity
	// Unison spreads symmetrically about the note; phases stagger so the stack
	// does not start as one in-phase saw
	n := max(min(sp.Unison, maxUnison), 1)
	detune := sp.Detune
	if detune <= 0 {
		detune = 1
	}
	for i := range n {
		v.ratio[i] = math.Pow(detune, float64(i)-float64(n-1)/2)
		v.phase[i] = float64(i) / float64(n)
		v.modPhase[i] = 0
	}
	v.subPhase = 0
	v.vibPhase, v.age = 0, 0
	if f, ok := vowels[sp.Vowel]; ok {
		for i := range v.formants {
			v.formants[i] = bandpass(f[i], f[i]/100)
		}
	}
	v.ic1, v.ic2, v.coefLeft = 0, 0, 0
	v.fenv, v.ienv = 1, 1
	if sp.EnvDecay > 0 {
		v.fenvCoef = math.Exp(-1 / (sp.EnvDecay * AudioSampleRate))
	}
	if sp.FMDecay > 0 {
		v.ienvCoef = math.Exp(-1 / (sp.FMDecay * AudioSampleRate))
	}
	switch v.wave {
	case waveString:
		v.strike(sp)
	case waveModal:
		v.excite(sp)
	}

	sr := float64(AudioSampleRate)
	v.attack = int(sp.Attack * sr)
	v.decay = int(sp.Decay * sr)
	v.sustain = sp.Sustain
	v.release = int(sp.Release * sr)

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

// biquad is one second-order section with its state
type biquad struct {
	b0, b1, b2, a1, a2 float64
	x1, x2, y1, y2     float64
}

// bandpass is a resonance at hz of width q, peaking at one (the cookbook's
// constant-peak band-pass)
func bandpass(hz, q float64) biquad {
	w := 2 * math.Pi * min(hz, 0.45*AudioSampleRate) / AudioSampleRate
	alpha := math.Sin(w) / (2 * q)
	a0 := 1 + alpha
	return biquad{b0: alpha / a0, b2: -alpha / a0, a1: -2 * math.Cos(w) / a0, a2: (1 - alpha) / a0}
}

func (b *biquad) process(x float64) float64 {
	y := b.b0*x + b.b1*b.x1 + b.b2*b.x2 - b.a1*b.y1 - b.a2*b.y2
	b.x2, b.x1, b.y2, b.y1 = b.x1, x, b.y1, y
	return y
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
