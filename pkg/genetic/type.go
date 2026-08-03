package genetic

import "math/rand/v2"

// Solution is any candidate encoding
type Solution any

// Numeric constrains fitness score types
type Numeric interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 |
		~float32 | ~float64
}

// Candidate pairs an encoded solution with its evaluated score
type Candidate[S Solution, F Numeric] struct {
	Data  S
	Score F
}

// Pool is a scored candidate set. Engines maintain Members sorted by Score descending
type Pool[S Solution, F Numeric] struct {
	Members    []Candidate[S, F]
	Generation int
	Stats      PoolStats[F]
}

// PoolStats is an immutable engine state snapshot
type PoolStats[F Numeric] struct {
	BestScore    F
	WorstScore   F
	AverageScore F
	Diversity    float64
	Size         int
	Generation   int
	Pending      int
	Evaluations  uint64
	Evicted      uint64
}

type (
	InitializerFunc[S Solution]            func(rng *rand.Rand) S
	EvaluatorFunc[S Solution, F Numeric]   func(solution S) F
	TerminationFunc[S Solution, F Numeric] func(pool *Pool[S, F], iteration int) bool
	DiversityFunc[S Solution, F Numeric]   func(members []Candidate[S, F]) float64
)

// Selector fills dst with parents drawn from members. Implementations must not allocate
type Selector[S Solution, F Numeric] interface {
	Select(members []Candidate[S, F], dst []Candidate[S, F], rng *rand.Rand)
}

// Combiner writes offspring into dst, reusing dst buffers when shape-compatible.
// Returns the number of entries written
type Combiner[S Solution, F Numeric] interface {
	Combine(parents []Candidate[S, F], dst []S, rng *rand.Rand) int
}

// Perturbator mutates a solution in place.
// rate is the per-element probability, strength the magnitude scale
type Perturbator[S Solution] interface {
	Perturb(solution *S, rate, strength float64, rng *rand.Rand)
}

// Cloner copies src into dst, reusing dst capacity when possible.
// Engines require it to separate caller-owned from engine-owned solutions
type Cloner[S Solution] interface {
	Clone(dst, src S) S
}

// SliceCloner implements Cloner for slice-encoded genotypes
type SliceCloner[S ~[]T, T any] struct{}

func (SliceCloner[S, T]) Clone(dst, src S) S {
	if cap(dst) < len(src) {
		dst = make(S, len(src))
	}
	dst = dst[:len(src)]
	copy(dst, src)
	return dst
}

