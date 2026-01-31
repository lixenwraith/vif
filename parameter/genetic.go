package parameter

import "time"

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

// Genetic Algorithm - Drain Evolution Bounds
const (
	// Homing acceleration (cells/sec²)
	GADrainHomingAccelMin = 1.0
	GADrainHomingAccelMax = 8.0

	// Shield approach angle (radians, reserved)
	GADrainShieldApproachMin = 0.0
	GADrainShieldApproachMax = 6.28

	// Aggression multiplier (speed factor)
	GADrainAggressionMin = 0.8
	GADrainAggressionMax = 2.0

	// Perturbation standard deviation for drain genes
	GADrainPerturbationStdDev = 0.15
)

// Genetic Algorithm - Fitness Weights (Drain)
const (
	GADrainFitnessWeightEnergyDrain  = 0.4
	GADrainFitnessWeightSurvival     = 0.3
	GADrainFitnessWeightPositioning  = 0.2
	GADrainFitnessWeightCoordination = 0.0
	GADrainFitnessWeightHeatPenalty  = 0.1
)

// Genetic Algorithm - Fitness Normalization Defaults
const (
	GAFitnessMaxEnergyDefault = 1000
	GAFitnessMaxTicksDefault  = 600
)