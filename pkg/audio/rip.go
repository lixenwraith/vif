package audio

import (
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"slices"
	"sort"

	"github.com/lixenwraith/toml"
)

// RipParams tunes the analysis a Rip makes of a recording. DefaultRipParams is
// the hand-set starting point; a search over material the engine renders, whose
// patterns are known, refines them (RenderMusic, ExpectedRip, CompareRips), and
// a [rip] table in a TOML document carries the result (LoadRipParams).
type RipParams struct {
	TempoPrior float64 `toml:"tempo_prior"` // the BPM the tempo prior centres on
	TempoWidth float64 `toml:"tempo_width"` // the prior's width, in octaves
	Tightness  float64 `toml:"tightness"`   // how hard beat tracking holds to the period

	OnsetFloor float64 `toml:"onset_floor"` // a hit rises this far above its band's local median
	OnsetMin   float64 `toml:"onset_min"`   // and this far, as a share of the band's loudest, at least
	OnsetGap   float64 `toml:"onset_gap"`   // seconds between two hits of one drum
	KickShare  float64 `toml:"kick_share"`  // the share of a hit's percussive rise under 100 Hz that makes it a kick
	SnareShare float64 `toml:"snare_share"` // the share between 200 Hz and 4 kHz a snare has at least
	SnareNoise float64 `toml:"snare_noise"` // and the share above 5 kHz its wires give it at least
	HatShare   float64 `toml:"hat_share"`   // the share above 5 kHz that makes a hit a hihat

	Harmonics     int     `toml:"harmonics"`      // partials summed into a pitch's salience
	HarmonicDecay float64 `toml:"harmonic_decay"` // the weight ratio between successive partials
	Voicing       float64 `toml:"voicing"`        // the salience, against the recording's loud frames, under which a frame carries no note
	Jump          float64 `toml:"jump"`           // the pitch track's cost per semitone moved
	Switch        float64 `toml:"switch"`         // its cost of a note starting or ending
	Retrigger     float64 `toml:"retrigger"`      // the fraction of a note's peak salience it dips under before a rise is a new note
	MinNote       float64 `toml:"min_note"`       // seconds a note lasts at least
}

func DefaultRipParams() RipParams {
	return RipParams{
		TempoPrior: 120, TempoWidth: 1, Tightness: 100,
		OnsetFloor: 2.0, OnsetMin: 0.1, OnsetGap: 0.06, KickShare: 0.06, SnareShare: 0.45, SnareNoise: 0.1, HatShare: 0.3,
		Harmonics: 6, HarmonicDecay: 0.8, Voicing: 0.3, Jump: 0.06, Switch: 0.35, Retrigger: 0.4, MinNote: 0.06,
	}
}

// ripGene is one parameter as a search sees it: its range, and where it lives.
type ripGene struct {
	name     string
	min, max float64
	at       func(*RipParams) *float64
}

// ripGenes is the parameters in gene order with the range each is sane in; the
// partial count is the one integer, rounded when read back.
var ripGenes = []ripGene{
	{"tempo_prior", 90, 150, func(p *RipParams) *float64 { return &p.TempoPrior }},
	{"tempo_width", 0.5, 2, func(p *RipParams) *float64 { return &p.TempoWidth }},
	{"tightness", 20, 400, func(p *RipParams) *float64 { return &p.Tightness }},
	{"onset_floor", 1.2, 4, func(p *RipParams) *float64 { return &p.OnsetFloor }},
	{"onset_min", 0.02, 0.3, func(p *RipParams) *float64 { return &p.OnsetMin }},
	{"onset_gap", 0.03, 0.12, func(p *RipParams) *float64 { return &p.OnsetGap }},
	{"kick_share", 0.02, 0.3, func(p *RipParams) *float64 { return &p.KickShare }},
	{"snare_share", 0.3, 0.7, func(p *RipParams) *float64 { return &p.SnareShare }},
	{"snare_noise", 0.02, 0.3, func(p *RipParams) *float64 { return &p.SnareNoise }},
	{"hat_share", 0.15, 0.6, func(p *RipParams) *float64 { return &p.HatShare }},
	{"harmonics", 3, 8, nil},
	{"harmonic_decay", 0.5, 0.95, func(p *RipParams) *float64 { return &p.HarmonicDecay }},
	{"voicing", 0.1, 0.6, func(p *RipParams) *float64 { return &p.Voicing }},
	{"jump", 0.01, 0.2, func(p *RipParams) *float64 { return &p.Jump }},
	{"switch", 0.1, 0.8, func(p *RipParams) *float64 { return &p.Switch }},
	{"retrigger", 0.2, 0.7, func(p *RipParams) *float64 { return &p.Retrigger }},
	{"min_note", 0.03, 0.15, func(p *RipParams) *float64 { return &p.MinNote }},
}

func (g *ripGene) get(p *RipParams) float64 {
	if g.at == nil {
		return float64(p.Harmonics)
	}
	return *g.at(p)
}

func (g *ripGene) set(p *RipParams, v float64) {
	if g.at == nil {
		p.Harmonics = int(math.Round(v))
		return
	}
	*g.at(p) = v
}

// RipGeneRanges is the range of each gene of Genes, in its order, for a search
// to stay inside.
func RipGeneRanges() [][2]float64 {
	out := make([][2]float64, len(ripGenes))
	for i, g := range ripGenes {
		out[i] = [2]float64{g.min, g.max}
	}
	return out
}

