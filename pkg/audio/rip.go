package audio

import (
	"cmp"
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
	SecondVoice   float64 `toml:"second_voice"`   // the strength, against the melody's, a second voice in its register is kept from; 0 hears one
}

func DefaultRipParams() RipParams {
	return RipParams{
		TempoPrior: 120, TempoWidth: 1, Tightness: 100,
		OnsetFloor: 2.0, OnsetMin: 0.1, OnsetGap: 0.06, KickShare: 0.06, SnareShare: 0.45, SnareNoise: 0.1, HatShare: 0.3,
		Harmonics: 6, HarmonicDecay: 0.8, Voicing: 0.3, Jump: 0.06, Switch: 0.35, Retrigger: 0.4, MinNote: 0.06,
		SecondVoice: 0.5,
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
	{"second_voice", 0, 0.9, func(p *RipParams) *float64 { return &p.SecondVoice }},
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
	// MelodyVoice and BassVoice are the voices a front end with a voice head heard
	// each register played with, nil otherwise
	MelodyVoice, BassVoice *VoiceDef
	// Notes is the transcript before the grid: every note heard, in time
	Notes []RipNote
}

// RipNote is one note as heard, in seconds of the recording, before it was
// quantized: the sample a voice is fitted to.
type RipNote struct {
	Start, End float64
	MIDI       int
	Vel        float64
	Bass       bool // under E3, the bass register
}

// RipBar is one bar: each drum's hits, kick, snare and hihat in instrument order,
// and the melody and bass notes, whose degrees are relative to the Rip's key.
type RipBar struct {
	Drums  [InstrClap][]Step
	Melody []Step
	Bass   []Step
}

// The analysis rate and transform, 93 ms frames of 10.8 Hz bins every 11.6 ms,
// the bands the drums are told apart by, the pitches the salience spans and the
// log-mel bands a front-end model hears: the frame grid every front end keeps.
const (
	RipRate      = 22050
	RipN         = 2048
	RipHop       = 256
	RipFrameSec  = float64(RipHop) / RipRate
	RipMelBands  = 80
	ripKernel    = 17    // frames and bins a median spans in the harmonic-percussive split
	ripSubHz     = 100.0 // a kick's body is under it; a snare's is not
	ripKickHz    = 200.0
	ripSnareHz   = 4000.0
	ripHatHz     = 5000.0
	RipMinMIDI   = 28 // E1
	RipBassTop   = 52 // the bass line lives under E3
	RipMelodyLow = 53 // the melody from F3 up
	RipMaxMIDI   = 97 // C7 exclusive
	RipPitches   = RipMaxMIDI - RipMinMIDI
	ripMaxSec    = 20 * 60
)

// RipFrames is a recording as the rip's decoders read it, one value per frame
// every Hop seconds: the strength of any onset, of each drum's (kick, snare,
// hihat), each pitch's salience from E1 to C7, the bass register's by its first
// partials and the melody's with the bass cancelled, and each register's chroma.
// A front end fills it: this package's analysis, or a model (ripnet), which may
// also say per frame which wave and voice genes a note is played with.
type RipFrames struct {
	Hop                float64
	Flux               []float64
	Onset              [InstrClap][]float64
	Salience           []float32 // frames × RipPitches, MIDI RipMinMIDI up
	Chroma, BassChroma [][12]float64
	Wave               []float32 // frames × len(VoiceWaves), or nil
	Voice              []float32 // frames × VoiceGeneCount, or nil
}

func (f *RipFrames) frames() int { return len(f.Flux) }

// RipFrontEnd hears a recording into frames
type RipFrontEnd interface {
	Hear(x []float64, rate int, p RipParams) (*RipFrames, error)
}

// Ripper rips with a front end of its choosing; a nil FrontEnd is the package's
// own analysis.
type Ripper struct {
	// Grid is the tempo and downbeat to read the recording on, nil to decode them
	Grid     *RipGrid
	Params   RipParams
	FrontEnd RipFrontEnd
}

// RipGrid pins the tempo and the downbeat a recording is read on, for one whose
// grid is known, as the engine's own renders are: the tempo, beat and bar-line
// decoders are skipped and the hits and notes fall on the given grid, so a rip of
// a render grades the render, not those decoders.
type RipGrid struct {
	BPM      int
	Downbeat float64 // seconds
}

// pinned is the grid a RipGrid describes: beats a tempo apart through the
// recording, from before the downbeat to its end, the last beat allowed half a
// beat past the end so a render of whole bars keeps its last
func (f *RipFrames) pinned(gr *RipGrid) ripGrid {
	beat := 60 / float64(max(gr.BPM, 1)) / f.Hop
	start := max(gr.Downbeat, 0) / f.Hop
	before := int(start / beat)
	var beats []int
	for i := -before; ; i++ {
		fr := int(math.Round(start + float64(i)*beat))
		if float64(fr) > float64(f.frames())+beat/2 {
			break
		}
		beats = append(beats, fr)
	}
	g := ripGrid{beats: beats, first: before}
	g.bars = max((len(beats)-1-g.first)/BeatsPerBar, 0)
	return g
}

// RipWAV reads a WAV stream and rips it with the package's analysis.
func RipWAV(r io.Reader, p RipParams) (*Rip, error) {
	return Ripper{Params: p}.WAV(r)
}

// RipSamples rips a mono recording at rate with the package's analysis.
func RipSamples(x []float64, rate int, p RipParams) (*Rip, error) {
	return Ripper{Params: p}.Samples(x, rate)
}

// WAV reads a WAV stream and rips it.
func (rp Ripper) WAV(r io.Reader) (*Rip, error) {
	x, rate, err := ReadWAV(r)
	if err != nil {
		return nil, err
	}
	return rp.Samples(x, rate)
}

// Samples rips a mono recording at rate: tempo and bars first, then the key, the
// drums on a sixteenth grid between the beats, and the melody and bass as pitch
// tracks cut into notes on that grid. Notes off the key move to its nearest
// degree, which is all a pattern can say. A front end that names voices leaves
// the registers' voices on the rip.
func (rp Ripper) Samples(x []float64, rate int) (*Rip, error) {
	if rate <= 0 || len(x) < 4*rate {
		return nil, errors.New("rip: the recording is shorter than four seconds")
	}
	if len(x) > ripMaxSec*rate {
		x = x[:ripMaxSec*rate]
	}
	p := rp.Params
	fe := rp.FrontEnd
	if fe == nil {
		fe = analysis{}
	}
	f, err := fe.Hear(x, rate, p)
	if err != nil {
		return nil, err
	}
	if f.frames() < 2 {
		return nil, errors.New("rip: the front end heard nothing")
	}
	hits := f.drumHits(p)
	root, scale := f.key()
	var bpm float64
	var g ripGrid
	if rp.Grid != nil {
		bpm = float64(rp.Grid.BPM)
		g = f.pinned(rp.Grid)
	} else {
		var period float64
		bpm, period = f.tempo(p)
		beats := f.beats(period, p)
		if len(beats) < BeatsPerBar+1 {
			return nil, errors.New("rip: no full bar found")
		}
		g = f.grid(beats, hits, root%12)
	}
	if g.bars < 1 {
		return nil, errors.New("rip: no full bar found")
	}
	r := &Rip{BPM: int(math.Round(bpm)), Root: root, Scale: scale, Downbeat: float64(g.beats[g.first]) * f.Hop, Bars: make([]RipBar, g.bars), Params: p}
	r.Progression = f.progression(g, r.Root, r.Scale)
	g.drums(r, hits)
	melody := f.notes(RipMelodyLow, RipMaxMIDI, p)
	bass := f.notes(RipMinMIDI, RipBassTop, p)
	for _, n := range melody {
		f.place(g, r, n, func(b *RipBar) *[]Step { return &b.Melody })
	}
	for _, n := range bass {
		f.place(g, r, n, func(b *RipBar) *[]Step { return &b.Bass })
	}
	r.MelodyVoice, r.BassVoice = voiceOf(melody), voiceOf(bass)
	for i, ns := range [2][]ripNote{melody, bass} {
		for _, n := range ns {
			r.Notes = append(r.Notes, RipNote{Start: float64(n.start) * f.Hop, End: float64(n.end) * f.Hop, MIDI: n.midi, Vel: n.vel, Bass: i == 1})
		}
	}
	slices.SortFunc(r.Notes, func(a, b RipNote) int { return cmp.Compare(a.Start, b.Start) })
	return r, nil
}

// MelDistance is how far two sets of mel frames are apart, the mean squared
// band difference over the longer's frames, the shorter silent past its end, so
// a note that rings on or dies early pays for it.
func MelDistance(a, b [][]float32) float64 {
	n := max(len(a), len(b))
	if n == 0 {
		return 0
	}
	var sum float64
	var count int
	for t := range n {
		var fa, fb []float32
		if t < len(a) {
			fa = a[t]
		}
		if t < len(b) {
			fb = b[t]
		}
		bands := max(len(fa), len(fb))
		for k := range bands {
			var x, y float64
			if k < len(fa) {
				x = float64(fa[k])
			}
			if k < len(fb) {
				y = float64(fb[k])
			}
			sum += (x - y) * (x - y)
			count++
		}
	}
	return sum / float64(max(count, 1))
}

// BarDistance is how far two bars are apart, the steps one has and the other
// lacks over the steps either has, a hit by its drum and step and a note by its
// step, degree and octave: zero for the same bar, one for bars sharing nothing.
func BarDistance(a, b *RipBar) float64 {
	keys := func(bar *RipBar) map[[4]int]bool {
		m := map[[4]int]bool{}
		for d := range bar.Drums {
			for _, s := range bar.Drums[d] {
				m[[4]int{d, s.Pos, 0, 0}] = true
			}
		}
		for _, s := range bar.Melody {
			m[[4]int{10, s.Pos, s.Deg, s.Oct}] = true
		}
		for _, s := range bar.Bass {
			m[[4]int{11, s.Pos, s.Deg, s.Oct}] = true
		}
		return m
	}
	ka, kb := keys(a), keys(b)
	union, differ := len(ka), 0
	for k := range kb {
		if ka[k] {
			continue
		}
		union++
		differ++
	}
	for k := range ka {
		if !kb[k] {
			differ++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(differ) / float64(union)
}

// voiceOf is the voice a register's notes were heard played with, each note's
// genes and wave weighed by its length; nil when the front end named none.
func voiceOf(notes []ripNote) *VoiceDef {
	var genes []float64
	var waves [len(VoiceWaves)]float64
	var total float64
	for _, n := range notes {
		if n.voice == nil {
			continue
		}
		if genes == nil {
			genes = make([]float64, len(n.voice))
		}
		w := float64(n.end - n.start)
		for i, g := range n.voice {
			genes[i] += g * w
		}
		waves[n.wave] += w
		total += w
	}
	if genes == nil || total == 0 {
		return nil
	}
	for i := range genes {
		genes[i] /= total
	}
	wave := 0
	for i, w := range waves {
		if w > waves[wave] {
			wave = i
		}
	}
	return VoiceFromGenes(VoiceWaves[wave], genes)
}

// Voices is the voices the rip heard the registers played with, named after
// name as Patterns names their tracks; none for a front end without a voice head.
func (r *Rip) Voices(name string) []*VoiceDef {
	var out []*VoiceDef
	if r.MelodyVoice != nil {
		v := r.MelodyVoice.Clone()
		v.Name = name + "_melody_voice"
		out = append(out, v)
	}
	if r.BassVoice != nil {
		v := r.BassVoice.Clone()
		v.Name = name + "_bass_voice"
		out = append(out, v)
	}
	return out
}

// Patterns writes the rip as patterns of up to bars bars each, named
// <name>_beat_<n>, <name>_melody_<n> and <name>_bass_<n>, in a group of their
// own and no tier, so they play where they are placed; a window with nothing in
// a part has no pattern for it. A register's track names the voice Voices heard
// for it, or the lead and the bass when none was.
func (r *Rip) Patterns(name string, bars int) []*PatternDef {
	bars = min(max(bars, 1), MaxPatternLen/StepsPerBar)
	var out []*PatternDef
	for w := 0; w*bars < len(r.Bars); w++ {
		lo, hi := w*bars, min((w+1)*bars, len(r.Bars))
		for part := range RipParts {
			if p := r.Pattern(fmt.Sprintf("%s_%s_%d", name, RipPartNames[part], w+1), name, part, lo, hi); p != nil {
				out = append(out, p)
			}
		}
	}
	return out
}

// analysis is the package's own front end: one pass over the spectrogram, each
// frame split into a harmonic and a percussive part by the medians across time and
// across frequency around it, so a frame is finished ripKernel/2 frames after it
// arrives; the drums by where the percussive magnitude rose, the pitches by a
// harmonic salience.
type analysis struct{}

func (analysis) Hear(x []float64, rate int, p RipParams) (*RipFrames, error) {
	x = resample(x, rate, RipRate)
	const bins, half = RipN/2 + 1, ripKernel / 2
	binHz := float64(RipRate) / RipN
	f := &RipFrames{Hop: RipFrameSec}
	var lo, mid, hi, rSub, rLo, rMid, rHi []float64 // per band: log flux and linear rise
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
	pitchHz := make([]float64, RipPitches)
	for i := range pitchHz {
		pitchHz[i] = NoteFreq(RipMinMIDI + i)
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
		var fl, fm, fh, rs, rl, rm, rh float64
		for k := range row {
			h2, p2 := hrow[k]*hrow[k], prow[k]*prow[k]
			mask := h2 / (h2 + p2 + 1e-12)
			hm[k], pm[k] = row[k]*mask, row[k]*(1-mask)
			lp[k] = math.Log1p(100 * pm[k])
			d := max(lp[k]-prevLP[k], 0)
			r := max(pm[k]-prevPM[k], 0)
			switch {
			case k < subBin:
				fl, rs = fl+d, rs+r
			case k < loBin:
				fl, rl = fl+d, rl+r
			case k < midBin:
				fm, rm = fm+d, rm+r
			case k >= hatBin:
				fh, rh = fh+d, rh+r
			}
		}
		prevLP, lp = lp, prevLP
		prevPM, pm = pm, prevPM
		lo, mid, hi = append(lo, fl), append(mid, fm), append(hi, fh)
		rSub, rLo, rMid, rHi = append(rSub, rs), append(rLo, rl), append(rMid, rm), append(rHi, rh)
		// the chroma reads every pitch's full series: a ghost an octave up shares
		// the pitch class of what casts it
		var ch, bch [12]float64
		for i, f0 := range pitchHz {
			ch[(RipMinMIDI+i)%12] += salience(hm, f0, len(weights))
		}
		// the bass is the strongest pitch of its register by its first partials;
		// those partials come out of what the melody is read from
		bass, bassAt := -1, 0.0
		for i := 0; i < RipBassTop-RipMinMIDI; i++ {
			s := salience(hm, pitchHz[i], 3)
			f.Salience = append(f.Salience, float32(s))
			bch[(RipMinMIDI+i)%12] += s
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
		for i := RipBassTop - RipMinMIDI; i < RipPitches; i++ {
			f.Salience = append(f.Salience, float32(salience(cancelled, pitchHz[i], len(weights))))
		}
		f.Chroma, f.BassChroma = append(f.Chroma, ch), append(f.BassChroma, bch)
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
	spectrogram(x, RipN, RipHop, func(t int, mag []float64) {
		frames = t + 1
		push(t, mag)
	})
	zero := make([]float64, bins)
	for t := frames; t < frames+half; t++ { // the last frames' windows run past the end
		push(t, zero)
	}

	// Any onset: each band's flux against its own peak, the kick's band counting
	// double and the hihat's half, so a broadband hat does not outvote the drum
	// that keeps the beat. Each drum's: its band's flux where the rise sits as the
	// drum's does, a kick's under 100 Hz, a hihat's mostly above 5 kHz, a snare's in
	// the middle with some of its noise above 5 kHz, which a kick's click lacks.
	n := len(lo)
	peak := func(v []float64) float64 {
		m := 1e-9
		for _, x := range v {
			m = max(m, x)
		}
		return m
	}
	pl, pm2, ph := peak(lo), peak(mid), peak(hi)
	f.Flux = make([]float64, n)
	for d := range f.Onset {
		f.Onset[d] = make([]float64, n)
	}
	for t := range n {
		f.Flux[t] = 2*lo[t]/pl + mid[t]/pm2 + 0.5*hi[t]/ph
		var a, b, c, e float64
		for u := t; u <= t+1 && u < n; u++ {
			a, b, c, e = max(a, rSub[u]), max(b, rLo[u]), max(c, rMid[u]), max(e, rHi[u])
		}
		sum := a + b + c + e
		if sum == 0 {
			continue
		}
		sub, midShare, hiShare := a/sum, c/sum, e/sum
		if sub >= p.KickShare {
			f.Onset[InstrKick][t] = lo[t]
		}
		if midShare >= p.SnareShare && hiShare >= p.SnareNoise && hiShare < p.HatShare {
			f.Onset[InstrSnare][t] = mid[t]
		}
		if hiShare >= p.HatShare {
			f.Onset[InstrHihat][t] = hi[t]
		}
	}
	return f, nil
}

func (m *slidingMedian) reset() {
	m.n, m.pos = 0, 0
	m.sorted = m.sorted[:0]
}

// MelFrames is a recording as the log-mel frames a front-end model hears: the
// rip's frame grid, RipMelBands triangular bands from 30 Hz to the top of the
// band on the mel scale, the energy under each as log1p of a hundred times it,
// the recording's peak at one so a level change is not a different sound.
func MelFrames(x []float64, rate int) [][]float32 {
	x = resample(x, rate, RipRate)
	var peak float64
	for _, v := range x {
		peak = max(peak, math.Abs(v))
	}
	if peak > 0 {
		y := make([]float64, len(x))
		for i, v := range x {
			y[i] = v / peak
		}
		x = y
	}
	const bins = RipN/2 + 1
	mel := func(hz float64) float64 { return 2595 * math.Log10(1+hz/700) }
	hz := func(m float64) float64 { return 700 * (math.Pow(10, m/2595) - 1) }
	lo, hi := mel(30), mel(RipRate/2)
	edges := make([]float64, RipMelBands+2)
	for i := range edges {
		edges[i] = hz(lo+(hi-lo)*float64(i)/float64(RipMelBands+1)) * RipN / RipRate // in bins
	}
	var out [][]float32
	scale := 1 / float64(RipN/4) // a unit sine's peak magnitude under the window
	spectrogram(x, RipN, RipHop, func(_ int, mag []float64) {
		frame := make([]float32, RipMelBands)
		for b := range RipMelBands {
			l, c, r := edges[b], edges[b+1], edges[b+2]
			var e float64
			for k := max(int(l), 0); k <= int(r) && k < bins; k++ {
				var w float64
				switch fk := float64(k); {
				case fk < l || fk > r:
				case fk <= c:
					w = (fk - l) / max(c-l, 1e-9)
				default:
					w = (r - fk) / max(r-c, 1e-9)
				}
				m := mag[k] * scale
				e += w * m * m
			}
			frame[b] = float32(math.Log1p(100 * e))
		}
		out = append(out, frame)
	})
	return out
}

// tempo is the lag the onset strength repeats at, under a log-Gaussian prior on
// the tempo and a comb that credits a lag with its double, folded into the
// sequencer's range; the period is in frames.
func (f *RipFrames) tempo(p RipParams) (bpm, period float64) {
	o := f.onsets()
	minLag, maxLag := int(60/240/f.Hop), int(60/60/f.Hop)
	acf := func(lag int) float64 {
		var s float64
		for t := lag; t < len(o); t++ {
			s += o[t] * o[t-lag]
		}
		return s / float64(max(len(o)-lag, 1))
	}
	score := func(lag int) float64 {
		b := 60 / (float64(lag) * f.Hop)
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
	bpm = 60 / (lag * f.Hop)
	for bpm < MinBPM {
		bpm *= 2
	}
	for bpm > MaxBPM {
		bpm /= 2
	}
	return bpm, 60 / (bpm * f.Hop)
}

// onsets is what the tempo and the beats are read from: the onset strength above
// its local mean, clipped at zero and scaled to unit deviation.
func (f *RipFrames) onsets() []float64 {
	n := f.frames()
	o := make([]float64, n)
	w := int(1 / f.Hop)
	sum := make([]float64, n+1)
	for t, v := range f.Flux {
		sum[t+1] = sum[t] + v
	}
	var sq float64
	for t := range o {
		a, b := max(t-w, 0), min(t+w+1, n)
		o[t] = max(f.Flux[t]-(sum[b]-sum[a])/float64(b-a), 0)
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
func (f *RipFrames) beats(period float64, p RipParams) []int {
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
func (f *RipFrames) grid(beats []int, hits [InstrClap][]ripHit, rootPC int) ripGrid {
	bassRoot := func(a, b int) float64 {
		var c [12]float64
		for t := a; t < b && t < f.frames(); t++ {
			for pc := range c {
				c[pc] += f.BassChroma[t][pc]
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
		for t := a; t < b && t < f.frames(); t++ {
			for pc := range c {
				c[pc] += f.Chroma[t][pc]
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
func (f *RipFrames) key() (int, ScaleID) {
	var c, b [12]float64
	for t := range f.frames() {
		for pc := range c {
			c[pc] += f.Chroma[t][pc]
			b[pc] += f.BassChroma[t][pc]
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
func (f *RipFrames) progression(g ripGrid, root int, scale ScaleID) []int {
	iv := scaleTable[scale]
	prog := make([]int, g.bars)
	for bar := range prog {
		var c [12]float64
		start, end := g.span(bar)
		for t := start; t < end && t < f.frames(); t++ {
			for pc := range c {
				c[pc] += f.Chroma[t][pc]
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

// drumHits picks each drum's onsets off its strength.
func (f *RipFrames) drumHits(p RipParams) [InstrClap][]ripHit {
	gap := max(int(p.OnsetGap/f.Hop), 1)
	var out [InstrClap][]ripHit
	for d := range f.Onset {
		out[d] = pickHits(f.Onset[d], gap, p, f.Hop)
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

// ripNote is a stretch of frames the pitch track held one pitch over, with the
// wave and voice genes the front end heard it played with when it hears those
type ripNote struct {
	start, end, midi int
	vel              float64
	strength         float64 // mean salience over the note
	second           bool    // heard under another note of the register
	wave             int
	voice            []float64
}

// notes tracks the register's loudest line, and in the melody register a second
// line with the first's notes and their partials cancelled, kept where its notes
// reach SecondVoice of the first's strength: two voices in one register, a
// violin under a pluck, where one path heard only the louder.
func (f *RipFrames) notes(lo, hi int, p RipParams) []ripNote {
	first := f.path(lo, hi, p, nil)
	if p.SecondVoice <= 0 || lo < RipMelodyLow || len(first) == 0 {
		return first
	}
	mask := make([]bool, f.frames()*RipPitches)
	var strength float64
	for _, n := range first {
		strength += n.strength
		for t := n.start; t < n.end; t++ {
			for _, h := range [...]int{0, 12, 19, 24, 28, 31} {
				for d := -1; d <= 1; d++ {
					if m := n.midi + h + d; m >= RipMinMIDI && m < RipMaxMIDI {
						mask[t*RipPitches+m-RipMinMIDI] = true
					}
				}
			}
		}
	}
	strength /= float64(len(first))
	for _, n := range f.path(lo, hi, p, mask) {
		if n.strength >= p.SecondVoice*strength {
			n.second = true
			first = append(first, n)
		}
	}
	return first
}

// notes tracks the pitch between lo and hi through every frame, a hidden path
// scored by salience against the costs of moving and of starting or ending a
// note, and cuts the track into notes, a note that dips and rises again being
// two. A note shorter than MinNote is not one.
// path is one line through a register: a hidden path through the salience,
// masked where mask says, scored against the costs of moving and of starting
// or ending a note, cut into notes; the reference loudness is the register's
// own, masked or not.
func (f *RipFrames) path(lo, hi int, p RipParams, mask []bool) []ripNote {
	states := hi - lo // then the unvoiced state
	raw := func(t, s int) float64 { return float64(f.Salience[t*RipPitches+lo+s-RipMinMIDI]) }
	sal := raw
	if mask != nil {
		sal = func(t, s int) float64 {
			if mask[t*RipPitches+lo+s-RipMinMIDI] {
				return 0
			}
			return raw(t, s)
		}
	}
	peaks := make([]float64, f.frames())
	for t := range peaks {
		for s := range states {
			peaks[t] = max(peaks[t], raw(t, s))
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
	back := make([][]int32, f.frames())
	for s := range prev {
		prev[s] = emit(0, s)
	}
	for t := 1; t < f.frames(); t++ {
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
	track := make([]int, f.frames())
	s := 0
	for q := range prev {
		if prev[q] > prev[s] {
			s = q
		}
	}
	for t := f.frames() - 1; t >= 0; t-- {
		track[t] = s
		if t > 0 {
			s = int(back[t][s])
		}
	}
	var notes []ripNote
	minFrames := max(int(p.MinNote/f.Hop), 1)
	flush := func(n ripNote, sum float64) {
		if n.end-n.start >= minFrames {
			n.strength = sum / float64(n.end-n.start)
			n.vel = min(max(0.3, n.strength/ref), 1)
			n.wave, n.voice = f.voiceOver(n.start, n.end)
			notes = append(notes, n)
		}
	}
	open := ripNote{start: -1}
	var sum, peak float64
	dipped := false
	for t := 0; t < f.frames(); t++ {
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
		flush(ripNote{start: open.start, end: f.frames(), midi: open.midi}, sum)
	}
	return notes
}

// voiceOver is the wave and voice genes a front end heard over frames a to b,
// the wave by its summed scores, the genes by their mean; nil without a voice head.
func (f *RipFrames) voiceOver(a, b int) (int, []float64) {
	if f.Voice == nil || f.Wave == nil {
		return 0, nil
	}
	genes := make([]float64, VoiceGeneCount)
	var waves [len(VoiceWaves)]float64
	for t := a; t < b; t++ {
		for i := range genes {
			genes[i] += float64(f.Voice[t*VoiceGeneCount+i])
		}
		for i := range waves {
			waves[i] += float64(f.Wave[t*len(VoiceWaves)+i])
		}
	}
	for i := range genes {
		genes[i] /= float64(max(b-a, 1))
	}
	wave := 0
	for i := range waves {
		if waves[i] > waves[wave] {
			wave = i
		}
	}
	return wave, genes
}

// place quantizes a note onto the grid as a step of the key's nearest degree,
// its length in sixteenths, the louder of two on one step kept.
func (f *RipFrames) place(g ripGrid, r *Rip, n ripNote, part func(*RipBar) *[]Step) {
	bar, step, ok := g.at(n.start)
	if !ok {
		return
	}
	deg, oct := degreeIn(n.midi, r.Root, scaleTable[r.Scale])
	dur := min(max(int(math.Round(float64(n.end-n.start)/g.stepFrames())), 1), MaxPatternLen)
	s := Step{Pos: step, Vel: n.vel, Deg: deg, Oct: oct, Dur: dur}
	// a second voice's note joins another pitch on its step; otherwise the
	// louder of two on one step is kept
	evs := part(&r.Bars[bar])
	if i := slices.IndexFunc(*evs, func(e Step) bool {
		return e.Pos == step && (!n.second || e.Deg == deg && e.Oct == oct)
	}); i >= 0 {
		if (*evs)[i].Vel < s.Vel {
			(*evs)[i] = s
		}
		return
	}
	*evs = append(*evs, s)
	slices.SortFunc(*evs, func(a, b Step) int { return a.Pos - b.Pos })
}

// RipEvent is one step of patterns placed in the slots as they would sound: a
// drum's hit, or a note's pitch, length and instrument.
type RipEvent struct {
	Bar, Step, Dur int
	Instr          InstrumentType
	MIDI           int // 0 for a drum
	Vel            float64
}

// ExpectedEvents is what patterns placed in the slots and played for bars bars
// under the harmony given would sound, step by step, chance steps left out
// since they may not. It is the truth a rendered loop is labelled with.
func ExpectedEvents(slots [MusicSlots]PatternID, bars, root int, scale ScaleID, prog []int) []RipEvent {
	h := newHarmony()
	h.set(root, scale, prog)
	var out []RipEvent
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
					e := RipEvent{Bar: bar, Step: step, Dur: max(ev.Dur, 1), Instr: tr.Instr, Vel: ev.Vel}
					if !tr.Instr.IsDrum() {
						e.MIDI = h.resolve(ev.Deg, ev.Oct, tr.FollowChord)
					}
					out = append(out, e)
				}
			}
		}
	}
	return out
}

// ExpectedRip is the rip a perfect listener would make of patterns placed in the
// slots and played for bars bars under the harmony given: each bar's drum hits by
// instrument and its tonal notes by pitch, a note under E3 a bass note, as the
// rip reports them. It is what CompareRips grades a rip of RenderMusic's output
// against.
func ExpectedRip(slots [MusicSlots]PatternID, bpm, bars, root int, scale ScaleID, prog []int) *Rip {
	r := &Rip{BPM: bpm, Root: root, Scale: scale, Bars: make([]RipBar, bars)}
	for _, e := range ExpectedEvents(slots, bars, root, scale, prog) {
		b := &r.Bars[e.Bar]
		if e.Instr.IsDrum() {
			if e.Instr < InstrClap {
				b.Drums[e.Instr] = append(b.Drums[e.Instr], Step{Pos: e.Step, Vel: e.Vel})
			}
			continue
		}
		deg, oct := degreeIn(e.MIDI, root, scaleTable[scale])
		s := Step{Pos: e.Step, Vel: e.Vel, Deg: deg, Oct: oct, Dur: e.Dur}
		if e.MIDI < RipBassTop {
			b.Bass = append(b.Bass, s)
		} else {
			b.Melody = append(b.Melody, s)
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
