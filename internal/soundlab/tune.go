//go:build !vif_headless && !vif_noaudio && !wasm

package soundlab

import (
	"bufio"
	"cmp"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"

	"github.com/lixenwraith/vif/pkg/audio"
	"github.com/lixenwraith/vif/pkg/genetic"
)

// tuneOptions sizes a search: the loops rendered and their bars, the generations
// run over a pool of candidates, the seed every draw follows, and whether the
// loops are augmented, a room, a mix and random voices behind each
type tuneOptions struct {
	loops, bars, generations, pool int
	seed                           uint64
	aug                            bool
}

// tuneLoop is one rendered loop, what made it, the rip a perfect listener makes
// of it, and the voices its tonal tracks were played with
type tuneLoop struct {
	audio  []float64
	want   *audio.Rip
	slots  [audio.MusicSlots]audio.PatternID
	bpm    int
	root   int
	scale  audio.ScaleID
	voices map[string]*audio.VoiceDef
}

// tempos spread the corpus over the sequencer's range
var tuneTempos = []int{96, 112, 128, 140, 156, 176}

func cmdTune(s *Session, a []string) error {
	o := tuneOptions{loops: 12, bars: 4, generations: 8, pool: 16, seed: 1}
	if len(a) > 0 {
		n, err := strconv.Atoi(a[0])
		if err != nil || n < 1 || n > 200 {
			return fmt.Errorf("generations %q: want 1 to 200", a[0])
		}
		o.generations = n
	}
	file := ""
	if len(a) > 1 {
		file = a[1]
	}
	return s.tune(o, file)
}

// corpus renders loops of the registered rhythm, melody and bass patterns, drawn
// by the seed over the tempos and the keys, each with its expected rip
func (s *Session) corpus(o tuneOptions) ([]tuneLoop, error) {
	var byRole [audio.MusicSlots][]audio.PatternID
	for _, p := range audio.RegisteredPatterns() {
		switch p.Role {
		case audio.RoleRhythm, audio.RoleMelody, audio.RoleBass:
			byRole[p.Role-1] = append(byRole[p.Role-1], p.ID)
		}
	}
	if len(byRole[0]) == 0 || len(byRole[1]) == 0 || len(byRole[3]) == 0 {
		return nil, errors.New("tune needs registered rhythm, melody and bass patterns (builtin pattern, apply)")
	}
	rng := rand.New(rand.NewPCG(o.seed, 0x5bd1e995))
	loops := make([]tuneLoop, 0, o.loops)
	for i := range o.loops {
		var slots [audio.MusicSlots]audio.PatternID
		for slot, pool := range byRole {
			if len(pool) > 0 {
				slots[slot] = pool[rng.IntN(len(pool))]
			}
		}
		bpm := tuneTempos[i%len(tuneTempos)]
		root := audio.DefaultRootNote + rng.IntN(12) - 6
		scale := audio.ScaleID(rng.IntN(int(audio.ScaleCount)))
		// the augmentation draws from a stream of its own, so a loop's patterns
		// and key are the same with and without it
		arng := rand.New(rand.NewPCG(o.seed^0xa5a5a5a5, uint64(i)+1))
		voices, restore := s.loopVoices(slots, o.aug, arng)
		buf, err := s.eng.RenderMusic(slots, bpm, o.bars, root, scale, nil, int64(i+1))
		restore()
		if err != nil {
			return nil, err
		}
		if o.aug {
			buf = degrade(buf, arng)
		}
		loops = append(loops, tuneLoop{audio: buf, want: audio.ExpectedRip(slots, bpm, o.bars, root, scale, nil), slots: slots, bpm: bpm, root: root, scale: scale, voices: voices})
	}
	return loops, nil
}

// loopVoices is the voice each tonal instrument the slotted patterns name plays
// with: the registered one, or under aug one of random genes near it, a third of
// the time on another wave, registered for the render and put back by restore,
// so a corpus hears voices beyond the shipped ones and labels them as played
func (s *Session) loopVoices(slots [audio.MusicSlots]audio.PatternID, aug bool, rng *rand.Rand) (map[string]*audio.VoiceDef, func()) {
	voices := map[string]*audio.VoiceDef{}
	var originals []*audio.VoiceDef
	for _, id := range slots {
		p := audio.GetPattern(id)
		if p == nil {
			continue
		}
		for _, tr := range p.Tracks {
			if tr.Instr.IsDrum() {
				continue
			}
			name := audio.InstrumentName(tr.Instr)
			v := audio.VoiceDefByName(name)
			if v == nil || voices[name] != nil {
				continue
			}
			if !aug {
				voices[name] = v
				continue
			}
			genes := v.Genes()
			for i := range genes {
				genes[i] = min(max(genes[i]+rng.NormFloat64()*0.15, 0), 1)
			}
			wave := cmp.Or(v.Wave, "sine")
			if rng.Float64() < 0.3 {
				wave = audio.VoiceWaves[rng.IntN(len(audio.VoiceWaves))]
			}
			r := audio.VoiceFromGenes(wave, genes)
			r.Name = name
			if audio.ValidateVoice(r) != nil {
				voices[name] = v
				continue
			}
			if _, err := s.eng.DefineVoice(r); err != nil {
				voices[name] = v
				continue
			}
			originals = append(originals, v)
			voices[name] = r
		}
	}
	return voices, func() {
		for _, v := range originals {
			s.eng.DefineVoice(v)
		}
	}
}