// Genes is the parameters as a vector a search moves, in RipGeneRanges' order.
func (p RipParams) Genes() []float64 {
	out := make([]float64, len(ripGenes))
	for i := range ripGenes {
		out[i] = ripGenes[i].get(&p)
	}
	return out
}

// RipParamsFromGenes is Genes' inverse, each gene held to its range.
func RipParamsFromGenes(genes []float64) RipParams {
	p := DefaultRipParams()
	for i := range ripGenes {
		if i < len(genes) {
			ripGenes[i].set(&p, min(max(genes[i], ripGenes[i].min), ripGenes[i].max))
		}
	}
	return p
}

// ValidateRipParams refuses a parameter outside its range, by its key.
func ValidateRipParams(p RipParams) error {
	for i := range ripGenes {
		if v := ripGenes[i].get(&p); !finite(v) || v < ripGenes[i].min || v > ripGenes[i].max {
			return fmt.Errorf("rip: %s %v outside [%v,%v]", ripGenes[i].name, v, ripGenes[i].min, ripGenes[i].max)
		}
	}
	return nil
}

// ripFile is the root of a rip parameters document.
type ripFile struct {
	Rip RipParams `toml:"rip"`
}

// LoadRipParams reads a [rip] table, every key present and in range; a key the
// document omits keeps its default.
func LoadRipParams(data []byte) (RipParams, error) {
	raw, err := toml.NewParser(data).Parse()
	if err != nil {
		return RipParams{}, fmt.Errorf("rip params: %w", err)
	}
	if err := checkKeys("", raw, reflect.TypeOf(ripFile{})); err != nil {
		return RipParams{}, fmt.Errorf("rip params: %w", err)
	}
	f := ripFile{Rip: DefaultRipParams()}
	if err := toml.Decode(raw, &f); err != nil {
		return RipParams{}, fmt.Errorf("rip params: %w", err)
	}
	if err := ValidateRipParams(f.Rip); err != nil {
		return RipParams{}, err
	}
	return f.Rip, nil
}

// MarshalRipParams writes the parameters as a document LoadRipParams reads.
func MarshalRipParams(p RipParams) ([]byte, error) {
	return toml.Marshal(ripFile{Rip: p})
}

// Rip is what the analysis heard in a recording: its tempo, where its bars fall,
// its key, the chord root each bar leans on, and per bar the drum hits, the
// melody and the bass line as steps of the engine's grid. Patterns writes it as
// patterns the sequencer plays.
type Rip struct {
	BPM         int
	Root        int // MIDI, in the bass octave as the engine's harmony root is
	Scale       ScaleID
	Progression []int   // chord root degree per bar, as SetHarmony takes it
	Downbeat    float64 // seconds into the recording the first full bar starts
	Bars        []RipBar
	Params      RipParams
}

// RipBar is one bar: each drum's hits, kick, snare and hihat in instrument order,
// and the melody and bass notes, whose degrees are relative to the Rip's key.
type RipBar struct {
	Drums  [InstrClap][]Step
	Melody []Step
	Bass   []Step
}

// The analysis rate and transform: 93 ms frames of 10.8 Hz bins every 11.6 ms,
// the bands the drums are told apart by, and the pitches the salience spans.
const (
	ripRate     = 22050
	ripN        = 2048
	ripHop      = 256
	ripKernel   = 17    // frames and bins a median spans in the harmonic-percussive split
	ripSubHz    = 100.0 // a kick's body is under it; a snare's is not
	ripKickHz   = 200.0
	ripSnareHz  = 4000.0
	ripHatHz    = 5000.0
	ripMinMIDI  = 28 // E1
	ripBassTop  = 52 // the bass line lives under E3
	ripMelodyLo = 53 // the melody from F3 up
	ripMaxMIDI  = 97 // C7 exclusive
	ripPitches  = ripMaxMIDI - ripMinMIDI
	ripMaxSec   = 20 * 60
)

// RipWAV reads a WAV stream and rips it.
func RipWAV(r io.Reader, p RipParams) (*Rip, error) {
	x, rate, err := ReadWAV(r)
	if err != nil {
		return nil, err
	}
	return RipSamples(x, rate, p)
}

// RipSamples rips a mono recording at rate: tempo and bars first, then the key,
// the drums on a sixteenth grid between the beats, and the melody and bass as
// pitch tracks cut into notes on that grid. Notes off the key move to its nearest
// degree, which is all a pattern can say.
func RipSamples(x []float64, rate int, p RipParams) (*Rip, error) {
	if rate <= 0 || len(x) < 4*rate {
		return nil, errors.New("rip: the recording is shorter than four seconds")
	}
	if len(x) > ripMaxSec*rate {
		x = x[:ripMaxSec*rate]
	}
	f := analyse(resample(x, rate, ripRate), p)
	bpm, period := f.tempo(p)
	beats := f.beats(period, p)
	if len(beats) < BeatsPerBar+1 {
		return nil, errors.New("rip: no full bar found")
	}
	hits := f.drumHits(p)
	root, scale := f.key()
	g := f.grid(beats, hits, root%12)
	r := &Rip{BPM: int(math.Round(bpm)), Root: root, Scale: scale, Downbeat: float64(g.beats[g.first]) * f.hop, Bars: make([]RipBar, g.bars), Params: p}
	r.Progression = f.progression(g, r.Root, r.Scale)
	g.drums(r, hits)
	for _, n := range f.notes(ripMelodyLo, ripMaxMIDI, p) {
		f.place(g, r, n, func(b *RipBar) *[]Step { return &b.Melody })
	}
	for _, n := range f.notes(ripMinMIDI, ripBassTop, p) {
		f.place(g, r, n, func(b *RipBar) *[]Step { return &b.Bass })
	}
	return r, nil
}

