package registry

import (
	"math/rand/v2"
	"sync"
	"time"

	"github.com/lixenwraith/vi-fighter/genetic"
	"github.com/lixenwraith/vi-fighter/genetic/fitness"
	"github.com/lixenwraith/vi-fighter/genetic/tracking"
)

// TrackedSpecies manages evolution for a single species
type TrackedSpecies struct {
	Config     SpeciesConfig
	Engine     *genetic.StreamingEngine[[]float64, float64]
	Aggregator fitness.Aggregator

	active   map[uint64]*activeEval
	activeMu sync.RWMutex

	pool *tracking.CollectorPool
}

type activeEval struct {
	evalID    uint64
	collector tracking.Collector
	startTime time.Time
}

// NewTrackedSpecies creates a tracker with configured engine
func NewTrackedSpecies(cfg SpeciesConfig, agg fitness.Aggregator) *TrackedSpecies {
	perturbator := &genetic.BoundedPerturbator{
		Bounds:            cfg.Bounds,
		StandardDeviation: cfg.PerturbationStdDev,
	}

	initializer := func(rng *rand.Rand) []float64 {
		g := make([]float64, cfg.GeneCount)
		for i, b := range cfg.Bounds {
			if i < cfg.GeneCount {
				g[i] = b.Min + rng.Float64()*(b.Max-b.Min)
			}
		}
		return g
	}

	selector := &genetic.TournamentSelector[[]float64, float64]{
		TournamentSize:  3,
		WithReplacement: true,
	}

	combiner := &genetic.UniformCombiner[[]float64, float64, float64]{
		MixProbability: 0.5,
	}

	engineCfg := genetic.DefaultStreamingConfig()
	if cfg.EngineConfig != nil {
		engineCfg = *cfg.EngineConfig
	}

	engine := genetic.NewStreamingEngine(
		initializer,
		selector,
		combiner,
		perturbator,
		engineCfg,
	)

	return &TrackedSpecies{
		Config:     cfg,
		Engine:     engine,
		Aggregator: agg,
		active:     make(map[uint64]*activeEval),
		pool:       tracking.NewCollectorPool(32),
	}
}

// Sample returns genotype and evaluation ID
func (ts *TrackedSpecies) Sample() ([]float64, uint64) {
	samples := ts.Engine.SamplePopulation(1)
	if len(samples) == 0 {
		g := make([]float64, ts.Config.GeneCount)
		for i, b := range ts.Config.Bounds {
			g[i] = (b.Min + b.Max) / 2
		}
		return g, 0
	}

	evalID := ts.Engine.BeginEvaluation(samples[0])
	return samples[0], uint64(evalID)
}

// BeginTracking starts metric collection for an evaluation
func (ts *TrackedSpecies) BeginTracking(evalID uint64) tracking.Collector {
	var collector tracking.Collector
	if ts.Config.IsComposite {
		collector = ts.pool.AcquireComposite()
	} else {
		collector = ts.pool.AcquireStandard()
	}

	ts.activeMu.Lock()
	ts.active[evalID] = &activeEval{
		evalID:    evalID,
		collector: collector,
		startTime: time.Now(),
	}
	ts.activeMu.Unlock()

	return collector
}

// CompleteTracking finalizes and reports fitness
func (ts *TrackedSpecies) CompleteTracking(evalID uint64, deathCondition tracking.MetricBundle, ctx fitness.Context) {
	ts.activeMu.Lock()
	active, ok := ts.active[evalID]
	if ok {
		delete(ts.active, evalID)
	}
	ts.activeMu.Unlock()

	if !ok {
		return
	}

	metrics := active.collector.Finalize(deathCondition)

	var fitnessVal float64
	if ts.Aggregator != nil {
		fitnessVal = ts.Aggregator.Calculate(metrics, ctx)
	}

	ts.Engine.CompleteEvaluation(genetic.EvalID(evalID), fitnessVal)

	// Return collector to pool
	if ts.Config.IsComposite {
		if c, ok := active.collector.(*tracking.CompositeCollector); ok {
			ts.pool.ReleaseComposite(c)
		}
	} else {
		if c, ok := active.collector.(*tracking.StandardCollector); ok {
			ts.pool.ReleaseStandard(c)
		}
	}
}

// CollectMetrics pushes metrics to active evaluation
func (ts *TrackedSpecies) CollectMetrics(evalID uint64, metrics tracking.MetricBundle, dt time.Duration) {
	ts.activeMu.RLock()
	active, ok := ts.active[evalID]
	ts.activeMu.RUnlock()

	if ok {
		active.collector.Collect(metrics, dt)
	}
}

// Stats returns population statistics
func (ts *TrackedSpecies) Stats() Stats {
	best, worst, avg, _ := ts.Engine.PoolStats()
	return Stats{
		Generation:   ts.Engine.Generation(),
		BestFitness:  best,
		WorstFitness: worst,
		AvgFitness:   avg,
		PendingCount: ts.Engine.PendingCount(),
		TotalEvals:   ts.Engine.EvaluationsStarted(),
	}
}

// Start begins the evolution engine
func (ts *TrackedSpecies) Start() {
	ts.Engine.Start()
}

// Stop halts the evolution engine
func (ts *TrackedSpecies) Stop() {
	ts.Engine.Stop()
}