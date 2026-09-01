package genetic

import "time"

// Package defaults. Embedders override fields on the value returned by
// DefaultConfig / DefaultStreamingConfig.
const (
	DefaultPoolSize             = 32
	DefaultEliteCount           = 4
	DefaultPerturbationRate     = 0.2
	DefaultPerturbationStrength = 0.15
	DefaultMaxIterations        = 1000
	DefaultParallelism          = 4
	DefaultTickBudget           = 500 * time.Microsecond
	DefaultProposalCapacity     = 32
	DefaultMinOutcomesPerGen    = 5
	DefaultPendingCapacity      = 512
	DefaultTournamentSize       = 3
	DefaultMixProbability       = 0.5
)

// RefillMode selects how StreamingEngine bounds proposal refills.
//
// Deterministic refills are the default: the queue is filled completely, so a
// seed and an operation sequence determine one proposal stream independently of
// machine speed. Time-budgeted refills are available for callers that prefer a
// wall-clock bound and accept that scheduling can change the stream.
type RefillMode uint8

const (
	RefillDeterministic RefillMode = iota
	RefillTimeBudget
)

// EngineConfig holds parameters shared by the batch and steady-state engines
type EngineConfig struct {
	// PoolSize is the archive capacity (retained scored candidates)
	PoolSize int
	// EliteCount is the batch engine's preserved head count.
	// The steady-state archive is elitist by construction and ignores this
	EliteCount int
	// PerturbationRate is the per-element mutation probability
	PerturbationRate float64
	// PerturbationStrength is the mutation sigma as a fraction of the element range
	PerturbationStrength float64
	MaxIterations        int
	Parallelism          int
	// Seed fixes the PCG stream; the caller owns reproducibility and 0 is a valid seed
	Seed uint64
}

func DefaultConfig() EngineConfig {
	return EngineConfig{
		PoolSize:             DefaultPoolSize,
		EliteCount:           DefaultEliteCount,
		PerturbationRate:     DefaultPerturbationRate,
		PerturbationStrength: DefaultPerturbationStrength,
		MaxIterations:        DefaultMaxIterations,
		Parallelism:          DefaultParallelism,
	}
}

// Normalize clamps out-of-range fields to safe values
func (c EngineConfig) Normalize() EngineConfig {
	if c.PoolSize < 2 {
		c.PoolSize = DefaultPoolSize
	}
	if c.EliteCount < 0 {
		c.EliteCount = 0
	}
	if c.EliteCount >= c.PoolSize {
		c.EliteCount = c.PoolSize - 1
	}
	c.PerturbationRate = clamp01(c.PerturbationRate)
	c.PerturbationStrength = clamp01(c.PerturbationStrength)
	if c.MaxIterations < 1 {
		c.MaxIterations = DefaultMaxIterations
	}
	if c.Parallelism < 1 {
		c.Parallelism = 1
	}
	return c
}

// StreamingConfig extends EngineConfig with steady-state parameters
type StreamingConfig struct {
	EngineConfig
	// RefillMode selects deterministic full refills or the opt-in wall-clock cap.
	RefillMode RefillMode
	// TickBudget caps time spent generating proposals in one RefillTimeBudget
	// refill. It is ignored by RefillDeterministic.
	TickBudget time.Duration
	// ProposalCapacity is the depth of the unevaluated offspring queue
	ProposalCapacity int
	// PendingCapacity bounds in-flight evaluations; rounded up to a power of two.
	// Evaluations older than this many issues are evicted
	PendingCapacity int
	// MinOutcomesPerGen is the outcome count that advances a generation
	MinOutcomesPerGen int
}

func DefaultStreamingConfig() StreamingConfig {
	return StreamingConfig{
		EngineConfig:      DefaultConfig(),
		TickBudget:        DefaultTickBudget,
		ProposalCapacity:  DefaultProposalCapacity,
		PendingCapacity:   DefaultPendingCapacity,
		MinOutcomesPerGen: DefaultMinOutcomesPerGen,
	}
}

func (c StreamingConfig) Normalize() StreamingConfig {
	c.EngineConfig = c.EngineConfig.Normalize()
	if c.RefillMode != RefillDeterministic && c.RefillMode != RefillTimeBudget {
		c.RefillMode = RefillDeterministic
	}
	if c.TickBudget <= 0 {
		c.TickBudget = DefaultTickBudget
	}
	if c.ProposalCapacity < 2 {
		c.ProposalCapacity = DefaultProposalCapacity
	}
	if c.PendingCapacity < 8 {
		c.PendingCapacity = DefaultPendingCapacity
	}
	c.PendingCapacity = nextPow2(c.PendingCapacity)
	if c.MinOutcomesPerGen < 1 {
		c.MinOutcomesPerGen = 1
	}
	return c
}

func clamp01(v float64) float64 {
	if v < 0 || v != v {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func nextPow2(n int) int {
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}