// Patterns writes the rip as patterns of up to bars bars each, named
// <name>_beat_<n>, <name>_melody_<n> and <name>_bass_<n>, in a group of their
// own and no tier, so they play where they are placed; a window with nothing in
// a part has no pattern for it.
func (r *Rip) Patterns(name string, bars int) []*PatternDef {
	bars = min(max(bars, 1), MaxPatternLen/StepsPerBar)
	var out []*PatternDef
	for w := 0; w*bars < len(r.Bars); w++ {
		lo, hi := w*bars, min((w+1)*bars, len(r.Bars))
		steps := (hi - lo) * StepsPerBar
		suffix := fmt.Sprintf("_%d", w+1)
		beat := &PatternDef{Name: name + "_beat" + suffix, Steps: steps, Role: "rhythm", Groups: []string{name}}
		for d := InstrKick; d < InstrClap; d++ {
			var evs []StepDef
			for b := lo; b < hi; b++ {
				for _, s := range r.Bars[b].Drums[d] {
					evs = append(evs, StepDef{Pos: s.Pos + (b-lo)*StepsPerBar, Vel: s.Vel})
				}
			}
			if len(evs) > 0 {
				beat.Track = append(beat.Track, TrackDef{Instr: d.String(), Event: evs})
			}
		}
		if len(beat.Track) > 0 {
			out = append(out, beat)
		}
		line := func(part string, instr string, role string, pick func(*RipBar) []Step) {
			var evs []StepDef
			for b := lo; b < hi; b++ {
				for _, s := range pick(&r.Bars[b]) {
					evs = append(evs, StepDef{Pos: s.Pos + (b-lo)*StepsPerBar, Vel: s.Vel, Deg: s.Deg, Oct: s.Oct, Dur: min(s.Dur, MaxPatternLen)})
				}
			}
			if len(evs) > 0 {
				out = append(out, &PatternDef{Name: name + "_" + part + suffix, Steps: steps, Role: role, Groups: []string{name},
					Track: []TrackDef{{Instr: instr, Event: evs}}})
			}
		}
		line("melody", "lead", "melody", func(b *RipBar) []Step { return b.Melody })
		line("bass", "bass", "bass", func(b *RipBar) []Step { return b.Bass })
	}
	return out
}

// ripFrames is the recording as its frames: the percussive onset strength in all
// and in each drum's band, the rise of percussive magnitude per band that tells
// the drums apart, the pitch salience, the bass register's by its first partials
// and the melody's with the bass cancelled, and the harmonic chroma.
type ripFrames struct {
	hop                float64 // seconds a frame advances
	n                  int
	flux, lo, mid, hi  []float64
	rSub, rLo, rMid    []float64 // rises under 100 Hz, to 200 Hz and to 4 kHz
	rHi                []float64 // and above 5 kHz
	sal                []float32 // n * ripPitches
	chroma, bassChroma [][12]float64
}

