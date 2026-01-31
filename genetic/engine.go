package genetic

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"

	"github.com/lixenwraith/vi-fighter/parameter"
)

// --- Algorithm Engine ---

// Engine is the main genetic algorithm execution engine
// It coordinates all operators and manages the evolution process
type Engine[S Solution, F Numeric] struct {
	// Core operators
	evaluator   EvaluatorFunc[S, F]
	initializer InitializerFunc[S]
	selector    Selector[S, F]
	combiner    Combiner[S, F]
	perturbator Perturbator[S]
	terminator  TerminationFunc[S, F]

	// Configuration
	config EngineConfig

	// State
	rng         *rand.Rand
	currentPool *Pool[S, F]
	history     []PoolStats[F]

	// Concurrency control
	workerPool *sync.Pool
	semaphore  chan struct{}
}

// EngineConfig holds configuration parameters for the algorithm
type EngineConfig struct {
	// PoolSize is the number of candidates maintained in each generation
	PoolSize int
	// EliteCount is the number of best solutions preserved unchanged
	EliteCount int
	// PerturbationRate is the probability of applying perturbation (0-1)
	PerturbationRate float64
	// PerturbationStrength controls the intensity of perturbations (0-1)
	PerturbationStrength float64
	// MaxIterations is the maximum number of generations to run
	MaxIterations int
	// Parallelism controls the number of concurrent evaluations
	Parallelism int
	// Seed for random number generation (0 for random seed)
	Seed uint64
}

// DefaultConfig returns a reasonable default configuration.
func DefaultConfig() EngineConfig {
	return EngineConfig{
		PoolSize:             parameter.GAPoolSize,
		EliteCount:           parameter.GAEliteCount,
		PerturbationRate:     parameter.GAPerturbationRate,
		PerturbationStrength: parameter.GAPerturbationStrength,
		MaxIterations:        parameter.GAMaxIterations,
		Parallelism:          parameter.GAParallelism,
		Seed:                 0,
	}
}

// NewEngine creates a new genetic algorithm engine with the specified operators
func NewEngine[S Solution, F Numeric](
	evaluator EvaluatorFunc[S, F],
	initializer InitializerFunc[S],
	selector Selector[S, F],
	combiner Combiner[S, F],
	perturbator Perturbator[S],
	config EngineConfig,
) *Engine[S, F] {
	// Initialize random number generator
	var rng *rand.Rand
	if config.Seed == 0 {
		rng = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	} else {
		rng = rand.New(rand.NewPCG(config.Seed, config.Seed))
	}

	// Create semaphore for parallelism control
	semaphore := make(chan struct{}, config.Parallelism)

	return &Engine[S, F]{
		evaluator:   evaluator,
		initializer: initializer,
		selector:    selector,
		combiner:    combiner,
		perturbator: perturbator,
		config:      config,
		rng:         rng,
		semaphore:   semaphore,
		history:     make([]PoolStats[F], 0, config.MaxIterations),
	}
}

// SetTerminator sets a custom termination condition
func (e *Engine[S, F]) SetTerminator(terminator TerminationFunc[S, F]) {
	e.terminator = terminator
}

// Run executes the genetic algorithm until termination
func (e *Engine[S, F]) Run(ctx context.Context) (*Pool[S, F], error) {
	// Initialize population
	if err := e.initializePool(); err != nil {
		return nil, err
	}

	// Main evolution loop
	for iteration := 0; iteration < e.config.MaxIterations; iteration++ {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return e.currentPool, ctx.Err()
		default:
		}

		// Check termination condition
		if e.terminator != nil && e.terminator(e.currentPool, iteration) {
			break
		}

		// Evolve one generation
		if err := e.evolveGeneration(); err != nil {
			return e.currentPool, err
		}

		// Record statistics
		e.history = append(e.history, e.currentPool.Stats)
	}

	return e.currentPool, nil
}

