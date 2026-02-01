package registry

import "github.com/lixenwraith/vi-fighter/genetic"

// SpeciesID uniquely identifies a tracked species
type SpeciesID uint8

// SpeciesConfig defines evolution parameters for a species
type SpeciesConfig struct {
	ID                 SpeciesID
	Name               string
	GeneCount          int
	Bounds             []genetic.ParameterBounds
	PerturbationStdDev float64
	IsComposite        bool
	EngineConfig       *genetic.StreamingConfig
}

// Stats holds population statistics
type Stats struct {
	Generation   int
	BestFitness  float64
	WorstFitness float64
	AvgFitness   float64
	PendingCount int
	TotalEvals   uint64
}