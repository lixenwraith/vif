package genetic

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"sync"
	"sync/atomic"
)

// defaultSeed restates vmath.Golden64, since this package imports only the
// standard library
const defaultSeed = 0x9E3779B97F4A7C15

// ErrNoCandidates is returned when the pool has not been initialized
var ErrNoCandidates = errors.New("genetic: pool is empty")

// Engine runs a generational GA with synchronous evaluation.
// Use StreamingEngine when scoring is asynchronous and caller-driven.
// The evaluator must be goroutine-safe when Parallelism exceeds 1
type Engine[S Solution, F Numeric] struct {
	evaluator   EvaluatorFunc[S, F]
	initializer InitializerFunc[S]
	selector    Selector[S, F]
	combiner    Combiner[S, F]
	perturbator Perturbator[S]
	terminator  TerminationFunc[S, F]
	diversity   DiversityFunc[S, F]
	schedule    ScheduleFunc[F]
	seeds       []S

	cfg     EngineConfig
	pcg     *rand.PCG
	rng     *rand.Rand
	started bool
	// rate and strength are the mutation knobs in force, the config's until a
	// schedule moves them
	rate, strength float64

	pool    Pool[S, F]
	version uint64
	next    []Candidate[S, F]
	parents []Candidate[S, F]
	buf     []S
	free    freeList[S]
	history []PoolStats[F]
}

func NewEngine[S Solution, F Numeric](
	evaluator EvaluatorFunc[S, F],
	initializer InitializerFunc[S],
	selector Selector[S, F],
	combiner Combiner[S, F],
	perturbator Perturbator[S],
	config EngineConfig,
) *Engine[S, F] {
	cfg := config.Normalize()
	pcg := rand.NewPCG(cfg.Seed, cfg.Seed^defaultSeed)

	e := &Engine[S, F]{
		evaluator:   evaluator,
		initializer: initializer,
		selector:    selector,
		combiner:    combiner,
		perturbator: perturbator,
		cfg:         cfg,
		pcg:         pcg,
		rng:         rand.New(pcg),
		rate:        cfg.PerturbationRate,
		strength:    cfg.PerturbationStrength,
		parents:     make([]Candidate[S, F], 2),
		buf:         make([]S, 2),
		next:        make([]Candidate[S, F], 0, cfg.PoolSize),
		history:     make([]PoolStats[F], 0, cfg.MaxIterations),
	}
	e.free.init(cfg.PoolSize)
	return e
}

func (e *Engine[S, F]) SetTerminator(fn TerminationFunc[S, F]) { e.terminator = fn }
func (e *Engine[S, F]) SetDiversity(fn DiversityFunc[S, F])    { e.diversity = fn }

// ScheduleFunc chooses the next generation's mutation rate and strength from the
// pool as it stands, called before each step; AdaptiveStrength is one.
type ScheduleFunc[F Numeric] func(generation int, stats PoolStats[F]) (rate, strength float64)

func (e *Engine[S, F]) SetSchedule(fn ScheduleFunc[F]) { e.schedule = fn }

// SetSeeds places candidates in the first generation ahead of the initializer's,
// as many as the pool holds; the engine owns them from here, as it owns every
// member, so pass copies of what is kept.
func (e *Engine[S, F]) SetSeeds(seeds []S) { e.seeds = seeds }

// AdaptiveStrength is a schedule after the one-fifth rule: the strength rises
// by factor on a generation that bettered the best score and falls by it on one
// that did not, held within lo and hi, from start; the rate stays.
func AdaptiveStrength[F Numeric](rate, start, lo, hi, factor float64) ScheduleFunc[F] {
	strength := start
	var best F
	primed := false
	return func(_ int, stats PoolStats[F]) (float64, float64) {
		switch {
		case !primed:
			primed, best = true, stats.BestScore
		case stats.BestScore > best:
			best, strength = stats.BestScore, min(strength*factor, hi)
		default:
			strength = max(strength/factor, lo)
		}
		return rate, strength
	}
}

// Memoize wraps an evaluator with a cache by genome key, for a search whose
// candidates repeat and whose evaluation is dear; it holds every key it scored,
// and two workers may score one key at once.
func Memoize[S Solution, F Numeric](key func(S) string, eval EvaluatorFunc[S, F]) EvaluatorFunc[S, F] {
	var mu sync.Mutex
	seen := make(map[string]F)
	return func(s S) F {
		k := key(s)
		mu.Lock()
		v, ok := seen[k]
		mu.Unlock()
		if ok {
			return v
		}
		v = eval(s)
		mu.Lock()
		seen[k] = v
		mu.Unlock()
		return v
	}
}

// Run evolves until MaxIterations, the terminator, or context cancellation.
// The pool it returns is engine memory, rewritten by the next Run
func (e *Engine[S, F]) Run(ctx context.Context) (*Pool[S, F], error) {
	if !e.started {
		e.initialize()
		e.started = true
	}

	for e.pool.Generation < e.cfg.MaxIterations {
		select {
		case <-ctx.Done():
			return &e.pool, ctx.Err()
		default:
		}
		if e.terminator != nil && e.terminator(&e.pool, e.pool.Generation) {
			break
		}
		if e.schedule != nil {
			e.rate, e.strength = e.schedule(e.pool.Generation, e.pool.Stats)
		}
		e.step()
		e.history = append(e.history, e.pool.Stats)
	}
	return &e.pool, nil
}

// EngineState is a generational engine's continuation point: its pool, its
// history, the mutation knobs in force and the random stream's position.
// Members alias the engine's memory, so a caller that goes on running the
// engine copies them first; Restore takes ownership of what it is given.
type EngineState[S Solution, F Numeric] struct {
	Version        int
	Config         EngineConfig
	Members        []Candidate[S, F]
	Generation     int
	History        []PoolStats[F]
	Rate, Strength float64
	RNG            []byte
}

