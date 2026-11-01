//go:build !vif_headless && !vif_noaudio && !wasm

package soundlab

// The voice fit: analysis by synthesis. A recording's register is heard as notes;
// the engine plays a candidate voice at those notes and the mel frames of its
// render are compared with the recording's around them, and a search over the
// voice's genes and wave, seeded with every registered voice, keeps the nearest.

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/lixenwraith/vif/pkg/audio"
	"github.com/lixenwraith/vif/pkg/genetic"
)

// fitOptions sizes a voice fit: the notes of a register it listens to, the
// generations run over a pool of candidate voices, and the seed every draw follows
type fitOptions struct {
	notes, generations, pool int
	seed                     uint64
}

var defaultFit = fitOptions{notes: 6, generations: 8, pool: 16, seed: 1}

// fitTail is how long past a note's end its sample runs, for the release
const fitTail = 0.25

// ripped is a recording ripped this session: what was heard, and the samples it
// was heard in, so fit and compress work on it without the file
type ripped struct {
	rip   *audio.Rip
	x     []float64
	rate  int
	file  string
	sound *audio.RipSound // the recording as Fidelity hears it, heard on first use
}

func cmdFit(s *Session, a []string) error {
	rp, ok := s.rips[a[0]]
	if !ok {
		return fmt.Errorf("%q: nothing ripped under that name this session (rip first)", a[0])
	}
	o := s.fit
	if len(a) > 1 {
		n, err := strconv.Atoi(a[1])
		if err != nil || n < 1 || n > 500 {
			return fmt.Errorf("generations %q: want 1 to 500", a[1])
		}
		o.generations = n
	}
	s.fitVoices(rp, o, true)
	for _, v := range rp.rip.Voices(a[0]) {
		if _, err := s.eng.DefineVoice(v.Clone()); err != nil {
			return fmt.Errorf("voice %q: %w", v.Name, err)
		}
		s.voices.put(v, false)
		fmt.Fprintf(s.out, "voice %s: %s, registered; the patterns naming it play it\n", v.Name, cmp.Or(v.Wave, "sine"))
	}
	return nil
}

// cmdNotes lists a rip's transcript, the notes as heard before the grid, each
// with the bar and step it fell on, its pitch as the key's degree with any
// accidental, its length and loudness, so a listener checks the rip by eye.
func cmdNotes(s *Session, a []string) error {
	rp, ok := s.rips[a[0]]
	if !ok {
		return fmt.Errorf("%q: nothing ripped under that name this session (rip first)", a[0])
	}
	r := rp.rip
	fmt.Fprintf(s.out, "%s: %d note(s) at %d bpm in %s %s, tuned %+.0f cents, the first bar from %.2fs\n", a[0], len(r.Notes), r.BPM, noteName(r.Root), scaleName(r.Scale), r.Tuning, r.Downbeat)
	marks := [...]string{"bb", "b", "", "#", "x"}
	for _, n := range r.Notes {
		line := "melody"
		switch {
		case n.Bass:
			line = "bass"
		case n.Second:
			line = "under"
		}
		where, span := "before the first bar", 60/float64(max(r.BPM, 1))/audio.StepsPerBeat
		if i := sort.SearchFloat64s(r.Beats, n.Start+1e-9) - 1; i >= 0 && i+1 < len(r.Beats) {
			span = (r.Beats[i+1] - r.Beats[i]) / audio.StepsPerBeat
			where = fmt.Sprintf("bar %d step %2d", i/audio.BeatsPerBar+1, audio.StepsPerBeat*(i%audio.BeatsPerBar)+int(math.Round((n.Start-r.Beats[i])/span)))
		}
		deg, oct, acc := audio.PitchStep(r.Root, r.Scale, n.MIDI)
		fmt.Fprintf(s.out, "%-6s %7.3fs %-20s %-4s degree %d%s octave %d, %.1f step(s) at %.2f\n", line, n.Start, where, noteName(n.MIDI), deg, marks[acc+2], oct, (n.End-n.Start)/span, n.Vel)
	}
	return nil
}

