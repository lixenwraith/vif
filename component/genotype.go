package component

import "time"

// SpeciesType identifies the entity type for genetic routing
type SpeciesType uint8

const (
	SpeciesDrain SpeciesType = iota
	SpeciesSwarm
	SpeciesQuasar
)

// DecodedPhenotype holds decoded genetic parameters for all species
// Only fields relevant to the species are populated
type DecodedPhenotype struct {
	// Drain
	HomingAccel    int64
	AggressionMult int64

	// Swarm (future)
	ChaseSpeedMult int64
	LockDuration   time.Duration
	ChargeDuration time.Duration

	// Quasar (future)
	ZapDuration time.Duration
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