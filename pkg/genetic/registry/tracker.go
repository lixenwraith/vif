package registry

import (
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/pkg/genetic"
	"github.com/lixenwraith/vi-fighter/pkg/genetic/fitness"
	"github.com/lixenwraith/vi-fighter/pkg/genetic/tracking"
)

// TrackedSpecies owns the engine and optional metric pipeline for one species
type TrackedSpecies struct {
	Config     SpeciesConfig
	Engine     *genetic.StreamingEngine[[]float64, float64]
	Aggregator fitness.Aggregator

	started atomic.Bool

	probeMu      sync.Mutex
	probeRng     *rand.Rand
	probeCounter uint64

	trackMu sync.Mutex
	active  map[uint64]tracking.Collector
	pool    *tracking.CollectorPool
}

func NewTrackedSpecies(cfg SpeciesConfig, agg fitness.Aggregator) *TrackedSpecies {
	engineCfg := genetic.DefaultStreamingConfig()
	if cfg.EngineConfig != nil {
		engineCfg = *cfg.EngineConfig
	}
	if cfg.PerturbationStdDev > 0 {
		engineCfg.PerturbationStrength = cfg.PerturbationStdDev
	}

	// Pin the seed so engine and probe streams are jointly reproducible
	if engineCfg.Seed == 0 {
		engineCfg.Seed = rand.Uint64()
	}

	bounds := cfg.Bounds
	initializer := func(rng *rand.Rand) []float64 {
		g := make([]float64, cfg.GeneCount)
		for i := range g {
			if i < len(bounds) {
				g[i] = bounds[i].Min + rng.Float64()*(bounds[i].Max-bounds[i].Min)
			}
		}
		return g
	}

	return &TrackedSpecies{
		Config:     cfg,
		Aggregator: agg,
		probeRng:   rand.New(rand.NewPCG(engineCfg.Seed^0xA5A5A5A5, engineCfg.Seed)),
		Engine: genetic.NewStreamingEngine[[]float64, float64](
			initializer,
			&genetic.TournamentSelector[[]float64, float64]{TournamentSize: cfg.TournamentSize},
			&genetic.UniformCombiner[[]float64, float64, float64]{MixProbability: cfg.MixProbability},
			&genetic.BoundedPerturbator{Bounds: bounds, Boundary: cfg.Boundary},
			genetic.SliceCloner[[]float64, float64]{},
			engineCfg,
		),
	}
}

func (ts *TrackedSpecies) Start() {
	ts.Engine.Start()
	ts.started.Store(true)
}

func (ts *TrackedSpecies) Stop() {
	ts.Engine.Stop()
	ts.started.Store(false)
}

func (ts *TrackedSpecies) Started() bool { return ts.started.Load() }

// Sample returns a genotype and evaluation id, falling back to bound midpoints
// with a zero id when the engine is stopped
func (ts *TrackedSpecies) Sample() ([]float64, uint64) {
	g, id := ts.Engine.Propose()
	if id == 0 {
		return ts.midpoint(), 0
	}
	return g, uint64(id)
}

// SampleScout synthesizes a probe genotype. gene[0] is stratified round-robin
// across ProbeBins (bin center); remaining genes are uniform within bounds
func (ts *TrackedSpecies) SampleScout() ([]float64, uint64) {
	n := ts.Config.GeneCount
	if n == 0 || len(ts.Config.Bounds) == 0 || !ts.started.Load() {
		return nil, 0
	}

	g := make([]float64, n)

	ts.probeMu.Lock()
	for i := 0; i < n && i < len(ts.Config.Bounds); i++ {
		b := ts.Config.Bounds[i]
		g[i] = b.Min + ts.probeRng.Float64()*(b.Max-b.Min)
	}
	if bins := ts.Config.ProbeBins; bins > 0 {
		bin := int(ts.probeCounter % uint64(bins))
		ts.probeCounter++
		g[0] = ts.Config.Bounds[0].BinCenter(bin, bins)
	}
	ts.probeMu.Unlock()

	return g, uint64(ts.Engine.BeginEvaluation(g))
}

func (ts *TrackedSpecies) midpoint() []float64 {
	g := make([]float64, ts.Config.GeneCount)
	for i := range g {
		if i < len(ts.Config.Bounds) {
			g[i] = (ts.Config.Bounds[i].Min + ts.Config.Bounds[i].Max) / 2
		}
	}
	return g
}

// BeginTracking starts metric collection for an evaluation
func (ts *TrackedSpecies) BeginTracking(evalID uint64) tracking.Collector {
	ts.trackMu.Lock()
	defer ts.trackMu.Unlock()

	if ts.pool == nil {
		ts.pool = tracking.NewCollectorPool(16)
		ts.active = make(map[uint64]tracking.Collector, 16)
	}

	var c tracking.Collector
	if ts.Config.IsComposite {
		c = ts.pool.AcquireComposite()
	} else {
		c = ts.pool.AcquireStandard()
	}
	ts.active[evalID] = c
	return c
}

func (ts *TrackedSpecies) CollectMetrics(evalID uint64, m tracking.MetricBundle, dt time.Duration) {
	ts.trackMu.Lock()
	c := ts.active[evalID]
	ts.trackMu.Unlock()

	if c != nil {
		c.Collect(m, dt)
	}
}

// CompleteTracking finalizes collection and reports aggregated fitness.
// A nil aggregator abandons the evaluation rather than scoring it zero
func (ts *TrackedSpecies) CompleteTracking(evalID uint64, death tracking.MetricBundle, ctx fitness.Context) {
	ts.trackMu.Lock()
	c, ok := ts.active[evalID]
	delete(ts.active, evalID)
	ts.trackMu.Unlock()

	if !ok {
		return
	}

	metrics := c.Finalize(death)
	if ts.Aggregator == nil {
		ts.Engine.AbandonEvaluation(genetic.EvalID(evalID))
	} else {
		ts.Engine.CompleteEvaluation(genetic.EvalID(evalID), ts.Aggregator.Calculate(metrics, ctx))
	}

	ts.trackMu.Lock()
	switch v := c.(type) {
	case *tracking.CompositeCollector:
		ts.pool.ReleaseComposite(v)
	case *tracking.StandardCollector:
		ts.pool.ReleaseStandard(v)
	}
	ts.trackMu.Unlock()
}

func (ts *TrackedSpecies) Stats() Stats {
	s := ts.Engine.Stats()
	return Stats{
		Generation:   s.Generation,
		BestFitness:  s.BestScore,
		WorstFitness: s.WorstScore,
		AvgFitness:   s.AverageScore,
		Diversity:    s.Diversity,
		PoolSize:     s.Size,
		PendingCount: s.Pending,
		TotalEvals:   s.Evaluations,
		Evicted:      s.Evicted,
	}
}