// analyse walks the spectrogram once. Each frame's magnitude is split into a
// harmonic and a percussive part by the medians across time and across frequency
// around it, so a frame is finished ripKernel/2 frames after it arrives.
func analyse(x []float64, p RipParams) *ripFrames {
	const bins, half = ripN/2 + 1, ripKernel / 2
	binHz := float64(ripRate) / ripN
	f := &ripFrames{hop: float64(ripHop) / ripRate}
	ring := make([][]float64, ripKernel)
	for i := range ring {
		ring[i] = make([]float64, bins)
	}
	timeMed := make([]*slidingMedian, bins)
	for k := range timeMed {
		timeMed[k] = newSlidingMedian(ripKernel)
	}
	freqMed := newSlidingMedian(ripKernel)
	hm, pm, prevPM, lp, prevLP, hrow, prow := make([]float64, bins), make([]float64, bins), make([]float64, bins), make([]float64, bins), make([]float64, bins), make([]float64, bins), make([]float64, bins)
	cancelled := make([]float64, bins)
	weights := make([]float64, max(p.Harmonics, 1))
	for h := range weights {
		weights[h] = math.Pow(p.HarmonicDecay, float64(h))
	}
	pitchHz := make([]float64, ripPitches)
	for i := range pitchHz {
		pitchHz[i] = NoteFreq(ripMinMIDI + i)
	}
	subBin, loBin, midBin, hatBin := int(ripSubHz/binHz), int(ripKickHz/binHz), int(ripSnareHz/binHz), int(ripHatHz/binHz)
	// partial reads the magnitude at a frequency between its bins, which tells
	// neighbouring semitones apart where a bin is wider than one; zero past the band
	partial := func(m []float64, hz float64) float64 {
		pos := hz / binHz
		b := int(pos)
		if b+1 >= bins {
			return 0
		}
		frac := pos - float64(b)
		return m[b]*(1-frac) + m[b+1]*frac
	}
	// salience sums a pitch's partials, each weighted by its rank, scaled down as
	// the fundamental falls short of the strongest partial, which a note's own
	// octave above and a bass's second partial both do
	salience := func(m []float64, f0 float64, n int) float64 {
		var s, top float64
		for h := 0; h < n && h < len(weights); h++ {
			v := partial(m, f0*float64(h+1))
			s, top = s+weights[h]*v, max(top, v)
		}
		if top > 0 {
			s *= min(1, partial(m, f0)/(0.3*top))
		}
		return s
	}

	finish := func(c int) {
		row := ring[c%ripKernel]
		for k := range hrow {
			hrow[k] = timeMed[k].sorted[len(timeMed[k].sorted)/2]
		}
		freqMed.reset()
		for k := 0; k < bins+half; k++ {
			v := 0.0
			if k < bins {
				v = row[k]
			}
			m := freqMed.push(v)
			if j := k - half; j >= 0 {
				prow[j] = m
			}
		}
		var flux, lo, mid, hi, rSub, rLo, rMid, rHi float64
		for k := range row {
			h2, p2 := hrow[k]*hrow[k], prow[k]*prow[k]
			mask := h2 / (h2 + p2 + 1e-12)
			hm[k], pm[k] = row[k]*mask, row[k]*(1-mask)
			lp[k] = math.Log1p(100 * pm[k])
			d := max(lp[k]-prevLP[k], 0)
			r := max(pm[k]-prevPM[k], 0)
			flux += d
			switch {
			case k < subBin:
				lo, rSub = lo+d, rSub+r
			case k < loBin:
				lo, rLo = lo+d, rLo+r
			case k < midBin:
				mid, rMid = mid+d, rMid+r
			case k >= hatBin:
				hi, rHi = hi+d, rHi+r
			}
		}
		prevLP, lp = lp, prevLP
		prevPM, pm = pm, prevPM
		f.flux, f.lo, f.mid, f.hi = append(f.flux, flux), append(f.lo, lo), append(f.mid, mid), append(f.hi, hi)
		f.rSub, f.rLo, f.rMid, f.rHi = append(f.rSub, rSub), append(f.rLo, rLo), append(f.rMid, rMid), append(f.rHi, rHi)
		// the chroma reads every pitch's full series: a ghost an octave up shares
		// the pitch class of what casts it
		var ch, bch [12]float64
		for i, f0 := range pitchHz {
			ch[(ripMinMIDI+i)%12] += salience(hm, f0, len(weights))
		}
		// the bass is the strongest pitch of its register by its first partials;
		// those partials come out of what the melody is read from
		bass, bassAt := -1, 0.0
		for i := 0; i < ripBassTop-ripMinMIDI; i++ {
			s := salience(hm, pitchHz[i], 3)
			f.sal = append(f.sal, float32(s))
			bch[(ripMinMIDI+i)%12] += s
			if s > bassAt {
				bass, bassAt = i, s
			}
		}
		copy(cancelled, hm)
		if bass >= 0 {
			for h := 1; h <= 3; h++ {
				if b := int(pitchHz[bass]*float64(h)/binHz + 0.5); b+1 < bins && b >= 1 {
					cancelled[b-1], cancelled[b], cancelled[b+1] = 0, 0, 0
				}
			}
		}
		for i := ripBassTop - ripMinMIDI; i < ripPitches; i++ {
			f.sal = append(f.sal, float32(salience(cancelled, pitchHz[i], len(weights))))
		}
		f.chroma, f.bassChroma = append(f.chroma, ch), append(f.bassChroma, bch)
		f.n++
	}
	frames := 0
	push := func(t int, mag []float64) {
		row := ring[t%ripKernel]
		copy(row, mag)
		for k, v := range mag {
			timeMed[k].push(v)
		}
		if c := t - half; c >= 0 {
			finish(c)
		}
	}
	spectrogram(x, ripN, ripHop, func(t int, mag []float64) {
		frames = t + 1
		push(t, mag)
	})
	zero := make([]float64, bins)
	for t := frames; t < frames+half; t++ { // the last frames' windows run past the end
		push(t, zero)
	}
	return f
}

func (m *slidingMedian) reset() {
	m.n, m.pos = 0, 0
	m.sorted = m.sorted[:0]
}

// tempo is the lag the onset strength repeats at, under a log-Gaussian prior on
// the tempo and a comb that credits a lag with its double, folded into the
// sequencer's range; the period is in frames.
func (f *ripFrames) tempo(p RipParams) (bpm, period float64) {
	o := f.onsets()
	minLag, maxLag := int(60/240/f.hop), int(60/60/f.hop)
	acf := func(lag int) float64 {
		var s float64
		for t := lag; t < len(o); t++ {
			s += o[t] * o[t-lag]
		}
		return s / float64(max(len(o)-lag, 1))
	}
	score := func(lag int) float64 {
		b := 60 / (float64(lag) * f.hop)
		prior := math.Exp(-0.5 * math.Pow(math.Log2(b/p.TempoPrior)/p.TempoWidth, 2))
		s := acf(lag)
		if 2*lag < len(o) {
			s += 0.5 * acf(2*lag)
		}
		return s * prior
	}
	best, bestScore := minLag, -1.0
	for lag := minLag; lag <= maxLag; lag++ {
		if s := score(lag); s > bestScore {
			best, bestScore = lag, s
		}
	}
	lag := float64(best)
	if best > minLag && best < maxLag { // the peak between its neighbours
		l, c, r := score(best-1), score(best), score(best+1)
		if d := l - 2*c + r; d < 0 {
			lag += 0.5 * (l - r) / d
		}
	}
	bpm = 60 / (lag * f.hop)
	for bpm < MinBPM {
		bpm *= 2
	}
	for bpm > MaxBPM {
		bpm /= 2
	}
	return bpm, 60 / (bpm * f.hop)
}

