// Package registry manages up to 256 populations addressed by a SpeciesID: each
// a streaming engine over bounded real genes with stratified probes, an optional
// metric pipeline from tracking through fitness, archive persistence through a
// Store, and an exact export and import of every population's position.
package registry

import (
	"fmt"

	"github.com/lixenwraith/vif/pkg/genetic"
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

	TournamentSize int
	MixProbability float64

	// ProbeBins stratifies gene[0] for SampleScout; 0 disables stratification
	ProbeBins int
	// IsComposite selects the member-tracking collector
	IsComposite bool

	// EngineConfig is the species' engine configuration; nil takes the defaults
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