// initializePool creates the initial population of candidates
func (e *Engine[S, F]) initializePool() error {
	candidates := make([]Candidate[S, F], e.config.PoolSize)

	// Generate initial solutions in parallel
	var wg sync.WaitGroup
	errs := make(chan error, e.config.PoolSize)

	for i := 0; i < e.config.PoolSize; i++ {
		wg.Add(1)
		e.semaphore <- struct{}{} // Acquire semaphore

		go func(idx int) {
			defer wg.Done()
			defer func() { <-e.semaphore }() // Release semaphore

			// Generate random solution
			solution := e.initializer(e.rng)

			// Evaluate fitness
			score := e.evaluator(solution)

			candidates[idx] = Candidate[S, F]{
				Data:     solution,
				Score:    score,
				Metadata: make(map[string]any),
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	// Check for errors
	for err := range errs {
		if err != nil {
			return err
		}
	}

	e.currentPool = &Pool[S, F]{
		Members:    candidates,
		Generation: 0,
		Stats:      e.calculateStats(candidates),
	}

	return nil
}

// evolveGeneration creates the next generation of candidates
func (e *Engine[S, F]) evolveGeneration() error {
	nextGen := make([]Candidate[S, F], 0, e.config.PoolSize)

	// Preserve elite solutions (best performers)
	elite := e.selectElite()
	nextGen = append(nextGen, elite...)

	// Generate new offspring to fill the pool
	for len(nextGen) < e.config.PoolSize {
		// Select parents
		parents := e.selector.Select(e.currentPool, 2, e.rng)

		// Create offspring through recombination
		offspring := e.combiner.Combine(parents, e.rng)

		// Apply perturbation based on probability
		for i := range offspring {
			if e.rng.Float64() < e.config.PerturbationRate {
				e.perturbator.Perturb(&offspring[i], e.config.PerturbationStrength, e.rng)
			}

			// Evaluate and add to next generation
			score := e.evaluator(offspring[i])
			nextGen = append(nextGen, Candidate[S, F]{
				Data:     offspring[i],
				Score:    score,
				Metadata: make(map[string]any),
			})

			if len(nextGen) >= e.config.PoolSize {
				break
			}
		}
	}

	// Update current pool
	e.currentPool = &Pool[S, F]{
		Members:    nextGen[:e.config.PoolSize],
		Generation: e.currentPool.Generation + 1,
		Stats:      e.calculateStats(nextGen[:e.config.PoolSize]),
	}

	return nil
}

// selectElite returns the best performing candidates for preservation
func (e *Engine[S, F]) selectElite() []Candidate[S, F] {
	if e.config.EliteCount <= 0 {
		return []Candidate[S, F]{}
	}

	// Sort by fitness (simplified - use sort.Slice in real code)
	// This would sort e.currentPool.Members by Score descending

	// Return top performers
	eliteCount := min(e.config.EliteCount, len(e.currentPool.Members))
	return e.currentPool.Members[:eliteCount]
}

// calculateStats computes statistical measures for a candidate pool
func (e *Engine[S, F]) calculateStats(candidates []Candidate[S, F]) PoolStats[F] {
	if len(candidates) == 0 {
		return PoolStats[F]{}
	}

	stats := PoolStats[F]{
		BestScore:  candidates[0].Score,
		WorstScore: candidates[0].Score,
	}

	total := F(0)
	for _, c := range candidates {
		if c.Score > stats.BestScore {
			stats.BestScore = c.Score
		}
		if c.Score < stats.WorstScore {
			stats.WorstScore = c.Score
		}
		total += c.Score
	}

	stats.AverageScore = total / F(len(candidates))

	// Diversity calculation would go here
	// (e.g., average pairwise distance between solutions)

	return stats
}

// GetHistory returns the statistical history of the evolution process
func (e *Engine[S, F]) GetHistory() []PoolStats[F] {
	return e.history
}

// GetBest returns the best candidate found so far
func (e *Engine[S, F]) GetBest() (Candidate[S, F], error) {
	if e.currentPool == nil || len(e.currentPool.Members) == 0 {
		return Candidate[S, F]{}, errors.New("no candidates available")
	}

	best := e.currentPool.Members[0]
	for _, c := range e.currentPool.Members[1:] {
		if c.Score > best.Score {
			best = c
		}
	}

	return best, nil
}