package genetic

import (
	"math/rand/v2"
)

// --- Core Type Constraints ---

// Solution represents any type that can be used as a solution encoding
// This is the most general constraint - any type can be a solution
type Solution any

// Numeric constrains types to numeric values for fitness scores
type Numeric interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 |
		~float32 | ~float64
}

// --- Core Data Structures ---

// Candidate represents a potential solution with its evaluated quality score
// S is the solution type, F is the fitness/quality score type
type Candidate[S Solution, F Numeric] struct {
	// Data holds the encoded solution representation
	Data S
	// Score represents the quality/fitness of this solution (higher = better)
	Score F
	// Metadata can store additional information about this candidate
	Metadata map[string]any
}

// Pool represents a collection of solution candidates
// This is the working set of solutions at any given iteration
type Pool[S Solution, F Numeric] struct {
	// Members contains all candidates in this pool
	Members []Candidate[S, F]
	// Generation tracks the iteration number this pool represents
	Generation int
	// Stats holds statistical information about this pool
	Stats PoolStats[F]
}

// PoolStats contains statistical information about a candidate pool
type PoolStats[F Numeric] struct {
	BestScore    F
	WorstScore   F
	AverageScore F
	Diversity    float64 // Measure of solution diversity (0-1)
}

// --- Function Types for Flexibility ---

// EvaluatorFunc defines a function that calculates the quality score for a solution
// This is passed as a function to allow maximum flexibility in fitness calculation
type EvaluatorFunc[S Solution, F Numeric] func(solution S) F

// InitializerFunc creates an initial solution candidate
// Used for generating the initial population with various strategies
type InitializerFunc[S Solution] func(rng *rand.Rand) S

// TerminationFunc determines if the algorithm should stop
// Returns true when termination criteria are met
type TerminationFunc[S Solution, F Numeric] func(pool *Pool[S, F], iteration int) bool

// --- Core Operators as Interfaces ---

// Selector defines the selection operator for choosing candidates for reproduction
// Multiple selection strategies can be implemented (tournament, roulette, rank, etc.)
type Selector[S Solution, F Numeric] interface {
	// Select chooses candidates from the pool for reproduction
	// The size parameter indicates how many candidates to select
	Select(pool *Pool[S, F], size int, rng *rand.Rand) []Candidate[S, F]
}

// Combiner defines the recombination operator for creating new solutions
// This combines information from parent solutions to create offspring
type Combiner[S Solution, F Numeric] interface {
	// Combine creates offspring from parent solutions
	// Returns one or more new solution encodings
	Combine(parents []Candidate[S, F], rng *rand.Rand) []S
}

// Perturbator defines the mutation operator for introducing variation
// This makes small random changes to maintain diversity in the solution space
type Perturbator[S Solution] interface {
	// Perturb modifies a solution in-place to introduce variation
	// The rate parameter controls the intensity of perturbation (0-1)
	Perturb(solution *S, rate float64, rng *rand.Rand)
}