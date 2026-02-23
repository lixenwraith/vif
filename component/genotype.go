package component

import "time"

// SpeciesType identifies the entity type for genetic routing
type SpeciesType uint8

const (
	SpeciesNone SpeciesType = iota
	SpeciesDrain
	SpeciesSwarm
	SpeciesQuasar
	SpeciesStorm
	SpeciesPylon
	SpeciesSnake
	SpeciesEye
	SpeciesCount
)

// GenotypeComponent stores evolution data for tracked entities
type GenotypeComponent struct {
	Genes     []float64
	EvalID    uint64
	Species   SpeciesType
	SpawnTime time.Time

	// Observed metrics (updated by GeneticSystem)
	TicksAlive       int
	CumulativeDistSq float64
	DistSamples      int
	TimeInShield     time.Duration
}

// GeneticStats holds telemetry for a species population
type GeneticStats struct {
	Generation    int
	Best          float64
	Worst         float64
	Avg           float64
	PendingCount  int
	OutcomesTotal uint64
}