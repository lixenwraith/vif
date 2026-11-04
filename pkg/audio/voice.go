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
	v.buffer = v.kit.take(v.instr, params.Velocity, v.nextVar)
	if v.buffer == nil {
		return
	}
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
	// keyRefHz is middle C, where a voice's ring, decay and inharmonicity are
	// given and from which they track the key
	keyRefHz  = 261.63
	ringCheck = 1024 // samples between checks that a string or a bank has rung out
	ringFloor = 1e-10
	// strikeAt is where along the string a pluck or hammer lands when the voice
	// leaves strike_pos unset
	strikeAt = 0.12
	// bodyHz is a modal voice's bright 1: its soundboard or body radiates the
	// partials under bright·bodyHz weakly, as a piano's does its bass fundamentals
	bodyHz = 1000.0
	// driftShare is how far a unison oscillator wanders in pitch, as a share of
	// the detune between neighbours, over a period of driftMinHz to driftMaxHz
	driftShare = 0.3
	driftMinHz = 0.07
	driftMaxHz = 0.3
	// hammerVelExp is how a bank's strike low-pass follows velocity, hammer_hz
	// times velocity to it: a soft hammer stays on the string longer, so a
	// recorded piano's pp is a third of an octave darker than its mf
	hammerVelExp = 1.5
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
	// each unison oscillator's slow pitch wander: its phase 0-1, rate in Hz and
	// the ratio it plays at now; depth is the wander's share of the note
	drift, driftHz, wander [maxUnison]float64
	depth                  float64
	serial                 uint64 // Trigger order; StealOldest ranks on this

	// vibrato's phase and the note's age, and the vowel's formant bank
	vibPhase float64
	age      int
	formants [3]biquad

	// The plucked string: a delay line of the period's whole samples, the loop's
	// two-point low-pass (its stretch and last input), the all-pass that tunes
	// the period's fraction (its coefficient and state) and the loss per period
	str              []float64
	strLen, strPos   int
	strS, strLast    float64
	apC, apIn, apOut float64
	strLoss          float64

	// The modal bank: a decaying phasor per partial (and per pair string), the
	// sum's scale, and the contact noise through a one-pole
	mre, mim, mcr, mci []float64
	mscale             float64
	knockEnv, knockLP  float64
	knockSeed          uint32
	check              int // samples since a string or a bank was checked for ringing out

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
	n := max(min(sp.Unison, maxUnison), 1)
	if v.depth > 0 && v.age%filterCoefHold == 0 {
		for i := range n {
			v.wander[i] = v.ratio[i] * (1 + v.depth*math.Sin(2*math.Pi*v.drift[i]))
			v.drift[i] = wrap(v.drift[i] + v.driftHz[i]*filterCoefHold/AudioSampleRate)
		}
	}
	v.age++
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
			d := dt * v.wander[i]
			mod := math.Sin(2 * math.Pi * v.modPhase[i])
			raw += math.Sin(2*math.Pi*v.phase[i] + idx*mod)
			v.phase[i] = wrap(v.phase[i] + d)
			v.modPhase[i] = wrap(v.modPhase[i] + d*sp.FMRatio)
		}
		raw /= float64(n)
	default:
		for i := range n {
			d := dt * v.wander[i]
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

// pluck is one sample of the extended Karplus-Strong loop (Jaffe and Smith
// 1983): the delay line's sample through the two-point low-pass, the all-pass
// and the loss, written back; the note ends once the string has rung out
func (v *TonalVoice) pluck() float64 {
	d := v.str[v.strPos]
	s := (1-v.strS)*d + v.strS*v.strLast
	a := v.apC*(s-v.apOut) + v.apIn
	v.strLast, v.apIn, v.apOut = d, s, a
	y := v.strLoss * a
	v.str[v.strPos] = y
	if v.strPos++; v.strPos == v.strLen {
		v.strPos = 0
	}
	if v.check++; v.check >= ringCheck {
		v.check = 0
		var e float64
		for _, x := range v.str[:v.strLen] {
			e += x * x
		}
		if e < ringFloor*float64(v.strLen) {
			v.Release()
		}
	}
	return y
}

// strike tunes the string to the note and loads one period with the pluck.
// The fundamental's T60 is ring at middle C, shorter above it and longer below
// by decay_track (none: ring at every pitch). bright sets the low-pass's
// stretch, lessened where its loss at the fundamental alone would end the note
// before that T60;
// the period less the low-pass's phase delay is the delay line's whole
// samples and the all-pass's fraction, kept in [0.1, 1.1) off its pole.
func (v *TonalVoice) strike(sp *VoiceDef) {
	sr := float64(AudioSampleRate)
	w := 2 * math.Pi * v.freq / sr
	t60 := max(sp.Ring, 0.05) * math.Pow(keyRefHz/v.freq, sp.DecayTrack)
	g := math.Pow(0.001, 1/(t60*v.freq))
	s := (1 - sp.Bright) / 2
	if 1-2*s*(1-s)*(1-math.Cos(w)) < g*g {
		s = 0.5 - math.Sqrt(max(0.25-(1-g*g)/(2*(1-math.Cos(w))), 0))
	}
	v.strS = s
	v.strLoss = min(g/math.Sqrt(1-2*s*(1-s)*(1-math.Cos(w))), 1)
	p := sr/v.freq - math.Atan2(s*math.Sin(w), 1-s+s*math.Cos(w))/w
	n := min(max(int(p-0.1), 1), stringMaxLen)
	x := min(max(p-float64(n), 0.1), 1.1)
	v.apC = math.Sin((1-x)*w/2) / math.Sin((1+x)*w/2)
	v.strLen, v.strPos, v.strLast, v.apIn, v.apOut, v.check = n, 0, 0, 0, 0, 0
	v.pluckBurst(sp, p)
}

// pluckBurst loads the string with the pluck: a noise burst from a small
// generator seeded by the note and the trigger order, so a replay plucks the
// same string, through the comb a pluck strike_pos along the string leaves and
// the velocity's low-pass, hammer_hz at full velocity; zero mean, peak one.
func (v *TonalVoice) pluckBurst(sp *VoiceDef, period float64) {
	if cap(v.str) < stringMaxLen*3/2 {
		v.str = make([]float64, stringMaxLen*3/2)
	}
	n := v.strLen
	h := sp.StrikePos
	if h <= 0 {
		h = strikeAt
	}
	m := min(int(h*period+0.5), n/2)
	str := v.str[:n+m]
	seed := v.noteSeed()
	for i := range n {
		seed = xorshift(seed)
		str[i] = float64(seed)/float64(1<<32)*2 - 1
	}
	// the burst is one period of a periodic signal, so the comb wraps round it
	if m > 0 {
		copy(str[n:], str[:m])
		for i := range n {
			str[i] -= str[i+m]
		}
	}
	str = str[:n]
	if sp.HammerHz > 0 {
		a := 1 - math.Exp(-2*math.Pi*sp.HammerHz*(0.3+0.7*max(v.velocity, 0.05))/AudioSampleRate)
		lowpassRound(str, a)
		lowpassRound(str, a)
	}
	var mean, peak float64
	for _, x := range str {
		mean += x
	}
	mean /= float64(n)
	for i := range str {
		str[i] -= mean
		peak = max(peak, math.Abs(str[i]))
	}
	if peak > 0 {
		for i := range str {
			str[i] /= peak
		}
	}
}

// lowpassRound runs a one-pole low-pass of coefficient a over one period of a
// periodic signal in place, from the state the period returns to
func lowpassRound(x []float64, a float64) {
	var y float64
	for _, s := range x {
		y += a * (s - y)
	}
	y /= 1 - math.Pow(1-a, float64(len(x)))
	for i, s := range x {
		y += a * (s - y)
		x[i] = y
	}
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
		v.knockSeed = xorshift(v.knockSeed)
		n := float64(v.knockSeed)/float64(1<<32)*2 - 1
		v.knockLP += 0.25 * (n - v.knockLP) // a 2 kHz one-pole
		s += v.knockLP * v.knockEnv
		v.knockEnv *= 0.9943 // 4 ms
	}
	if v.check++; v.check >= ringCheck {
		v.check = 0
		var e float64
		for i := range v.mre {
			e += v.mre[i]*v.mre[i] + v.mim[i]*v.mim[i]
		}
		if e*v.mscale*v.mscale < ringFloor {
			v.Release()
		}
	}
	return s
}