// degrade puts a room and a mix between a render and the frames a model hears:
// a gain, a low-pass, a short reverb and a noise floor, each drawn from rng, so
// the model does not learn the synthesizer's clean output
func degrade(x []float64, rng *rand.Rand) []float64 {
	sr := float64(audio.AudioSampleRate)
	out := make([]float64, len(x))
	gain := math.Pow(10, (rng.Float64()*9-6)/20)
	cutoff := 2500 + rng.Float64()*7500
	a := 1 - math.Exp(-2*math.Pi*cutoff/sr)
	var lp float64
	for i, v := range x {
		lp += a * (v*gain - lp)
		out[i] = lp
	}
	// a Schroeder reverb: four combs in parallel into two allpasses, at a decay
	// and a mix of the draw's
	rt := 0.15 + rng.Float64()*0.55
	wet := 0.05 + rng.Float64()*0.25
	combs := [4]int{int(0.0297 * sr), int(0.0371 * sr), int(0.0411 * sr), int(0.0437 * sr)}
	var lines [4][]float64
	var feedback [4]float64
	for k, n := range combs {
		lines[k] = make([]float64, n)
		feedback[k] = math.Pow(0.001, float64(n)/sr/rt)
	}
	aps := [2]int{int(0.005 * sr), int(0.0017 * sr)}
	var apLines [2][]float64
	for k, n := range aps {
		apLines[k] = make([]float64, n)
	}
	var rms float64
	for i := range out {
		dry := out[i]
		var sum float64
		for k := range combs {
			j := i % len(lines[k])
			y := lines[k][j]
			lines[k][j] = dry + y*feedback[k]
			sum += y
		}
		sum *= 0.25
		for k := range aps {
			j := i % len(apLines[k])
			buf := apLines[k][j]
			apLines[k][j] = sum + buf*0.5
			sum = buf - sum*0.5
		}
		out[i] = dry + wet*sum
		rms += out[i] * out[i]
	}
	rms = math.Sqrt(rms / float64(max(len(out), 1)))
	noise := rms * math.Pow(10, (rng.Float64()*15-40)/20)
	for i := range out {
		out[i] = min(max(out[i]+rng.NormFloat64()*noise, -1), 1)
	}
	return out
}

// corpusEvent is one labelled event of an exported loop, in frames of the rip's
// grid: a drum's hit, or a note with the wave and voice genes it was played with
type corpusEvent struct {
	Frame int       `json:"frame"`
	End   int       `json:"end"`
	Drum  int       `json:"drum"` // kick 0, snare 1, hihat 2; -1 for a note
	MIDI  int       `json:"midi"`
	Wave  int       `json:"wave"` // index into the waves of the manifest; -1 unknown
	Voice []float64 `json:"voice,omitempty"`
}

// corpusLoop is an exported loop's label file
type corpusLoop struct {
	BPM    int           `json:"bpm"`
	Root   int           `json:"root"`
	Scale  int           `json:"scale"`
	Bars   int           `json:"bars"`
	Frames int           `json:"frames"`
	Events []corpusEvent `json:"events"`
}

// corpusManifest names the corpus's layout for the trainer
type corpusManifest struct {
	Version    int      `json:"version"`
	Loops      int      `json:"loops"`
	Rate       int      `json:"rate"`
	Hop        int      `json:"hop"`
	MelBands   int      `json:"mel_bands"`
	MinMIDI    int      `json:"min_midi"`
	Pitches    int      `json:"pitches"`
	Drums      []string `json:"drums"`
	Waves      []string `json:"waves"`
	VoiceGenes int      `json:"voice_genes"`
}

