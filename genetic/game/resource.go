package game

import (
	"math/rand/v2"
	"time"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/genetic"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// GeneticResource holds all GA engines and supporting infrastructure
type GeneticResource struct {
	// Drain evolution
	DrainEngine     *genetic.StreamingEngine[[]float64, float64]
	DrainTracker    *EntityTracker
	DrainCodec      *DrainCodec
	DrainAggregator *CombatFitnessAggregator

	// Player behavior tracking
	PlayerModel *PlayerBehaviorModel
}

// NewGeneticResource creates and initializes all GA components
// Engines start immediately and idle until outcomes arrive
func NewGeneticResource() *GeneticResource {
	drainCodec := NewDrainCodec()
	drainAggregator := NewCombatFitnessAggregator(DefaultDrainWeights)

	// Drain initializer - creates random genotypes within bounds
	drainInitializer := func(rng *rand.Rand) []float64 {
		g := make([]float64, DrainGeneCount)
		for i, b := range DrainBounds {
			g[i] = b.Min + rng.Float64()*(b.Max-b.Min)
		}
		return g
	}

	// Drain perturbator with bounds
	drainPerturbator := &genetic.BoundedPerturbator{
		Bounds:            DrainBounds,
		StandardDeviation: parameter.GADrainPerturbationStdDev,
	}

	// Tournament selector
	drainSelector := &genetic.TournamentSelector[[]float64, float64]{
		TournamentSize:  parameter.GATournamentSize,
		WithReplacement: true,
	}

	// Uniform crossover
	drainCombiner := &genetic.UniformCombiner[[]float64, float64, float64]{
		MixProbability: parameter.GACrossoverMixProbability,
	}

	// Using defaults
	config := genetic.DefaultStreamingConfig()

	drainEngine := genetic.NewStreamingEngine(
		drainInitializer,
		drainSelector,
		drainCombiner,
		drainPerturbator,
		config,
	)

	drainTracker := NewEntityTracker(drainEngine, drainAggregator)

	r := &GeneticResource{
		DrainEngine:     drainEngine,
		DrainTracker:    drainTracker,
		DrainCodec:      drainCodec,
		DrainAggregator: drainAggregator,
		PlayerModel:     NewPlayerBehaviorModel(),
	}

	// Self-start: engine idles until outcomes arrive
	drainEngine.Start()

	return r
}

// Start is a no-op, engines self-start on creation
func (r *GeneticResource) Start() {}

// Stop halts evolution goroutines
func (r *GeneticResource) Stop() {
	r.DrainEngine.Stop()
}

// Reset clears tracking state for new game, population retained
func (r *GeneticResource) Reset() {
	r.DrainTracker.Clear()
}

// SampleDrainGenotype returns a genotype from current population
func (r *GeneticResource) SampleDrainGenotype() []float64 {
	samples := r.DrainEngine.SamplePopulation(1)
	if len(samples) == 0 {
		// Fallback to default if population not ready
		return []float64{
			3.0, // HomingAccel
			0.0, // DeflectBias
			0.0, // ShieldApproach
			1.0, // Aggression
		}
	}
	return samples[0]
}

// DecodeDrainPhenotype converts genotype to physics parameters
func (r *GeneticResource) DecodeDrainPhenotype(genotype []float64) (homingAccel, shieldApproach, aggressionMult int64) {
	p := r.DrainCodec.Decode(genotype)
	return p.HomingAccel, p.ShieldApproach, p.AggressionMult
}

// BeginDrainTracking starts tracking a drain entity
func (r *GeneticResource) BeginDrainTracking(entity core.Entity, genotype []float64, spawnTime time.Time) {
	r.DrainTracker.BeginTracking(entity, genotype, spawnTime)
}

// RecordDrainTick records per-tick metrics for a drain
func (r *GeneticResource) RecordDrainTick(entity core.Entity, distToCursor float64, inShield bool, dt time.Duration) {
	r.DrainTracker.RecordTick(entity, distToCursor, inShield, dt)
}

// RecordDrainEnergyDrain records energy drained by a drain entity
func (r *GeneticResource) RecordDrainEnergyDrain(entity core.Entity, amount int64) {
	r.DrainTracker.RecordEnergyDrain(entity, amount)
}

// RecordDrainHeatChange records heat reduction caused by drain
func (r *GeneticResource) RecordDrainHeatChange(entity core.Entity, delta int) {
	r.DrainTracker.RecordHeatChange(entity, delta)
}

// EndDrainTracking completes tracking and submits fitness
func (r *GeneticResource) EndDrainTracking(entity core.Entity, deathTime time.Time) {
	r.DrainTracker.EndTracking(entity, deathTime)
}

// RecordPlayerShieldState records player shield usage
func (r *GeneticResource) RecordPlayerShieldState(active bool) {
	r.PlayerModel.RecordShieldState(active)
}

// RecordPlayerKeystroke records typing accuracy
func (r *GeneticResource) RecordPlayerKeystroke(correct bool) {
	r.PlayerModel.RecordKeystroke(correct)
}

// RecordPlayerMovement records cursor movement
func (r *GeneticResource) RecordPlayerMovement(distance float64) {
	r.PlayerModel.RecordMovement(distance)
}

// DrainGeneration returns current GA generation for drain population
func (r *GeneticResource) DrainGeneration() int {
	return r.DrainEngine.Generation()
}

// DrainPoolStats returns fitness statistics
func (r *GeneticResource) DrainPoolStats() (best, worst, avg float64) {
	best, worst, avg, _ = r.DrainEngine.PoolStats()
	return
}

// DrainPendingCount returns evaluations in flight
func (r *GeneticResource) DrainPendingCount() int {
	return r.DrainEngine.PendingCount()
}

// DrainOutcomesTotal returns total outcomes processed
func (r *GeneticResource) DrainOutcomesTotal() uint64 {
	return r.DrainEngine.EvaluationsStarted()
}