// excite builds the modal bank for the note: partial k sits at k·f0 stretched by
// the inharmonicity, at the amplitude the strike position's comb, the spectral
// tilt, the body's high-pass and the hammer's velocity-led low-pass leave it,
// decaying faster the higher it is; the lowest partials the body sounds get the
// coupled pair's slower, slightly detuned second string, beating against the first
func (v *TonalVoice) excite(sp *VoiceDef) {
	n := max(min(sp.Partials, maxPartials, int(modalTopHz/v.freq)), 4)
	if cap(v.mre) < maxPartials+pairPartials {
		c := maxPartials + pairPartials
		v.mre, v.mim, v.mcr, v.mci = make([]float64, 0, c), make([]float64, 0, c), make([]float64, 0, c), make([]float64, 0, c)
	}
	v.mre, v.mim, v.mcr, v.mci = v.mre[:0], v.mim[:0], v.mcr[:0], v.mci[:0]
	h := sp.StrikePos
	if h <= 0 {
		h = strikeAt
	}
	tilt := sp.Tilt
	if tilt <= 0 {
		tilt = 1
	}
	vel := max(v.velocity, 0.05)
	b := sp.Inharm * math.Pow(v.freq/keyRefHz, sp.InharmTrack)
	tau := max(sp.DecayTime, 0.05) * math.Pow(keyRefHz/v.freq, sp.DecayTrack)
	fc := sp.HammerHz * math.Pow(vel, hammerVelExp)
	body := sp.Bright * bodyHz
	pair0 := max(int(math.Round(body/v.freq)), 1)
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
		if body > 0 {
			r := (f / body) * (f / body) * (f / body)
			a *= r / math.Sqrt(1+r*r)
		}
		if fc > 0 {
			a /= 1 + (f/fc)*(f/fc)
		}
		sigma := 1 / tau
		if sp.DampHz > 0 {
			sigma *= 1 + (f/sp.DampHz)*(f/sp.DampHz)
		}
		if k >= pair0 && k < pair0+pairPartials && sp.PairLevel > 0 {
			// both strings start in phase; where the second overtakes the
			// first, at cross, a beat that would leave them over a quarter
			// cycle apart moves to a quarter, so the hand-over never cancels
			f2 := f * detune
			if slow < 1 && detune != 1 {
				if cross := math.Log(1/sp.PairLevel) / (sigma * (1 - slow)); cross > 0 {
					beats := math.Abs(f2-f) * cross
					whole, part := math.Modf(beats)
					if part > 0.25 && part < 0.75 {
						part = 0.25 + 0.5*math.Round(part-0.25)
						f2 = f + math.Copysign((whole+part)/cross, detune-1)
					}
				}
			}
			add(f, a/(1+sp.PairLevel), sigma)
			add(f2, a*sp.PairLevel/(1+sp.PairLevel), sigma*slow)
		} else {
			add(f, a, sigma)
		}
	}
	v.mscale = 1 / max(sum, 1e-9)
	v.check = 0
	v.knockEnv, v.knockLP = sp.Knock*vel, 0
	v.knockSeed = v.noteSeed()
}

