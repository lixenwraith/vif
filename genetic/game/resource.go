package game

import (
	"math/rand/v2"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/genetic"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// GeneticResource holds all GA engines
type GeneticResource struct {
	drainEngine     *genetic.StreamingEngine[[]float64, float64]
	drainCodec      *DrainCodec
	drainAggregator *CombatFitnessAggregator
}

// NewGeneticResource creates and initializes GA components
func NewGeneticResource() *GeneticResource {
	drainCodec := NewDrainCodec()
	drainAggregator := NewCombatFitnessAggregator(DefaultDrainWeights)

	drainInitializer := func(rng *rand.Rand) []float64 {
		g := make([]float64, DrainGeneCount)
		for i, b := range DrainBounds {
			g[i] = b.Min + rng.Float64()*(b.Max-b.Min)
		}
		return g
	}

	drainPerturbator := &genetic.BoundedPerturbator{
		Bounds:            DrainBounds,
		StandardDeviation: parameter.GADrainPerturbationStdDev,
	}

	drainSelector := &genetic.TournamentSelector[[]float64, float64]{
		TournamentSize:  parameter.GATournamentSize,
		WithReplacement: true,
	}

	drainCombiner := &genetic.UniformCombiner[[]float64, float64, float64]{
		MixProbability: parameter.GACrossoverMixProbability,
	}

	config := genetic.DefaultStreamingConfig()

	drainEngine := genetic.NewStreamingEngine(
		drainInitializer,
		drainSelector,
		drainCombiner,
		drainPerturbator,
		config,
	)

	r := &GeneticResource{
		drainEngine:     drainEngine,
		drainCodec:      drainCodec,
		drainAggregator: drainAggregator,
	}

	return r
}

func (r *GeneticResource) Start() {
	r.drainEngine.Start()
}

func (r *GeneticResource) Stop() {
	r.drainEngine.Stop()
}

func (r *GeneticResource) Reset() {
	// Population retained, pending cleared by engine
}

func (r *GeneticResource) Sample(species component.SpeciesType) ([]float64, uint64) {
	switch species {
	case component.SpeciesDrain:
		samples := r.drainEngine.SamplePopulation(1)
		if len(samples) == 0 {
			// Fallback default
			samples = [][]float64{{3.0, 0.0, 1.0}}
		}
		evalID := r.drainEngine.BeginEvaluation(samples[0])
		return samples[0], uint64(evalID)

	case component.SpeciesSwarm, component.SpeciesQuasar:
		// Future implementation
		return nil, 0
	}
	return nil, 0
}

func (r *GeneticResource) Decode(species component.SpeciesType, genotype []float64) component.DecodedPhenotype {
	switch species {
	case component.SpeciesDrain:
		p := r.drainCodec.Decode(genotype)
		return component.DecodedPhenotype{
			HomingAccel:    p.HomingAccel,
			AggressionMult: p.AggressionMult,
		}

	case component.SpeciesSwarm, component.SpeciesQuasar:
		// Future
		return component.DecodedPhenotype{}
	}
	return component.DecodedPhenotype{}
}

func (r *GeneticResource) Complete(species component.SpeciesType, evalID uint64, fitness float64) {
	switch species {
	case component.SpeciesDrain:
		r.drainEngine.CompleteEvaluation(genetic.EvalID(evalID), fitness)

	case component.SpeciesSwarm, component.SpeciesQuasar:
		// Future
	}
}

func (r *GeneticResource) Stats(species component.SpeciesType) component.GeneticStats {
	switch species {
	case component.SpeciesDrain:
		best, worst, avg, _ := r.drainEngine.PoolStats()
		return component.GeneticStats{
			Generation:    r.drainEngine.Generation(),
			Best:          best,
			Worst:         worst,
			Avg:           avg,
			PendingCount:  r.drainEngine.PendingCount(),
			OutcomesTotal: r.drainEngine.EvaluationsStarted(),
		}

	case component.SpeciesSwarm, component.SpeciesQuasar:
		return component.GeneticStats{}
	}
	return component.GeneticStats{}
}