package parameter

import "time"

// Set persistence path constant after existing constants
const (
	// GeneticPersistencePath is the directory for population save files
	GeneticPersistencePath = "./config/genetic"
)

// Genetic Algorithm - Engine Configuration
const (
	// GAPoolSize is the number of candidates in each population
	GAPoolSize = 32

	// GAEliteCount is preserved best performers per generation
	GAEliteCount = 4

	// GAPerturbationRate is probability of mutation (0.0-1.0)
	GAPerturbationRate = 0.2

	// GAPerturbationStrength controls mutation intensity (0.0-1.0)
	GAPerturbationStrength = 0.15

	// GAMaxIterations caps synchronous evolution runs
	GAMaxIterations = 1000

	// GAParallelism for batch evaluation (unused in streaming)
	GAParallelism = 4

	// GATournamentSize for selection pressure
	GATournamentSize = 3

	// GACrossoverMixProbability for uniform crossover
	GACrossoverMixProbability = 0.5
)

// Genetic Algorithm - Streaming Configuration
const (
	// GATickBudget is max time for evolution step between frames
	GATickBudget = 6 * time.Millisecond

	// GAOutcomeBufferSize is channel capacity for deferred evaluations
	GAOutcomeBufferSize = 256

	// GAMinOutcomesPerGen triggers evolution after N fitness results
	GAMinOutcomesPerGen = 5
)

// Route Distribution — Batched Softmax Bandit
const (
	// RoutePoolDefaultSize is pre-sampled assignments per weight update cycle
	RoutePoolDefaultSize = 100

	// RouteLearningRate (eta) for EXP3-style weight update
	RouteLearningRate = 0.1

	// RouteMinWeight floor prevents route starvation
	RouteMinWeight = 0.05

	// RouteDrainTimeout is max time to retain draining route state after gateway death
	RouteDrainTimeout = 60 * time.Second
)