// onsets is what the tempo and the beats are read from: each band's onset
// strength against its own peak, the kick's band counting double and the hihat's
// half, so a broadband hat does not outvote the drum that keeps the beat; above
// its local mean, clipped at zero and scaled to unit deviation.
func (f *ripFrames) onsets() []float64 {
	n := f.n
	o := make([]float64, n)
	peak := func(v []float64) float64 {
		m := 1e-9
		for _, x := range v {
			m = max(m, x)
		}
		return m
	}
	pl, pm, ph := peak(f.lo), peak(f.mid), peak(f.hi)
	raw := make([]float64, n)
	for t := range raw {
		raw[t] = 2*f.lo[t]/pl + f.mid[t]/pm + 0.5*f.hi[t]/ph
	}
	w := int(1 / f.hop)
	sum := make([]float64, n+1)
	for t, v := range raw {
		sum[t+1] = sum[t] + v
	}
	var sq float64
	for t := range o {
		a, b := max(t-w, 0), min(t+w+1, n)
		o[t] = max(raw[t]-(sum[b]-sum[a])/float64(b-a), 0)
		sq += o[t] * o[t]
	}
	if sd := math.Sqrt(sq / float64(max(n, 1))); sd > 0 {
		for t := range o {
			o[t] /= sd
		}
	}
	return o
}

// beats places beats one period apart, give or take what the onsets justify: each
// frame's best score is its onset strength plus the best earlier beat's, less a
// penalty growing with the square of the log of the interval's ratio to the period.
func (f *ripFrames) beats(period float64, p RipParams) []int {
	o := f.onsets()
	n := len(o)
	score, back := make([]float64, n), make([]int, n)
	lo, hi := int(period/2+0.5), int(2*period+0.5)
	for t := range n {
		best, arg := 0.0, -1
		for d := lo; d <= hi && d <= t; d++ {
			s := score[t-d] - p.Tightness*math.Pow(math.Log(float64(d)/period), 2)
			if arg < 0 || s > best {
				best, arg = s, t-d
			}
		}
		score[t], back[t] = o[t]+best, arg
	}
	last := n - 1
	for t := max(n-int(period)-1, 0); t < n; t++ {
		if score[t] > score[last] {
			last = t
		}
	}
	var beats []int
	for t := last; t >= 0; t = back[t] {
		beats = append(beats, t)
	}
	slices.Reverse(beats)
	return beats
}

// ripGrid is the bars over the beats: the beat the first full bar starts on, and
// how many full bars follow it.
type ripGrid struct {
	beats       []int
	first, bars int
}

// grid chooses the beat the bars start on: the phase whose bar lines carry a
// kick and no snare and the key's root in the bass, whose second and fourth beats
// carry the snare, and whose bars change chroma the most from the bar before;
// the earliest of equals.
func (f *ripFrames) grid(beats []int, hits [InstrClap][]ripHit, rootPC int) ripGrid {
	bassRoot := func(a, b int) float64 {
		var c [12]float64
		for t := a; t < b && t < f.n; t++ {
			for pc := range c {
				c[pc] += f.bassChroma[t][pc]
			}
		}
		normalize(&c)
		return c[rootPC]
	}
	near := func(d InstrumentType, frame int) float64 {
		i := sort.Search(len(hits[d]), func(k int) bool { return hits[d][k].frame >= frame-2 })
		if i < len(hits[d]) && hits[d][i].frame <= frame+2 {
			return 1
		}
		return 0
	}
	meanChroma := func(a, b int) [12]float64 {
		var c [12]float64
		for t := a; t < b && t < f.n; t++ {
			for pc := range c {
				c[pc] += f.chroma[t][pc]
			}
		}
		return c
	}
	best, bestScore := 0, math.Inf(-1)
	for phase := range BeatsPerBar {
		var s float64
		n := 0
		for i := phase; i+BeatsPerBar < len(beats); i += BeatsPerBar {
			b := beats[i]
			s += near(InstrKick, b) - near(InstrSnare, b) + near(InstrSnare, beats[i+1]) + near(InstrSnare, beats[i+3])
			s += bassRoot(b, beats[i+1])
			if i >= BeatsPerBar {
				s += 1 - cosine(meanChroma(beats[i-BeatsPerBar], b), meanChroma(b, beats[i+BeatsPerBar]))
			}
			n++
		}
		if n > 0 && s/float64(n) > bestScore+1e-9 {
			best, bestScore = phase, s/float64(n)
		}
	}
	return ripGrid{beats: beats, first: best, bars: (len(beats) - best - 1) / BeatsPerBar}
}