// ripLines are a rip's three lines in the order their voices are fitted: the
// melody, the line under it and the bass, each with the notes that are its
var ripLines = [3]struct {
	word string
	of   func(audio.RipNote) bool
}{
	{"melody", func(n audio.RipNote) bool { return !n.Bass && !n.Second }},
	{"under", func(n audio.RipNote) bool { return !n.Bass && n.Second }},
	{"bass", func(n audio.RipNote) bool { return n.Bass }},
}

// lineVoices are the rip's voices in ripLines' order
func lineVoices(r *audio.Rip) [3]**audio.VoiceDef {
	return [3]**audio.VoiceDef{&r.MelodyVoice, &r.UnderVoice, &r.BassVoice}
}

// fitVoices fits a voice to each line of a rip that lacks one, or to each
// again with the voice held as a seed, and says how far the fit came
func (s *Session) fitVoices(rp *ripped, o fitOptions, again bool) {
	for reg, at := range lineVoices(rp.rip) {
		if *at != nil && !again {
			continue
		}
		var notes []audio.RipNote
		for _, n := range rp.rip.Notes {
			if ripLines[reg].of(n) {
				notes = append(notes, n)
			}
		}
		if len(notes) == 0 {
			continue
		}
		word := ripLines[reg].word
		fmt.Fprintf(s.out, "fitting the %s voice over %d note(s), %d generation(s) of %d...\n", word, min(len(notes), o.notes), o.generations, o.pool)
		v, from, to := s.fitVoice(rp.x, rp.rate, notes, *at, o)
		if v == nil {
			continue
		}
		*at = v
		fmt.Fprintf(s.out, "%s voice: %s, distance %.3f from the best seed's %.3f\n", word, cmp.Or(v.Wave, "sine"), to, from)
	}
}

// fitSamples picks the notes a register is fitted on: the longest and loudest
// first, no pitch twice until every pitch has a note, each at least 80 ms
func fitSamples(notes []audio.RipNote, n int) []audio.RipNote {
	var cands []audio.RipNote
	for _, note := range notes {
		if note.End-note.Start >= 0.08 {
			cands = append(cands, note)
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		return (cands[i].End-cands[i].Start)*cands[i].Vel > (cands[j].End-cands[j].Start)*cands[j].Vel
	})
	var out []audio.RipNote
	seen := map[int]bool{}
	for pass := range 2 {
		for _, c := range cands {
			if len(out) >= n {
				return out
			}
			if (pass == 0 && !seen[c.MIDI]) || (pass == 1 && !slices.Contains(out, c)) {
				out = append(out, c)
				seen[c.MIDI] = true
			}
		}
	}
	return out
}

// voiceGenome is a voice as a search sees it: its genes and, last, its wave as a
// fraction of the wave table; voiceFromGenome reads one back
func voiceGenome(v *audio.VoiceDef) []float64 {
	w := max(slices.Index(audio.VoiceWaves[:], cmp.Or(v.Wave, "sine")), 0)
	return append(v.Genes(), (float64(w)+0.5)/float64(len(audio.VoiceWaves)))
}

func voiceFromGenome(g []float64) *audio.VoiceDef {
	genes := audio.VoiceGeneCount
	w := min(max(int(g[genes]*float64(len(audio.VoiceWaves))), 0), len(audio.VoiceWaves)-1)
	v := audio.VoiceFromGenes(audio.VoiceWaves[w], g[:genes])
	v.Name = "fitted"
	return v
}

// genomeKey names a genome to three decimals, for a cache of its score
func genomeKey(g []float64) string {
	var b strings.Builder
	for _, v := range g {
		fmt.Fprintf(&b, "%.3f,", v)
	}
	return b.String()
}

