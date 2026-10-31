//go:build !vif_headless && !vif_noaudio && !wasm

package soundlab

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"runtime"
	"strconv"

	"github.com/lixenwraith/vif/pkg/audio"
	"github.com/lixenwraith/vif/pkg/genetic"
)

// tuneOptions sizes a search: the loops rendered and their bars, the generations
// run over a pool of candidates, and the seed every draw follows
type tuneOptions struct {
	loops, bars, generations, pool int
	seed                           uint64
}

// tuneLoop is one rendered loop and the rip a perfect listener makes of it
type tuneLoop struct {
	audio []float64
	want  *audio.Rip
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
		buf, err := s.eng.RenderMusic(slots, bpm, o.bars, root, scale, nil, int64(i+1))
		if err != nil {
			return nil, err
		}
		loops = append(loops, tuneLoop{audio: buf, want: audio.ExpectedRip(slots, bpm, o.bars, root, scale, nil)})
	}
	return loops, nil
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