// filter is a trapezoidal state-variable low-pass whose cutoff follows the key and
// the filter envelope, which opens as far as the velocity takes it; coefficients
// refresh every filterCoefHold samples
func (v *TonalVoice) filter(x float64) float64 {
	sp := v.spec
	if sp.EnvDecay > 0 {
		v.fenv *= v.fenvCoef
	} else {
		v.fenv = v.envLevel
	}
	if v.coefLeft--; v.coefLeft <= 0 {
		v.coefLeft = filterCoefHold
		fc := sp.Cutoff + sp.KeyTrack*v.freq + sp.EnvAmount*v.fenv*v.velocity
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
	// Unison spreads symmetrically about the note; a stack's oscillators start
	// at their own phases, as free-running ones do, and wander in pitch when
	// detuned. A lone one starts at 0: its start phase is inaudible, and the
	// rip's analysis of two lines on one instrument hears it
	n := max(min(sp.Unison, maxUnison), 1)
	detune := sp.Detune
	if detune <= 0 {
		detune = 1
	}
	v.depth = 0
	if n > 1 {
		v.depth = driftShare * math.Abs(detune-1)
	}
	seed := v.noteSeed()
	unit := func() float64 { seed = xorshift(seed); return float64(seed) / (1 << 32) }
	for i := range n {
		v.ratio[i] = math.Pow(detune, float64(i)-float64(n-1)/2)
		v.wander[i] = v.ratio[i]
		v.phase[i] = 0
		if n > 1 {
			v.phase[i] = unit()
		}
		v.drift[i] = unit()
		v.driftHz[i] = driftMinHz + (driftMaxHz-driftMinHz)*unit()
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
	if v.wave == waveString || v.wave == waveModal {
		// a string fades as it rings, never held at a sustain; release is its damper
		v.decay, v.sustain = 0, 1
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

// Peaking is the cookbook's peaking filter, db at hz over a width of q, as the
// function of the next sample a mix's equaliser band is
func Peaking(hz, db, q float64) func(float64) float64 {
	gain := math.Pow(10, db/40)
	w := 2 * math.Pi * min(hz, 0.45*AudioSampleRate) / AudioSampleRate
	alpha := math.Sin(w) / (2 * q)
	a0 := 1 + alpha/gain
	b := biquad{b0: (1 + alpha*gain) / a0, b1: -2 * math.Cos(w) / a0, b2: (1 - alpha*gain) / a0, a1: -2 * math.Cos(w) / a0, a2: (1 - alpha/gain) / a0}
	return b.process
}

func (b *biquad) process(x float64) float64 {
	y := b.b0*x + b.b1*b.x1 + b.b2*b.x2 - b.a1*b.y1 - b.a2*b.y2
	b.x2, b.x1, b.y2, b.y1 = b.x1, x, b.y1, y
	return y
}

// noteSeed seeds a note's noise and phases by its pitch and trigger order, so a
// replay plays the same note
func (v *TonalVoice) noteSeed() uint32 {
	return uint32(v.note)*2654435761 + uint32(v.serial)*40503 + 1
}

func xorshift(s uint32) uint32 {
	s ^= s << 13
	s ^= s >> 17
	return s ^ s<<5
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