// fitVoice searches the voices for the one whose notes, rendered by the engine,
// sound most like a register's notes in the recording: a genome of the voice's
// genes and its wave, seeded with every registered voice and the one given,
// scored by the mel distance between the recording around each sample note and
// the voice playing that note. It returns the voice, the best seed's distance
// and the fit's; nil when the register has no note long enough.
func (s *Session) fitVoice(x []float64, rate int, notes []audio.RipNote, prior *audio.VoiceDef, o fitOptions) (*audio.VoiceDef, float64, float64) {
	samples := fitSamples(notes, max(o.notes, 1))
	if len(samples) == 0 {
		return nil, 0, 0
	}
	type target struct {
		mel      [][]float32
		midi     int
		vel, dur float64
	}
	targets := make([]target, len(samples))
	for i, n := range samples {
		lo := min(int(n.Start*float64(rate)), len(x))
		hi := min(int((n.End+fitTail)*float64(rate)), len(x))
		targets[i] = target{mel: audio.MelFrames(x[lo:hi], rate), midi: n.MIDI, vel: n.Vel, dur: n.End - n.Start}
	}
	genes := audio.VoiceGeneCount
	decode, encode := voiceFromGenome, voiceGenome
	eval := genetic.Memoize(genomeKey, func(g []float64) float64 {
		v := decode(g)
		if audio.ValidateVoice(v) != nil {
			return math.Inf(-1)
		}
		var sum float64
		for _, t := range targets {
			note := audio.RenderNote(v, t.midi, t.vel, t.dur, fitTail)
			sum += audio.MelDistance(audio.MelFrames(note, audio.AudioSampleRate), t.mel)
		}
		return -sum / float64(len(targets))
	})
	var seeds [][]float64
	if prior != nil {
		seeds = append(seeds, encode(prior))
	}
	for _, v := range audio.RegisteredVoices() {
		seeds = append(seeds, encode(v))
	}
	best := math.Inf(-1)
	for _, g := range seeds {
		best = max(best, eval(g))
	}
	bounds := make([]genetic.ParameterBounds, genes+1)
	for i := range bounds {
		bounds[i] = genetic.ParameterBounds{Min: 0, Max: 1}
	}
	cfg := genetic.DefaultConfig()
	cfg.PoolSize, cfg.EliteCount, cfg.MaxIterations = max(o.pool, 4), max(o.pool/8, 1), o.generations
	cfg.Parallelism, cfg.Seed = runtime.NumCPU(), o.seed
	cfg.PerturbationRate, cfg.PerturbationStrength = 0.15, 0.1
	e := genetic.NewEngine[[]float64, float64](eval, func(rng *rand.Rand) []float64 {
		g := make([]float64, genes+1)
		for i := range g {
			g[i] = rng.Float64()
		}
		return g
	}, &genetic.TournamentSelector[[]float64, float64]{TournamentSize: 3},
		&genetic.UniformCombiner[[]float64, float64, float64]{MixProbability: 0.5},
		&genetic.BoundedPerturbator{Bounds: bounds, Boundary: genetic.BoundaryReflect}, cfg)
	e.SetSeeds(seeds)
	e.SetSchedule(genetic.AdaptiveStrength[float64](0.15, 0.1, 0.01, 0.4, 1.5))
	e.SetTerminator(func(p *genetic.Pool[[]float64, float64], gen int) bool {
		fmt.Fprintf(s.out, "generation %d: distance %.3f\n", gen, -p.Stats.BestScore)
		return false
	})
	pool, err := e.Run(context.Background())
	if err != nil || len(pool.Members) == 0 {
		return nil, 0, 0
	}
	// MelFrames hears every sample at its peak, so the level is the search's
	// blind spot: set it so the voice's notes carry the loudness the recording
	// has around them
	v := decode(pool.Members[0].Data)
	var want, have float64
	for i, t := range targets {
		n := samples[i]
		lo, hi := min(int(n.Start*float64(rate)), len(x)), min(int(n.End*float64(rate)), len(x))
		want += rms(x[lo:hi])
		have += rms(audio.RenderNote(v, t.midi, t.vel, t.dur, 0))
	}
	if have > 0 && want > 0 {
		v.Level = min(max(v.Level*want/have, 0.1), 4) // the level ValidateVoice allows
	}
	return v, -best, -pool.Members[0].Score
}

func rms(x []float64) float64 {
	var e float64
	for _, v := range x {
		e += v * v
	}
	return math.Sqrt(e / float64(max(len(x), 1)))
}