func cmdCorpus(s *Session, a []string) error {
	o := tuneOptions{loops: 24, bars: 4, seed: 1}
	if n := len(a); n > 1 && a[n-1] == "aug" {
		o.aug, a = true, a[:n-1]
	}
	for i, p := range []*int{&o.loops, &o.bars} {
		if len(a) > i+1 {
			n, err := strconv.Atoi(a[i+1])
			if err != nil || n < 1 || n > 4096 {
				return fmt.Errorf("%q: want 1 to 4096", a[i+1])
			}
			*p = n
		}
	}
	return s.exportCorpus(a[0], o)
}

// exportCorpus writes loops into dir: loop_NNN.wav as rendered, loop_NNN.mel as
// the log-mel frames a front-end model hears (frames, bands, then float32 rows,
// little-endian), loop_NNN.json as the events that sound in it with their frames
// and voices, and corpus.json naming the layout.
func (s *Session) exportCorpus(dir string, o tuneOptions) error {
	loops, err := s.corpus(o)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for i, l := range loops {
		base := filepath.Join(dir, fmt.Sprintf("loop_%03d", i))
		f, err := os.Create(base + ".wav")
		if err != nil {
			return err
		}
		werr := audio.WriteWAV(f, l.audio)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return werr
		}
		mel := audio.MelFrames(l.audio, audio.AudioSampleRate)
		if err := writeMel(base+".mel", mel); err != nil {
			return err
		}
		lab := corpusLoop{BPM: l.bpm, Root: l.root, Scale: int(l.scale), Bars: o.bars, Frames: len(mel)}
		stepSec := float64(audio.SamplesPerStep(l.bpm)) / audio.AudioSampleRate
		for _, e := range audio.ExpectedEvents(l.slots, o.bars, l.root, l.scale, nil) {
			start := float64(e.Bar*audio.StepsPerBar+e.Step) * stepSec
			ev := corpusEvent{Frame: int(start/audio.RipFrameSec + 0.5), Drum: -1, Wave: -1}
			if e.Instr.IsDrum() {
				if e.Instr >= audio.InstrClap {
					continue
				}
				ev.Drum, ev.End = int(e.Instr), ev.Frame
			} else {
				ev.MIDI = e.MIDI
				ev.End = int((start+float64(e.Dur)*stepSec)/audio.RipFrameSec + 0.5)
				if v := l.voices[audio.InstrumentName(e.Instr)]; v != nil {
					ev.Wave = max(slices.Index(audio.VoiceWaves[:], cmp.Or(v.Wave, "sine")), 0)
					ev.Voice = v.Genes()
				}
			}
			if ev.Frame < len(mel) {
				lab.Events = append(lab.Events, ev)
			}
		}
		if err := writeJSON(base+".json", lab); err != nil {
			return err
		}
	}
	m := corpusManifest{Version: 1, Loops: len(loops), Rate: audio.RipRate, Hop: audio.RipHop, MelBands: audio.RipMelBands,
		MinMIDI: audio.RipMinMIDI, Pitches: audio.RipPitches, Drums: []string{"kick", "snare", "hihat"}, Waves: audio.VoiceWaves[:], VoiceGenes: audio.VoiceGeneCount}
	if err := writeJSON(filepath.Join(dir, "corpus.json"), m); err != nil {
		return err
	}
	how := "as rendered"
	if o.aug {
		how = "through a room, a mix and voices of random genes"
	}
	fmt.Fprintf(s.out, "wrote %d loop(s) of %d bar(s) to %s, %s\n", len(loops), o.bars, dir, how)
	return nil
}

