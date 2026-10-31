package genetic

import (
	"context"
	"errors"
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

	cfg EngineConfig
	rng *rand.Rand

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

	e := &Engine[S, F]{
		evaluator:   evaluator,
		initializer: initializer,
		selector:    selector,
		combiner:    combiner,
		perturbator: perturbator,
		cfg:         cfg,
		rng:         rand.New(rand.NewPCG(cfg.Seed, cfg.Seed^defaultSeed)),
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

// Run evolves until MaxIterations, the terminator, or context cancellation.
// The pool it returns is engine memory, rewritten by the next Run
func (e *Engine[S, F]) Run(ctx context.Context) (*Pool[S, F], error) {
	e.initialize()

	for i := range e.cfg.MaxIterations {
		select {
		case <-ctx.Done():
			return &e.pool, ctx.Err()
		default:
		}
		if e.terminator != nil && e.terminator(&e.pool, i) {
			break
		}
		e.step()
		e.history = append(e.history, e.pool.Stats)
	}
	return &e.pool, nil
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
			e.perturbator.Perturb(&e.buf[j],
				e.cfg.PerturbationRate, e.cfg.PerturbationStrength, e.rng)
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
