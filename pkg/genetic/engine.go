package genetic

import (
	"context"
	"errors"
	"math/rand/v2"
	"slices"
	"sync"
)

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
	next    []Candidate[S, F]
	parents []Candidate[S, F]
	buf     []S
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

	seed := cfg.Seed
	if seed == 0 {
		seed = rand.Uint64()
	}

	return &Engine[S, F]{
		evaluator:   evaluator,
		initializer: initializer,
		selector:    selector,
		combiner:    combiner,
		perturbator: perturbator,
		cfg:         cfg,
		rng:         rand.New(rand.NewPCG(seed, seed^0x9E3779B97F4A7C15)),
		parents:     make([]Candidate[S, F], 2),
		buf:         make([]S, 2),
		next:        make([]Candidate[S, F], 0, cfg.PoolSize),
		history:     make([]PoolStats[F], 0, cfg.MaxIterations),
	}
}

func (e *Engine[S, F]) SetTerminator(fn TerminationFunc[S, F]) { e.terminator = fn }
func (e *Engine[S, F]) SetDiversity(fn DiversityFunc[S, F])    { e.diversity = fn }

// Run evolves until MaxIterations, the terminator, or context cancellation
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

// Best returns the top-scoring candidate
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

	var zero S
	for len(e.next) < e.cfg.PoolSize {
		e.selector.Select(e.pool.Members, e.parents, e.rng)
		n := e.combiner.Combine(e.parents, e.buf, e.rng)
		if n == 0 {
			break
		}
		for j := range n {
			e.perturbator.Perturb(&e.buf[j],
				e.cfg.PerturbationRate, e.cfg.PerturbationStrength, e.rng)
			e.next = append(e.next, Candidate[S, F]{Data: e.buf[j]})
			e.buf[j] = zero // Ownership transferred; combiner reallocates
			if len(e.next) == e.cfg.PoolSize {
				break
			}
		}
	}

	e.evaluate(e.next[elite:])
	e.pool.Members = append(e.pool.Members[:0], e.next...)
	e.pool.Generation++
	e.finish()
}

// evaluate scores candidates, bounded by Parallelism
func (e *Engine[S, F]) evaluate(members []Candidate[S, F]) {
	if len(members) == 0 {
		return
	}
	if e.cfg.Parallelism <= 1 {
		for i := range members {
			members[i].Score = e.evaluator(members[i].Data)
		}
		return
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, e.cfg.Parallelism)
	for i := range members {
		wg.Add(1)
		sem <- struct{}{}
		go func(c *Candidate[S, F]) {
			defer wg.Done()
			defer func() { <-sem }()
			c.Score = e.evaluator(c.Data)
		}(&members[i])
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