// EngineStateVersion names the layout of EngineState
const EngineStateVersion = 1

// ErrInvalidEngineState is Restore's refusal of a state another layout, shape
// or engine made
var ErrInvalidEngineState = errors.New("genetic: invalid engine state")

// Checkpoint is the engine as it stands after a Run, to Restore into one built
// the same way, so a long search continues where it stopped.
func (e *Engine[S, F]) Checkpoint() (EngineState[S, F], error) {
	if !e.started {
		return EngineState[S, F]{}, fmt.Errorf("%w: nothing run yet", ErrInvalidEngineState)
	}
	rng, err := e.pcg.MarshalBinary()
	if err != nil {
		return EngineState[S, F]{}, err
	}
	return EngineState[S, F]{Version: EngineStateVersion, Config: e.cfg, Members: e.pool.Members,
		Generation: e.pool.Generation, History: e.history, Rate: e.rate, Strength: e.strength, RNG: rng}, nil
}

// Restore continues from a Checkpoint of an engine of this one's configuration;
// the next Run picks up at its generation.
func (e *Engine[S, F]) Restore(st EngineState[S, F]) error {
	switch {
	case st.Version != EngineStateVersion:
		return fmt.Errorf("%w: version %d, want %d", ErrInvalidEngineState, st.Version, EngineStateVersion)
	case st.Config != e.cfg:
		return fmt.Errorf("%w: another configuration", ErrInvalidEngineState)
	case len(st.Members) != e.cfg.PoolSize:
		return fmt.Errorf("%w: %d members, want %d", ErrInvalidEngineState, len(st.Members), e.cfg.PoolSize)
	}
	if err := e.pcg.UnmarshalBinary(st.RNG); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidEngineState, err)
	}
	e.pool.Members = append(e.pool.Members[:0], st.Members...)
	e.pool.Generation = st.Generation
	e.history = append(e.history[:0], st.History...)
	e.rate, e.strength = st.Rate, st.Strength
	e.started = true
	e.free.clear()
	e.finish()
	return nil
}

// Best returns the top-scoring candidate, aliasing engine memory
func (e *Engine[S, F]) Best() (Candidate[S, F], error) {
	if len(e.pool.Members) == 0 {
		return Candidate[S, F]{}, ErrNoCandidates
	}
	return e.pool.Members[0], nil
}

// History returns per-generation statistics
func (e *Engine[S, F]) History() []PoolStats[F] { return e.history }

// initialize seeds the pool. Solutions are generated serially because rng is
// not goroutine-safe; scoring is parallel
func (e *Engine[S, F]) initialize() {
	members := make([]Candidate[S, F], e.cfg.PoolSize)
	for i := range members {
		if i < len(e.seeds) {
			members[i].Data = e.seeds[i]
			continue
		}
		members[i].Data = e.initializer(e.rng)
	}
	e.evaluate(members)

	e.pool.Members = members
	e.pool.Generation = 0
	e.finish()
}

// step builds one generation: the sorted head is carried over as elites,
// the remainder comes from selection, recombination and mutation
func (e *Engine[S, F]) step() {
	elite := min(e.cfg.EliteCount, len(e.pool.Members))
	e.next = append(e.next[:0], e.pool.Members[:elite]...)

	for len(e.next) < e.cfg.PoolSize {
		e.selector.Select(e.pool.Members, e.parents, e.version, e.rng)
		n := e.combiner.Combine(e.parents, e.buf, e.rng)
		if n == 0 {
			break
		}
		for j := range n {
			e.perturbator.Perturb(&e.buf[j], e.rate, e.strength, e.rng)
			e.next = append(e.next, Candidate[S, F]{Data: e.buf[j]})
			e.buf[j] = e.free.take() // Ownership moved into next
			if len(e.next) == e.cfg.PoolSize {
				break
			}
		}
	}

	e.evaluate(e.next[elite:])
	// The members this generation replaces were parents until here; their
	// buffers take the next generation's offspring
	for _, c := range e.pool.Members[elite:] {
		e.free.put(c.Data)
	}
	e.pool.Members = append(e.pool.Members[:0], e.next...)
	e.pool.Generation++
	e.finish()
}

// evaluate scores candidates over a worker set bounded by Parallelism
func (e *Engine[S, F]) evaluate(members []Candidate[S, F]) {
	workers := min(e.cfg.Parallelism, len(members))
	if workers <= 1 {
		for i := range members {
			members[i].Score = e.evaluator(members[i].Data)
		}
		return
	}

	var next atomic.Int64
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for {
				i := int(next.Add(1)) - 1
				if i >= len(members) {
					return
				}
				members[i].Score = e.evaluator(members[i].Data)
			}
		})
	}
	wg.Wait()
}

// finish sorts the pool by score descending and refreshes statistics
func (e *Engine[S, F]) finish() {
	slices.SortFunc(e.pool.Members, func(a, b Candidate[S, F]) int {
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		}
		return 0
	})
	e.version++

	m := e.pool.Members
	s := PoolStats[F]{Size: len(m), Generation: e.pool.Generation}
	if len(m) > 0 {
		s.BestScore = m[0].Score
		s.WorstScore = m[len(m)-1].Score
		var total F
		for i := range m {
			total += m[i].Score
		}
		s.AverageScore = total / F(len(m))
	}
	if e.diversity != nil {
		s.Diversity = e.diversity(m)
	}
	e.pool.Stats = s
}