func writeMel(file string, mel [][]float32) error {
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	le := binary.LittleEndian
	bands := 0
	if len(mel) > 0 {
		bands = len(mel[0])
	}
	binary.Write(w, le, uint32(len(mel)))
	binary.Write(w, le, uint32(bands))
	for _, row := range mel {
		binary.Write(w, le, row)
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func writeJSON(file string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(file, data, 0o644)
}

func cmdBench(s *Session, a []string) error {
	o := tuneOptions{loops: 8, bars: 4, seed: 7}
	for i, p := range []*int{&o.loops, &o.bars} {
		if len(a) > i {
			n, err := strconv.Atoi(a[i])
			if err != nil || n < 1 || n > 256 {
				return fmt.Errorf("%q: want 1 to 256", a[i])
			}
			*p = n
		}
	}
	return s.bench(o)
}

// bench grades the front ends the session has, the analysis and the model when
// one loaded, on fresh loops the engine renders, clean and augmented: the mean
// of each rip's score against the loop's expected rip, so a trained model is
// measured against the analysis before it is trusted with a recording
func (s *Session) bench(o tuneOptions) error {
	type front struct {
		name string
		fe   audio.RipFrontEnd
	}
	fronts := []front{{"analysis", nil}}
	if s.frontEnd != nil {
		fronts = append(fronts, front{"model", s.frontEnd})
	}
	for _, aug := range []bool{false, true} {
		o.aug = aug
		loops, err := s.corpus(o)
		if err != nil {
			return err
		}
		kind := "clean"
		if aug {
			kind = "augmented"
		}
		for _, f := range fronts {
			var sum audio.RipScore
			n := 0
			for _, l := range loops {
				r, err := audio.Ripper{Params: s.rip, FrontEnd: f.fe}.Samples(l.audio, audio.AudioSampleRate)
				if err != nil {
					continue
				}
				sc := audio.CompareRips(r, l.want)
				sum.Tempo, sum.Bars, sum.Drums = sum.Tempo+sc.Tempo, sum.Bars+sc.Bars, sum.Drums+sc.Drums
				sum.Melody, sum.Bass, sum.Total = sum.Melody+sc.Melody, sum.Bass+sc.Bass, sum.Total+sc.Total
				n++
			}
			if n == 0 {
				fmt.Fprintf(s.out, "%s on %s loops: no rip\n", f.name, kind)
				continue
			}
			d := float64(n)
			fmt.Fprintf(s.out, "%s on %d %s loop(s): %.3f (tempo %.2f bars %.2f drums %.2f melody %.2f bass %.2f)\n",
				f.name, n, kind, sum.Total/d, sum.Tempo/d, sum.Bars/d, sum.Drums/d, sum.Melody/d, sum.Bass/d)
		}
	}
	return nil
}

// tune searches the rip's parameters: the current ones seed a pool the rest of
// which is drawn across the ranges, each candidate scored by how its rip of every
// loop compares with the loop's expected rip, and the best, when it beats the
// current, is written and listened with from then on.
func (s *Session) tune(o tuneOptions, file string) error {
	loops, err := s.corpus(o)
	if err != nil {
		return err
	}
	eval := func(genes []float64) float64 {
		p := audio.RipParamsFromGenes(genes)
		var sum float64
		for _, l := range loops {
			if r, err := audio.RipSamples(l.audio, audio.AudioSampleRate, p); err == nil {
				sum += audio.CompareRips(r, l.want).Total
			}
		}
		return sum / float64(len(loops))
	}
	ranges := audio.RipGeneRanges()
	bounds := make([]genetic.ParameterBounds, len(ranges))
	for i, r := range ranges {
		bounds[i] = genetic.ParameterBounds{Min: r[0], Max: r[1]}
	}
	start := s.rip.Genes()
	base := eval(start)
	fmt.Fprintf(s.out, "tuning over %d loop(s) of %d bar(s): the current parameters score %.3f\n", len(loops), o.bars, base)
	seeded := false
	init := func(rng *rand.Rand) []float64 {
		g := make([]float64, len(bounds))
		if !seeded {
			seeded = true
			copy(g, start)
			return g
		}
		for i, b := range bounds {
			g[i] = b.Min + rng.Float64()*(b.Max-b.Min)
		}
		return g
	}
	cfg := genetic.DefaultConfig()
	cfg.PoolSize, cfg.EliteCount, cfg.MaxIterations = o.pool, max(o.pool/8, 1), o.generations
	cfg.Parallelism, cfg.Seed = runtime.NumCPU(), o.seed
	cfg.PerturbationRate, cfg.PerturbationStrength = 0.25, 0.15
	e := genetic.NewEngine[[]float64, float64](eval, init,
		&genetic.TournamentSelector[[]float64, float64]{TournamentSize: 3},
		&genetic.UniformCombiner[[]float64, float64, float64]{MixProbability: 0.5},
		&genetic.BoundedPerturbator{Bounds: bounds, Boundary: genetic.BoundaryReflect}, cfg)
	e.SetTerminator(func(p *genetic.Pool[[]float64, float64], iter int) bool {
		fmt.Fprintf(s.out, "generation %d: best %.3f mean %.3f\n", iter, p.Stats.BestScore, p.Stats.AverageScore)
		return false
	})
	pool, err := e.Run(context.Background())
	if err != nil {
		return err
	}
	best := pool.Members[0]
	if best.Score <= base {
		fmt.Fprintf(s.out, "nothing beat the current parameters (%.3f); they stay\n", base)
		return nil
	}
	p := audio.RipParamsFromGenes(best.Data)
	if file, err = saveFile(file, s.ripSrc, s.files.ripTarget); err != nil {
		return err
	}
	data, err := audio.MarshalRipParams(p)
	if err != nil {
		return err
	}
	if err := os.WriteFile(file, data, 0o644); err != nil {
		return err
	}
	s.rip, s.ripSrc = p, file
	fmt.Fprintf(s.out, "wrote %s: score %.3f, was %.3f; rip listens with it from now on\n", file, best.Score, base)
	return nil
}
