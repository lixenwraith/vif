package registry

import (
	"fmt"

	"github.com/lixenwraith/vi-fighter/pkg/genetic"
)

// SpeciesID uniquely identifies a tracked species
type SpeciesID uint8

// SpeciesConfig defines evolution parameters for a species
type SpeciesConfig struct {
	ID        SpeciesID
	Name      string
	GeneCount int
	Bounds    []genetic.ParameterBounds

	// Boundary selects mutation out-of-range handling for every gene
	Boundary genetic.BoundaryMode

	// PerturbationStdDev overrides EngineConfig.PerturbationStrength when non-zero
	PerturbationStdDev float64
	TournamentSize     int
	MixProbability     float64

	// ProbeBins stratifies gene[0] for SampleScout; 0 disables stratification
	ProbeBins int
	// IsComposite selects the member-tracking collector
	IsComposite bool

	EngineConfig *genetic.StreamingConfig
}

// normalize fills unset operator parameters with package defaults
func (c SpeciesConfig) normalize() SpeciesConfig {
	if c.TournamentSize < 2 {
		c.TournamentSize = genetic.DefaultTournamentSize
	}
	if c.MixProbability <= 0 || c.MixProbability > 1 {
		c.MixProbability = genetic.DefaultMixProbability
	}
	if c.Name == "" {
		c.Name = fmt.Sprintf("species_%d", c.ID)
	}
	return c
}

// Stats holds population statistics
type Stats struct {
	Generation   int
	BestFitness  float64
	WorstFitness float64
	AvgFitness   float64
	Diversity    float64
	PoolSize     int
	PendingCount int
	TotalEvals   uint64
	Evicted      uint64
}
