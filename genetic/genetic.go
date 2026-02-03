package genetic

// Package genetic provides a flexible, generic-first genetic algorithm framework for optimization and search problems
// 1. Has zero knowledge of game-specific types in core packages
// 2. Supports both single entities and composite entities through unified interfaces
// 3. Uses registration patterns instead of hardcoded species initialization
// 4. Provides subscription-based metric collection instead of polling specific components
// 5. Can theoretically be extracted and used in another application without modification to core packages

import (
	"math/rand/v2"
	"sort"
)

// --- Concrete Operator Implementations ---

// TournamentSelector implements tournament selection
// Randomly samples small groups and selects the best from each group
type TournamentSelector[S Solution, F Numeric] struct {
	// TournamentSize is the number of candidates to compete in each tournament
	TournamentSize int
	// WithReplacement determines if selected candidates can be chosen again
	WithReplacement bool
}

// Select implements the Selector interface using tournament selection
func (ts *TournamentSelector[S, F]) Select(pool *Pool[S, F], size int, rng *rand.Rand) []Candidate[S, F] {
	selected := make([]Candidate[S, F], 0, size)
	poolSize := len(pool.Members)

	// Validate tournament size
	tournSize := ts.TournamentSize
	if tournSize > poolSize {
		tournSize = poolSize
	}
	if tournSize < 1 {
		tournSize = 2 // Default minimum
	}

	// Run tournaments until we have enough selected candidates
	for len(selected) < size {
		// Create tournament bracket
		tournament := make([]Candidate[S, F], tournSize)
		for i := 0; i < tournSize; i++ {
			tournament[i] = pool.Members[rng.IntN(poolSize)]
		}

		// Find winner (highest score)
		winner := tournament[0]
		for i := 1; i < len(tournament); i++ {
			if tournament[i].Score > winner.Score {
				winner = tournament[i]
			}
		}

		selected = append(selected, winner)

		// RemoveEntityAt winner from pool if not using replacement
		if !ts.WithReplacement {
			// This is simplified; real implementation would track indices
			// to avoid re-selection efficiently
		}
	}

	return selected[:size]
}

// RouletteSelector implements fitness-proportionate selection
// Candidates are selected with probability proportional to their fitness
type RouletteSelector[S Solution, F Numeric] struct {
	// Scaled determines if fitness values should be scaled before selection
	Scaled bool
}

// Select implements roulette wheel selection
func (rs *RouletteSelector[S, F]) Select(pool *Pool[S, F], size int, rng *rand.Rand) []Candidate[S, F] {
	// Calculate cumulative fitness scores
	totalScore := F(0)
	cumulative := make([]F, len(pool.Members))

	for i, candidate := range pool.Members {
		totalScore += candidate.Score
		cumulative[i] = totalScore
	}

	// Select candidates based on roulette wheel
	selected := make([]Candidate[S, F], size)
	for i := 0; i < size; i++ {
		// Spin the wheel
		spin := F(rng.Float64()) * totalScore

		// Find selected candidate
		for j, cum := range cumulative {
			if spin <= cum {
				selected[i] = pool.Members[j]
				break
			}
		}
	}

	return selected
}

// UniformCombiner performs uniform crossover between solutions
// Each element has equal probability of coming from either parent
type UniformCombiner[S ~[]T, T any, F Numeric] struct {
	// MixProbability is the chance of taking from parent 1 vs parent 2
	MixProbability float64
}

// Combine creates offspring using uniform crossover
func (uc *UniformCombiner[S, T, F]) Combine(parents []Candidate[S, F], rng *rand.Rand) []S {
	if len(parents) < 2 {
		// Need at least 2 parents for crossover
		if len(parents) == 1 {
			return []S{parents[0].Data}
		}
		return []S{}
	}

	parent1, parent2 := parents[0].Data, parents[1].Data
	length := min(len(parent1), len(parent2))

	// Create two offspring
	offspring1 := make(S, length)
	offspring2 := make(S, length)

	// Uniform crossover - each position independently chosen
	for i := 0; i < length; i++ {
		if rng.Float64() < uc.MixProbability {
			offspring1[i] = parent1[i]
			offspring2[i] = parent2[i]
		} else {
			offspring1[i] = parent2[i]
			offspring2[i] = parent1[i]
		}
	}

	return []S{offspring1, offspring2}
}

// NPointCombiner performs N-point crossover between solutions
// The solution is split at N random points and segments are alternated
type NPointCombiner[S ~[]T, T any, F Numeric] struct {
	// Points is the number of crossover points
	Points int
}

// Combine creates offspring using N-point crossover
func (nc *NPointCombiner[S, T, F]) Combine(parents []Candidate[S, F], rng *rand.Rand) []S {
	if len(parents) < 2 {
		if len(parents) == 1 {
			return []S{parents[0].Data}
		}
		return []S{}
	}

	parent1, parent2 := parents[0].Data, parents[1].Data
	length := min(len(parent1), len(parent2))

	// Generate crossover points
	points := make([]int, 0, nc.Points+2)
	points = append(points, 0)

	// Generate random unique points
	for i := 0; i < nc.Points && i < length-1; i++ {
		point := rng.IntN(length-1) + 1
		points = append(points, point)
	}
	points = append(points, length)

	sort.Ints(points)

	// Create offspring by alternating segments
	offspring1 := make(S, length)
	offspring2 := make(S, length)

	useParent1 := true
	for i := 0; i < len(points)-1; i++ {
		start, end := points[i], points[i+1]
		for j := start; j < end; j++ {
			if useParent1 {
				offspring1[j] = parent1[j]
				offspring2[j] = parent2[j]
			} else {
				offspring1[j] = parent2[j]
				offspring2[j] = parent1[j]
			}
		}
		useParent1 = !useParent1
	}

	return []S{offspring1, offspring2}
}