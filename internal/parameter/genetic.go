package parameter

import (
	"time"

	"github.com/lixenwraith/vi-fighter/pkg/genetic"
)

// Genetic Algorithm - Population
const (
	GAPoolSize                = 32
	GAPerturbationRate        = 0.2
	GAPerturbationStrength    = 0.15
	GATournamentSize          = 3
	GACrossoverMixProbability = 0.5
	GAParallelism             = 4 // Batch engine only; unused by the streaming path

	// GAScoutRate is the fraction of gateway samples replaced by probe genotypes
	GAScoutRate = 0.08
)

// Genetic Algorithm - Streaming
const (
	// GATickBudget caps proposal generation per refill
	GATickBudget = 400 * time.Microsecond

	GAProposalCapacity = 32
	GAPendingCapacity  = 1024

	// GAMinOutcomesPerGen advances a generation after N fitness reports
	GAMinOutcomesPerGen = 5
)

// Genetic Algorithm - Fitness shaping
const (
	// GAFitnessDamageRef is the dealt-damage value that saturates the damage term
	GAFitnessDamageRef = 100.0

	// GAFitnessDamageWeight scales the saturated damage term against proximity (0..1)
	GAFitnessDamageWeight = 1.0
)

// GAStreamingConfig returns package defaults overridden by game parameters
func GAStreamingConfig() genetic.StreamingConfig {
	c := genetic.DefaultStreamingConfig()
	c.PoolSize = GAPoolSize
	c.PerturbationRate = GAPerturbationRate
	c.PerturbationStrength = GAPerturbationStrength
	c.TickBudget = GATickBudget
	c.ProposalCapacity = GAProposalCapacity
	c.PendingCapacity = GAPendingCapacity
	c.MinOutcomesPerGen = GAMinOutcomesPerGen
	return c
}

// GeneBounds is the interval for gene[0] of the species;
// decoded to species type via ParameterBounds.Bin. Must match bounds[0] at registration
var GeneBounds = genetic.ParameterBounds{Min: 0, Max: 1}

// GABoundaryMode folds out-of-range mutations instead of pinning them to a bound,
// which would over-weight the first and last phenotype bins
const GABoundaryMode = genetic.BoundaryReflect
