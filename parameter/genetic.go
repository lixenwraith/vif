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

// Genetic Algorithm - Eye Evolution Bounds
const (
	// FlowLookahead: extended range for long maze paths
	GAEyeFlowLookaheadMin = 2.0
	GAEyeFlowLookaheadMax = 60.0

	// BudgetMultiplier: distance budget ratio (1.0 = optimal only, 2.5 = up to 2.5× optimal)
	GAEyeBudgetMultiplierMin = 1.0
	GAEyeBudgetMultiplierMax = 2.5

	// ExplorationBias: direction preference within budget (0.0 = progress, 1.0 = explore)
	GAEyeExplorationBiasMin = 0.0
	GAEyeExplorationBiasMax = 1.0

	// Perturbation standard deviation for eye genes
	GAEyePerturbationStdDev = 0.10
)

// Genetic Algorithm - Fitness Weights (Eye)
const (
	GAEyeFitnessWeightReachedTarget  = 0.5
	GAEyeFitnessWeightSpeedIfReached = 0.3
	GAEyeFitnessWeightSurvival       = 0.2
)

// Genetic Algorithm - Eye Fitness Normalization
const (
	// GAEyeFitnessMaxTicks is inverse normalization cap for speed-if-reached metric
	GAEyeFitnessMaxTicks = 400

	// GAEyeFitnessSurvivalCap is the tick count at which survival score saturates (NormalizeCap)
	GAEyeFitnessSurvivalCap = 600.0

	// GAEyeReachedTargetDistSq is squared distance threshold for target-reach detection
	// Accounts for composite header offset from exact target position
	GAEyeReachedTargetDistSq = 25
)