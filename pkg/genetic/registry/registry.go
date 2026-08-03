package registry

import (
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/pkg/genetic"
	"github.com/lixenwraith/vi-fighter/pkg/genetic/fitness"
	"github.com/lixenwraith/vi-fighter/pkg/genetic/persistence"
	"github.com/lixenwraith/vi-fighter/pkg/genetic/tracking"
)

const maxSpecies = 256

// Registry manages species registration and evolution.
// Sampling and stats are lock-free; registration takes a mutex
type Registry struct {
	mu    sync.Mutex
	slots [maxSpecies]atomic.Pointer[TrackedSpecies]
	store persistence.Store
}

// NewRegistry creates a registry over the given store; nil disables persistence
func NewRegistry(store persistence.Store) *Registry {
	return &Registry{store: store}
}

// Register adds a species; may be called before or after Start
func (r *Registry) Register(config SpeciesConfig, aggregator fitness.Aggregator) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.slots[config.ID].Load() != nil {
		return fmt.Errorf("species %d already registered", config.ID)
	}
	if config.GeneCount != len(config.Bounds) {
		return fmt.Errorf("species %d: gene count %d != bounds count %d",
			config.ID, config.GeneCount, len(config.Bounds))
	}

	r.slots[config.ID].Store(NewTrackedSpecies(config.normalize(), aggregator))
	return nil
}

// Start loads persisted populations and starts every species not yet running.
// Idempotent: re-running never re-injects an already started species
func (r *Registry) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var lastErr error
	for i := range r.slots {
		ts := r.slots[i].Load()
		if ts == nil || ts.Started() {
			continue
		}
		if r.store != nil {
			dto, err := r.store.Load(ts.Config.Name)
			switch {
			case err == nil && len(dto.Candidates) > 0:
				ts.Engine.Inject(dto.ToPool(), dto.Generation)
			case err != nil && !errors.Is(err, fs.ErrNotExist):
				lastErr = err // Missing file is normal on first run
			}
		}
		ts.Start()
	}
	return lastErr
}

func (r *Registry) Stop() {
	for i := range r.slots {
		if ts := r.slots[i].Load(); ts != nil {
			ts.Stop()
		}
	}
}

// Reset drops in-flight evaluations for every species; archives are retained
func (r *Registry) Reset() {
	for i := range r.slots {
		if ts := r.slots[i].Load(); ts != nil {
			ts.Engine.Reset()
		}
	}
}

func (r *Registry) get(id SpeciesID) *TrackedSpecies { return r.slots[id].Load() }

// Sample returns a caller-owned genotype and its evaluation id
func (r *Registry) Sample(id SpeciesID) ([]float64, uint64) {
	ts := r.get(id)
	if ts == nil {
		return nil, 0
	}
	return ts.Sample()
}

// SampleScout returns a stratified probe genotype
func (r *Registry) SampleScout(id SpeciesID) ([]float64, uint64) {
	ts := r.get(id)
	if ts == nil {
		return nil, 0
	}
	return ts.SampleScout()
}

// ReportFitness completes an evaluation directly, bypassing the aggregator
func (r *Registry) ReportFitness(id SpeciesID, evalID uint64, value float64) {
	if ts := r.get(id); ts != nil {
		ts.Engine.CompleteEvaluation(genetic.EvalID(evalID), value)
	}
}

// AbandonFitness discards an evaluation whose subject never materialized
func (r *Registry) AbandonFitness(id SpeciesID, evalID uint64) {
	if ts := r.get(id); ts != nil {
		ts.Engine.AbandonEvaluation(genetic.EvalID(evalID))
	}
}

func (r *Registry) BeginTracking(id SpeciesID, evalID uint64) tracking.Collector {
	ts := r.get(id)
	if ts == nil || evalID == 0 {
		return nil
	}
	return ts.BeginTracking(evalID)
}

func (r *Registry) CollectMetrics(id SpeciesID, evalID uint64, m tracking.MetricBundle, dt time.Duration) {
	if ts := r.get(id); ts != nil {
		ts.CollectMetrics(evalID, m, dt)
	}
}

func (r *Registry) CompleteTracking(id SpeciesID, evalID uint64, death tracking.MetricBundle, ctx fitness.Context) {
	if ts := r.get(id); ts != nil {
		ts.CompleteTracking(evalID, death, ctx)
	}
}

// Stats returns a lock-free statistics snapshot
func (r *Registry) Stats(id SpeciesID) Stats {
	ts := r.get(id)
	if ts == nil {
		return Stats{}
	}
	return ts.Stats()
}

// SaveAll persists every registered population
func (r *Registry) SaveAll() error {
	if r.store == nil {
		return nil
	}

	var lastErr error
	for i := range r.slots {
		ts := r.slots[i].Load()
		if ts == nil {
			continue
		}
		if err := r.store.Save(ts.Config.Name, persistence.FromPool(ts.Engine.Snapshot())); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

func (r *Registry) GetTracker(id SpeciesID) *TrackedSpecies { return r.get(id) }
