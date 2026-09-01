package registry

import (
	"math/rand/v2"
	"sync"
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

	probeMu      sync.Mutex
	probeSource  *rand.PCG
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
	probeSource := rand.NewPCG(engineCfg.Seed^0xA5A5A5A5, engineCfg.Seed)

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
		// Probe stream derives from the engine seed so both replay together
		probeSource: probeSource,
		probeRng:    rand.New(probeSource),
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
}

func (ts *TrackedSpecies) Stop() {
	ts.Engine.Stop()
}

func (ts *TrackedSpecies) Started() bool { return ts.Engine.Running() }

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
	if n == 0 || len(ts.Config.Bounds) == 0 || !ts.Engine.Running() {
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
	id := ts.Engine.BeginEvaluation(g)
	ts.probeMu.Unlock()

	return g, uint64(id)
}

// checkpoint takes the probe lock before the engine lock, the same order
// SampleScout uses, so a probe and the pending evaluation it creates appear
// together or not at all.
func (ts *TrackedSpecies) checkpoint() (SpeciesState, error) {
	ts.probeMu.Lock()
	defer ts.probeMu.Unlock()

	engineState, err := ts.Engine.Checkpoint()
	if err != nil {
		return SpeciesState{}, err
	}
	probeState, err := ts.probeSource.MarshalBinary()
	if err != nil {
		return SpeciesState{}, err
	}
	return SpeciesState{
		Engine:       engineState,
		ProbeRNG:     probeState,
		ProbeCounter: ts.probeCounter,
	}, nil
}

func (ts *TrackedSpecies) validateState(state SpeciesState) (*rand.PCG, error) {
	if err := ts.Engine.ValidateState(state.Engine); err != nil {
		return nil, err
	}
	source := rand.NewPCG(0, 0)
	if err := source.UnmarshalBinary(state.ProbeRNG); err != nil {
		return nil, err
	}
	return source, nil
}

func (ts *TrackedSpecies) restore(state SpeciesState, probeSource *rand.PCG) error {
	ts.probeMu.Lock()
	defer ts.probeMu.Unlock()

	if err := ts.Engine.Restore(state.Engine); err != nil {
		return err
	}
	ts.probeSource = probeSource
	ts.probeRng = rand.New(probeSource)
	ts.probeCounter = state.ProbeCounter
	return nil
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
