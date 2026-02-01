package registry

import (
	"fmt"
	"sync"
	"time"

	"github.com/lixenwraith/vi-fighter/genetic"
	"github.com/lixenwraith/vi-fighter/genetic/fitness"
	"github.com/lixenwraith/vi-fighter/genetic/persistence"
	"github.com/lixenwraith/vi-fighter/genetic/tracking"
)

// Registry manages species registration and evolution
type Registry struct {
	species     map[SpeciesID]*TrackedSpecies
	persistence *persistence.Manager
	mu          sync.RWMutex
}

// NewRegistry creates a registry with the given persistence path
func NewRegistry(persistPath string) *Registry {
	return &Registry{
		species:     make(map[SpeciesID]*TrackedSpecies),
		persistence: persistence.NewManager(persistPath),
	}
}

// Register adds a species to the registry (must be called before Start)
func (r *Registry) Register(config SpeciesConfig, aggregator fitness.Aggregator) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.species[config.ID]; exists {
		return fmt.Errorf("species %d already registered", config.ID)
	}

	if config.GeneCount != len(config.Bounds) {
		return fmt.Errorf("species %d: gene count %d != bounds count %d",
			config.ID, config.GeneCount, len(config.Bounds))
	}

	ts := NewTrackedSpecies(config, aggregator)
	r.species[config.ID] = ts
	return nil
}

// Start initializes all engines and loads persisted populations
func (r *Registry) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, ts := range r.species {
		dto, err := r.persistence.Load(ts.Config.Name)
		if err == nil && len(dto.Candidates) > 0 {
			candidates := dto.ToPool()
			ts.Engine.InjectPopulation(candidates, dto.Generation)
		}
		ts.Start()
	}
	return nil
}

// Stop halts all engines
func (r *Registry) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, ts := range r.species {
		ts.Stop()
	}
}

// Sample returns genotype for evaluation
func (r *Registry) Sample(id SpeciesID) ([]float64, uint64) {
	r.mu.RLock()
	ts, ok := r.species[id]
	r.mu.RUnlock()

	if !ok {
		return nil, 0
	}
	return ts.Sample()
}

// BeginTracking starts metric collection
func (r *Registry) BeginTracking(id SpeciesID, evalID uint64) tracking.Collector {
	r.mu.RLock()
	ts, ok := r.species[id]
	r.mu.RUnlock()

	if !ok || evalID == 0 {
		return nil
	}
	return ts.BeginTracking(evalID)
}

// CompleteTracking finalizes and calculates fitness
func (r *Registry) CompleteTracking(id SpeciesID, evalID uint64, deathCondition tracking.MetricBundle, ctx fitness.Context) {
	r.mu.RLock()
	ts, ok := r.species[id]
	r.mu.RUnlock()

	if !ok {
		return
	}
	ts.CompleteTracking(evalID, deathCondition, ctx)
}

// CollectMetrics pushes metrics for active evaluation
func (r *Registry) CollectMetrics(id SpeciesID, evalID uint64, metrics tracking.MetricBundle, dt time.Duration) {
	r.mu.RLock()
	ts, ok := r.species[id]
	r.mu.RUnlock()

	if !ok {
		return
	}
	ts.CollectMetrics(evalID, metrics, dt)
}

// ReportFitness directly reports fitness (bypasses aggregator)
func (r *Registry) ReportFitness(id SpeciesID, evalID uint64, fitnessVal float64) {
	r.mu.RLock()
	ts, ok := r.species[id]
	r.mu.RUnlock()

	if !ok {
		return
	}
	ts.Engine.CompleteEvaluation(genetic.EvalID(evalID), fitnessVal)
}

// Stats returns population statistics
func (r *Registry) Stats(id SpeciesID) Stats {
	r.mu.RLock()
	ts, ok := r.species[id]
	r.mu.RUnlock()

	if !ok {
		return Stats{}
	}
	return ts.Stats()
}

// SaveAll persists all populations
func (r *Registry) SaveAll() error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var lastErr error
	for _, ts := range r.species {
		pool := ts.Engine.GetPoolSnapshot()
		if pool == nil {
			continue
		}

		dto := persistence.FromPool(pool)
		if err := r.persistence.Save(ts.Config.Name, dto); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// GetTracker returns tracker for direct access (testing, telemetry)
func (r *Registry) GetTracker(id SpeciesID) *TrackedSpecies {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.species[id]
}