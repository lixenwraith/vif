package genetic

import (
	"math/rand/v2"
)

// --- Extension Points for Monte Carlo and Other Methods ---

// MonteCarloInitializer creates random solutions using Monte Carlo sampling.
// This can be used as an InitializerFunc for stochastic initialization
type MonteCarloInitializer[S Solution] struct {
	// SampleSpace defines the range of possible values
	SampleSpace func(rng *rand.Rand) S
	// Constraints defines validity checks for generated solutions
	Constraints func(solution S) bool
	// MaxAttempts limits retry attempts for constraint satisfaction
	MaxAttempts int
}

// Generate creates a solution using Monte Carlo sampling
func (mci *MonteCarloInitializer[S]) Generate(rng *rand.Rand) S {
	for attempt := 0; attempt < mci.MaxAttempts; attempt++ {
		candidate := mci.SampleSpace(rng)
		if mci.Constraints == nil || mci.Constraints(candidate) {
			return candidate
		}
	}
	// Fallback to last generated if constraints never satisfied
	return mci.SampleSpace(rng)
}

// AdaptiveEngine extends the basic Engine with adaptive parameter control
// Parameters like mutation rate can be adjusted based on convergence metrics
type AdaptiveEngine[S Solution, F Numeric] struct {
	*Engine[S, F]
	// AdaptationStrategy defines how parameters change over time
	AdaptationStrategy func(stats PoolStats[F], generation int) EngineConfig
}

// --- Example Perturbator Implementation ---

// BitFlipPerturbator flips bits in binary-encoded solutions
type BitFlipPerturbator struct{}

// Perturb implements bit-flipping mutation for byte slices
func (bfp *BitFlipPerturbator) Perturb(solution *[]byte, rate float64, rng *rand.Rand) {
	if solution == nil {
		return
	}

	// Apply bit flips based on rate
	for i := range *solution {
		if rng.Float64() < rate {
			// Flip a random bit in this byte
			bitPos := rng.IntN(8)
			(*solution)[i] ^= (1 << bitPos)
		}
	}
}

// GaussianPerturbator adds Gaussian noise to numeric solutions
type GaussianPerturbator[S ~[]F, F Numeric] struct {
	// StandardDeviation controls the spread of noise
	StandardDeviation float64
}

// Perturb adds Gaussian noise to numeric values
func (gp *GaussianPerturbator[S, F]) Perturb(solution *S, rate float64, rng *rand.Rand) {
	if solution == nil {
		return
	}

	for i := range *solution {
		if rng.Float64() < rate {
			// Add Gaussian noise
			noise := F(rng.NormFloat64() * gp.StandardDeviation)
			(*solution)[i] += noise
		}
	}
}