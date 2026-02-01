package game

import (
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/genetic/fitness"
	"github.com/lixenwraith/vi-fighter/genetic/game/species"
	"github.com/lixenwraith/vi-fighter/genetic/registry"
	"github.com/lixenwraith/vi-fighter/genetic/tracking"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// DecoderFunc converts genotype to game-specific phenotype
type DecoderFunc func(genes []float64) any

// GeneticResource provides GA services to the ECS
type GeneticResource struct {
	registry    *registry.Registry
	playerModel *PlayerBehaviorModel
	decoders    map[registry.SpeciesID]DecoderFunc
}

// NewGeneticResource creates the resource and registers species
func NewGeneticResource() *GeneticResource {
	r := &GeneticResource{
		registry:    registry.NewRegistry(parameter.GeneticPersistencePath),
		playerModel: NewPlayerBehaviorModel(),
		decoders:    make(map[registry.SpeciesID]DecoderFunc),
	}

	// Register drain species
	_ = r.registry.Register(species.DrainConfig, species.NewDrainAggregator())
	r.RegisterDecoder(species.DrainSpeciesID, species.DecodeDrain)

	return r
}

// RegisterDecoder adds a phenotype decoder for a species
func (r *GeneticResource) RegisterDecoder(id registry.SpeciesID, decoder DecoderFunc) {
	r.decoders[id] = decoder
}

// Start initializes evolution engines
func (r *GeneticResource) Start() {
	_ = r.registry.Start()
}

// Stop halts evolution engines
func (r *GeneticResource) Stop() {
	r.registry.Stop()
}

// Reset clears session state (populations retained)
func (r *GeneticResource) Reset() {
	r.playerModel.Reset()
}

// Sample returns genotype and evaluation ID for a species (implements GeneticProvider)
func (r *GeneticResource) Sample(species component.SpeciesType) ([]float64, uint64) {
	return r.registry.Sample(registry.SpeciesID(species))
}

// Decode returns typed phenotype for genes (implements GeneticProvider)
func (r *GeneticResource) Decode(species component.SpeciesType, genes []float64) any {
	decoder, ok := r.decoders[registry.SpeciesID(species)]
	if !ok {
		return nil
	}
	return decoder(genes)
}

// Complete reports fitness directly (implements GeneticProvider)
func (r *GeneticResource) Complete(species component.SpeciesType, evalID uint64, fitness float64) {
	r.registry.ReportFitness(registry.SpeciesID(species), evalID, fitness)
}

// Stats returns population statistics (implements GeneticProvider)
func (r *GeneticResource) Stats(species component.SpeciesType) component.GeneticStats {
	s := r.registry.Stats(registry.SpeciesID(species))
	return component.GeneticStats{
		Generation:    s.Generation,
		Best:          s.BestFitness,
		Worst:         s.WorstFitness,
		Avg:           s.AvgFitness,
		PendingCount:  s.PendingCount,
		OutcomesTotal: s.TotalEvals,
	}
}

// BeginTracking starts metric collection for an evaluation
func (r *GeneticResource) BeginTracking(id registry.SpeciesID, evalID uint64) tracking.Collector {
	return r.registry.BeginTracking(id, evalID)
}

// CollectMetrics pushes metrics for an active evaluation
func (r *GeneticResource) CollectMetrics(id registry.SpeciesID, evalID uint64, metrics tracking.MetricBundle, dt time.Duration) {
	r.registry.CollectMetrics(id, evalID, metrics, dt)
}

// CompleteTracking finalizes metrics and calculates fitness
func (r *GeneticResource) CompleteTracking(id registry.SpeciesID, evalID uint64, deathCondition tracking.MetricBundle) {
	r.registry.CompleteTracking(id, evalID, deathCondition, r.PlayerContext())
}

// SaveAll persists all populations
func (r *GeneticResource) SaveAll() error {
	return r.registry.SaveAll()
}

// PlayerModel returns the player behavior model
func (r *GeneticResource) PlayerModel() *PlayerBehaviorModel {
	return r.playerModel
}

// PlayerContext returns current player state as fitness context
func (r *GeneticResource) PlayerContext() fitness.Context {
	snapshot := r.playerModel.Snapshot()
	return fitness.MapContext{
		fitness.ContextThreatLevel:      snapshot.ThreatLevel(),
		fitness.ContextEnergyManagement: snapshot.EnergyManagement,
		fitness.ContextHeatManagement:   snapshot.HeatManagement,
		fitness.ContextTypingAccuracy:   snapshot.TypingAccuracy,
		"reaction_time_ms":              float64(snapshot.ReactionTime.Milliseconds()),
	}
}

// Tracker returns species tracker for telemetry
func (r *GeneticResource) Tracker(id registry.SpeciesID) *registry.TrackedSpecies {
	return r.registry.GetTracker(id)
}

// AcquireCollector gets a standard collector from pool
func (r *GeneticResource) AcquireCollector() *tracking.StandardCollector {
	ts := r.registry.GetTracker(species.DrainSpeciesID)
	if ts == nil {
		return tracking.NewStandardCollector()
	}
	return ts.AcquireCollector()
}

// ReleaseCollector returns collector to pool
func (r *GeneticResource) ReleaseCollector(c *tracking.StandardCollector) {
	ts := r.registry.GetTracker(species.DrainSpeciesID)
	if ts != nil {
		ts.ReleaseCollector(c)
	}
}

// AcquireCompositeCollector gets a composite collector
func (r *GeneticResource) AcquireCompositeCollector() *tracking.CompositeCollector {
	ts := r.registry.GetTracker(species.DrainSpeciesID)
	if ts == nil {
		return tracking.NewCompositeCollector()
	}
	return ts.AcquireCompositeCollector()
}

// ReleaseCompositeCollector returns composite collector to pool
func (r *GeneticResource) ReleaseCompositeCollector(c *tracking.CompositeCollector) {
	ts := r.registry.GetTracker(species.DrainSpeciesID)
	if ts != nil {
		ts.ReleaseCompositeCollector(c)
	}
}