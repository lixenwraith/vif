package game

import (
	"time"

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

// Sample returns genotype and evaluation ID for a species
func (r *GeneticResource) Sample(id registry.SpeciesID) ([]float64, uint64) {
	return r.registry.Sample(id)
}

// Decode returns typed phenotype for genes
func (r *GeneticResource) Decode(id registry.SpeciesID, genes []float64) any {
	decoder, ok := r.decoders[id]
	if !ok {
		return nil
	}
	return decoder(genes)
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

// Complete reports fitness directly (legacy compatibility)
func (r *GeneticResource) Complete(id registry.SpeciesID, evalID uint64, fitnessVal float64) {
	r.registry.ReportFitness(id, evalID, fitnessVal)
}

// Stats returns population statistics
func (r *GeneticResource) Stats(id registry.SpeciesID) registry.Stats {
	return r.registry.Stats(id)
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