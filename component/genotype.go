package component

import "time"

// SpeciesType identifies the entity type for genetic routing
// Matches registry.SpeciesID for type compatibility
type SpeciesType uint8

const (
	SpeciesDrain SpeciesType = iota + 1
	SpeciesSwarm
	SpeciesQuasar
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