func cosine(a, b [12]float64) float64 {
	var dot, na, nb float64
	for i := range a {
		dot, na, nb = dot+a[i]*b[i], na+a[i]*a[i], nb+b[i]*b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / math.Sqrt(na*nb)
}

// at is the bar and sixteenth a frame falls in, each beat's span cut in four,
// false before the first bar or after the last.
func (g *ripGrid) at(frame int) (bar, step int, ok bool) {
	i := sort.SearchInts(g.beats, frame+1) - 1
	if i < g.first {
		return 0, 0, false
	}
	rel := i - g.first
	bar, j := rel/BeatsPerBar, rel%BeatsPerBar
	if bar >= g.bars {
		return 0, 0, false
	}
	b0, b1 := g.beats[i], g.beats[i+1]
	step = StepsPerBeat*j + int(math.Round(float64(frame-b0)/float64(b1-b0)*StepsPerBeat))
	if step >= StepsPerBar {
		bar, step = bar+1, 0
		if bar >= g.bars {
			return 0, 0, false
		}
	}
	return bar, step, true
}

// stepFrames is the mean length of a sixteenth over the bars
func (g *ripGrid) stepFrames() float64 {
	span := g.beats[g.first+BeatsPerBar*g.bars] - g.beats[g.first]
	return float64(span) / float64(StepsPerBar*g.bars)
}

// frames of a bar: its first beat's frame to the next bar's
func (g *ripGrid) span(bar int) (int, int) {
	i := g.first + BeatsPerBar*bar
	return g.beats[i], g.beats[i+BeatsPerBar]
}

// key sums the chroma over the recording and takes the root and scale whose
// tones hold the most of it and whose other tones the least, the tonic and fifth
// weighing extra and the bass register voting for the tonic; the root lands in
// the octave the engine's harmony root lives in.
func (f *ripFrames) key() (int, ScaleID) {
	var c, b [12]float64
	for t := range f.n {
		for pc := range c {
			c[pc] += f.chroma[t][pc]
			b[pc] += f.bassChroma[t][pc]
		}
	}
	normalize(&c)
	normalize(&b)
	root, scale, best := 0, ScalePhrygian, math.Inf(-1)
	for _, s := range []ScaleID{ScaleMajor, ScaleMinor, ScaleDorian, ScalePhrygian, ScaleHarmonicMinor, ScaleMinorPent} {
		for r := range 12 {
			var in [12]bool
			for _, iv := range scaleTable[s] {
				in[(r+iv)%12] = true
			}
			var score float64
			for pc := range 12 {
				if in[pc] {
					score += c[pc]
				} else {
					score -= c[pc]
				}
			}
			score += c[r] + 0.5*c[(r+7)%12] + b[r]
			if score > best {
				root, scale, best = r, s, score
			}
		}
	}
	return root + 3*12, scale
}

func normalize(c *[12]float64) {
	var sum float64
	for _, v := range c {
		sum += v
	}
	if sum > 0 {
		for i := range c {
			c[i] /= sum
		}
	}
}

// progression names each bar's chord as the degree whose triad the bar's
// chroma favours.
func (f *ripFrames) progression(g ripGrid, root int, scale ScaleID) []int {
	iv := scaleTable[scale]
	prog := make([]int, g.bars)
	for bar := range prog {
		var c [12]float64
		start, end := g.span(bar)
		for t := start; t < end && t < f.n; t++ {
			for pc := range c {
				c[pc] += f.chroma[t][pc]
			}
		}
		best := math.Inf(-1)
		for d := range iv {
			pc := func(k int) int { return (root + iv[(d+k)%len(iv)]) % 12 }
			if s := c[pc(0)] + 0.6*c[pc(2)] + 0.8*c[pc(4)]; s > best {
				prog[bar], best = d, s
			}
		}
	}
	return prog
}

// ripHit is one onset of a band's strength
type ripHit struct {
	frame    int
	strength float64
}

// pickHits takes the local peaks of v that rise OnsetFloor over its median of the
// surrounding second and OnsetMin of its loudest, the stronger of two within gap
// frames.
func pickHits(v []float64, gap int, p RipParams, hop float64) []ripHit {
	n := len(v)
	half := int(0.5 / hop)
	m := newSlidingMedian(2*half + 1)
	med := make([]float64, n)
	var peak float64
	for t := 0; t < n+half; t++ {
		x := 0.0
		if t < n {
			x, peak = v[t], max(peak, v[t])
		}
		mv := m.push(x)
		if c := t - half; c >= 0 && c < n {
			med[c] = mv
		}
	}
	var hits []ripHit
	for t := range n {
		if v[t] <= p.OnsetFloor*med[t]+p.OnsetMin*peak {
			continue
		}
		top := true
		for d := -2; d <= 2 && top; d++ {
			if u := t + d; u >= 0 && u < n && (v[u] > v[t] || (v[u] == v[t] && u < t)) {
				top = false
			}
		}
		if !top {
			continue
		}
		if k := len(hits) - 1; k >= 0 && t-hits[k].frame < gap {
			if v[t] > hits[k].strength {
				hits[k] = ripHit{t, v[t]}
			}
			continue
		}
		hits = append(hits, ripHit{t, v[t]})
	}
	return hits
}

// drumHits reads each drum off its band's onsets, told apart by where the
// percussive magnitude rose: a kick has a body under 100 Hz, a hihat is mostly
// above 5 kHz, and a snare fills the middle with some of its noise above 5 kHz,
// which a kick's click lacks; a hit may be two drums at once.
func (f *ripFrames) drumHits(p RipParams) [InstrClap][]ripHit {
	gap := max(int(p.OnsetGap/f.hop), 1)
	share := func(t int) (sub, lo, mid, hi float64) {
		var a, b, c, d float64
		for u := t; u <= t+1 && u < f.n; u++ {
			a, b, c, d = max(a, f.rSub[u]), max(b, f.rLo[u]), max(c, f.rMid[u]), max(d, f.rHi[u])
		}
		if s := a + b + c + d; s > 0 {
			return a / s, b / s, c / s, d / s
		}
		return 0, 0, 0, 0
	}
	keep := [InstrClap]func(h ripHit) bool{
		InstrKick: func(h ripHit) bool {
			sub, _, _, _ := share(h.frame)
			return sub >= p.KickShare
		},
		InstrSnare: func(h ripHit) bool {
			_, _, mid, hi := share(h.frame)
			return mid >= p.SnareShare && hi >= p.SnareNoise && hi < p.HatShare
		},
		InstrHihat: func(h ripHit) bool {
			_, _, _, hi := share(h.frame)
			return hi >= p.HatShare
		},
	}
	bands := [InstrClap][]float64{InstrKick: f.lo, InstrSnare: f.mid, InstrHihat: f.hi}
	var out [InstrClap][]ripHit
	for d := range bands {
		for _, h := range pickHits(bands[d], gap, p, f.hop) {
			if keep[d](h) {
				out[d] = append(out[d], h)
			}
		}
	}
	return out
}

// drums quantizes the hits to the grid, a hit's velocity its strength against
// the drum's loudest, the louder of two on one step kept.
func (g *ripGrid) drums(r *Rip, hits [InstrClap][]ripHit) {
	for d := range hits {
		var loudest float64
		for _, h := range hits[d] {
			loudest = max(loudest, h.strength)
		}
		for _, h := range hits[d] {
			bar, step, ok := g.at(h.frame)
			if !ok {
				continue
			}
			vel := 0.4 + 0.6*h.strength/loudest
			evs := &r.Bars[bar].Drums[d]
			if i := slices.IndexFunc(*evs, func(s Step) bool { return s.Pos == step }); i >= 0 {
				(*evs)[i].Vel = max((*evs)[i].Vel, vel)
				continue
			}
			*evs = append(*evs, Step{Pos: step, Vel: vel})
		}
	}
}

// ripNote is a stretch of frames the pitch track held one pitch over
type ripNote struct {
	start, end, midi int
	vel              float64
}

// notes tracks the pitch between lo and hi through every frame, a hidden path
// scored by salience against the costs of moving and of starting or ending a
// note, and cuts the track into notes, a note that dips and rises again being
// two. A note shorter than MinNote is not one.
func (f *ripFrames) notes(lo, hi int, p RipParams) []ripNote {
	states := hi - lo // then the unvoiced state
	sal := func(t, s int) float64 { return float64(f.sal[t*ripPitches+lo+s-ripMinMIDI]) }
	peaks := make([]float64, f.n)
	for t := range peaks {
		for s := range states {
			peaks[t] = max(peaks[t], sal(t, s))
		}
	}
	sorted := slices.Clone(peaks)
	slices.Sort(sorted)
	ref := sorted[len(sorted)*9/10]
	if ref <= 0 {
		return nil
	}
	emit := func(t, s int) float64 {
		if s == states {
			return p.Voicing
		}
		return min(sal(t, s)/ref, 1)
	}
	prev, cur := make([]float64, states+1), make([]float64, states+1)
	back := make([][]int32, f.n)
	for s := range prev {
		prev[s] = emit(0, s)
	}
	for t := 1; t < f.n; t++ {
		back[t] = make([]int32, states+1)
		for s := 0; s <= states; s++ {
			best, arg := math.Inf(-1), s
			for q := 0; q <= states; q++ {
				var cost float64
				switch {
				case q == states && s == states:
				case q == states || s == states:
					cost = p.Switch
				default:
					cost = p.Jump * math.Abs(float64(q-s))
				}
				if v := prev[q] - cost; v > best {
					best, arg = v, q
				}
			}
			cur[s], back[t][s] = best+emit(t, s), int32(arg)
		}
		prev, cur = cur, prev
	}
	track := make([]int, f.n)
	s := 0
	for q := range prev {
		if prev[q] > prev[s] {
			s = q
		}
	}
	for t := f.n - 1; t >= 0; t-- {
		track[t] = s
		if t > 0 {
			s = int(back[t][s])
		}
	}
	var notes []ripNote
	minFrames := max(int(p.MinNote/f.hop), 1)
	flush := func(n ripNote, sum float64) {
		if n.end-n.start >= minFrames {
			n.vel = min(max(0.3, sum/float64(n.end-n.start)/ref), 1)
			notes = append(notes, n)
		}
	}
	open := ripNote{start: -1}
	var sum, peak float64
	dipped := false
	for t := 0; t < f.n; t++ {
		pitch := track[t]
		if open.start >= 0 && pitch == open.midi-lo {
			v := sal(t, pitch)
			if v < peak*p.Retrigger {
				dipped = true
			} else if dipped && v > 0.7*peak {
				flush(ripNote{start: open.start, end: t, midi: open.midi}, sum)
				open, sum, peak, dipped = ripNote{start: t, midi: open.midi}, 0, 0, false
			}
			sum, peak = sum+v, max(peak, v)
			continue
		}
		if open.start >= 0 {
			flush(ripNote{start: open.start, end: t, midi: open.midi}, sum)
			open = ripNote{start: -1}
		}
		if pitch < states {
			open, sum, peak, dipped = ripNote{start: t, midi: lo + pitch}, sal(t, pitch), sal(t, pitch), false
		}
	}
	if open.start >= 0 {
		flush(ripNote{start: open.start, end: f.n, midi: open.midi}, sum)
	}
	return notes
}

// place quantizes a note onto the grid as a step of the key's nearest degree,
// its length in sixteenths, the louder of two on one step kept.
func (f *ripFrames) place(g ripGrid, r *Rip, n ripNote, part func(*RipBar) *[]Step) {
	bar, step, ok := g.at(n.start)
	if !ok {
		return
	}
	deg, oct := degreeIn(n.midi, r.Root, scaleTable[r.Scale])
	dur := min(max(int(math.Round(float64(n.end-n.start)/g.stepFrames())), 1), MaxPatternLen)
	s := Step{Pos: step, Vel: n.vel, Deg: deg, Oct: oct, Dur: dur}
	evs := part(&r.Bars[bar])
	if i := slices.IndexFunc(*evs, func(e Step) bool { return e.Pos == step }); i >= 0 {
		if (*evs)[i].Vel < s.Vel {
			(*evs)[i] = s
		}
		return
	}
	*evs = append(*evs, s)
	slices.SortFunc(*evs, func(a, b Step) int { return a.Pos - b.Pos })
}

// ExpectedRip is the rip a perfect listener would make of patterns placed in the
// slots and played for bars bars under the harmony given: each bar's drum hits by
// instrument and its tonal notes by pitch, a note under E3 a bass note, as the
// rip reports them, with chance steps left out since they may not sound. It is
// what CompareRips grades a rip of RenderMusic's output against.
func ExpectedRip(slots [MusicSlots]PatternID, bpm, bars, root int, scale ScaleID, prog []int) *Rip {
	h := newHarmony()
	h.set(root, scale, prog)
	r := &Rip{BPM: bpm, Root: root, Scale: scale, Bars: make([]RipBar, bars)}
	for bar := range bars {
		if bar > 0 {
			h.advanceBar()
		}
		for _, id := range slots {
			p := GetPattern(id)
			if p == nil || p.Steps <= 0 {
				continue
			}
			for _, tr := range p.Tracks {
				for _, ev := range tr.Events {
					if ev.Prob > 0 && ev.Prob < 1 || ev.Vel <= 0 {
						continue
					}
					step := ev.Pos - (bar*StepsPerBar)%p.Steps
					if step < 0 || step >= StepsPerBar {
						continue
					}
					b := &r.Bars[bar]
					if tr.Instr.IsDrum() {
						if tr.Instr < InstrClap {
							b.Drums[tr.Instr] = append(b.Drums[tr.Instr], Step{Pos: step, Vel: ev.Vel})
						}
						continue
					}
					midi := h.resolve(ev.Deg, ev.Oct, tr.FollowChord)
					deg, oct := degreeIn(midi, root, scaleTable[scale])
					s := Step{Pos: step, Vel: ev.Vel, Deg: deg, Oct: oct, Dur: max(ev.Dur, 1)}
					if midi < ripBassTop {
						b.Bass = append(b.Bass, s)
					} else {
						b.Melody = append(b.Melody, s)
					}
				}
			}
		}
	}
	return r
}

// RipScore grades a rip against what was played, each part in [0, 1]: the tempo
// within two BPM, the bars found, and the drums, melody and bass as the harmonic
// mean of recall and precision of their steps, a note's pitch included. Total
// weighs the drums and the melody double.
type RipScore struct {
	Tempo, Bars, Drums, Melody, Bass, Total float64
}

// CompareRips grades got against want, the bars aligned by got's downbeat, so a
// rip that missed the first bar is graded on the rest.
func CompareRips(got, want *Rip) RipScore {
	var s RipScore
	if d := got.BPM - want.BPM; d >= -2 && d <= 2 {
		s.Tempo = 1
	}
	offset := 0
	if got.BPM > 0 {
		offset = int(math.Round(got.Downbeat / (float64(BeatsPerBar*60) / float64(got.BPM))))
	}
	n := min(len(got.Bars), len(want.Bars)-offset)
	if len(want.Bars) > 0 {
		s.Bars = float64(max(n, 0)) / float64(len(want.Bars))
	}
	key := func(r *Rip, st Step, pitched bool) int {
		if !pitched {
			return st.Pos
		}
		return st.Pos<<8 | r.Root + 12*st.Oct + scaleTable[r.Scale][st.Deg]
	}
	f := func(pick func(*RipBar) []Step, pitched bool) float64 {
		var tp, fp, fn int
		for i := 0; i < n; i++ {
			have := map[int]bool{}
			for _, st := range pick(&got.Bars[i]) {
				have[key(got, st, pitched)] = true
			}
			wantKeys := map[int]bool{}
			for _, st := range pick(&want.Bars[i+offset]) {
				wantKeys[key(want, st, pitched)] = true
			}
			for k := range wantKeys {
				if have[k] {
					tp++
				} else {
					fn++
				}
			}
			for k := range have {
				if !wantKeys[k] {
					fp++
				}
			}
		}
		if tp+fp+fn == 0 {
			return 1 // nothing played, nothing heard
		}
		return 2 * float64(tp) / float64(2*tp+fp+fn)
	}
	var drums float64
	for d := range InstrClap {
		drums += f(func(b *RipBar) []Step { return b.Drums[d] }, false)
	}
	s.Drums = drums / float64(InstrClap)
	s.Melody = f(func(b *RipBar) []Step { return b.Melody }, true)
	s.Bass = f(func(b *RipBar) []Step { return b.Bass }, true)
	s.Total = (s.Tempo + s.Bars + 2*s.Drums + 2*s.Melody + s.Bass) / 7
	return s
}

// degreeIn is the degree and octave of the scale nearest midi over root, the
// lower on a tie, as harmony.resolve reads them back.
func degreeIn(midi, root int, scale []int) (deg, oct int) {
	semis := midi - root
	oct = semis / 12
	if semis < 0 && semis%12 != 0 {
		oct--
	}
	within := semis - 12*oct
	best, bestD := 0, 12
	for i, iv := range scale {
		if d := max(within-iv, iv-within); d < bestD {
			best, bestD = i, d
		}
	}
	if 12-within < bestD {
		return 0, oct + 1
	}
	return best, oct
}
