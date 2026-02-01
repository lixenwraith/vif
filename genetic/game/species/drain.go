package species

import (
	"github.com/lixenwraith/vi-fighter/genetic"
	"github.com/lixenwraith/vi-fighter/genetic/fitness"
	"github.com/lixenwraith/vi-fighter/genetic/registry"
	"github.com/lixenwraith/vi-fighter/genetic/tracking"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// DrainSpeciesID is the unique identifier for drain entities
const DrainSpeciesID registry.SpeciesID = 1

// Genotype indices
const (
	DrainGeneHomingAccel = iota
	DrainGeneAggressionMult
	DrainGeneCount
)

// DrainBounds defines evolution parameter ranges
var DrainBounds = []genetic.ParameterBounds{
	{Min: parameter.GADrainHomingAccelMin, Max: parameter.GADrainHomingAccelMax},
	{Min: parameter.GADrainAggressionMin, Max: parameter.GADrainAggressionMax},
}

// DrainConfig is the species configuration for drain entities
var DrainConfig = registry.SpeciesConfig{
	ID:                 DrainSpeciesID,
	Name:               "drain",
	GeneCount:          DrainGeneCount,
	Bounds:             DrainBounds,
	PerturbationStdDev: parameter.GADrainPerturbationStdDev,
	IsComposite:        false,
}

// DrainPhenotype is the decoded phenotype for drain entities
type DrainPhenotype struct {
	HomingAccel    int64 // Q32.32
	AggressionMult int64 // Q32.32
}

// DecodeDrain converts genotype to phenotype
func DecodeDrain(genes []float64) any {
	if len(genes) < DrainGeneCount {
		return DrainPhenotype{
			HomingAccel:    parameter.DrainHomingAccel,
			AggressionMult: vmath.Scale,
		}
	}
	return DrainPhenotype{
		HomingAccel:    vmath.FromFloat(genes[DrainGeneHomingAccel]),
		AggressionMult: vmath.FromFloat(genes[DrainGeneAggressionMult]),
	}
}

// Fitness metric keys for drain
const (
	DrainMetricInShield    = "in_shield"
	DrainMetricDistanceSq  = "distance_sq"
	DrainMetricPositioning = "avg_" + DrainMetricDistanceSq
)

// DrainFitnessWeights defines scoring coefficients
var DrainFitnessWeights = map[string]float64{
	"time_" + DrainMetricInShield: parameter.GADrainFitnessWeightEnergyDrain,
	tracking.MetricTicksAlive:     parameter.GADrainFitnessWeightSurvival,
	DrainMetricPositioning:        parameter.GADrainFitnessWeightPositioning,
	tracking.MetricDeathAtTarget:  -parameter.GADrainFitnessWeightHeatPenalty,
}

// DrainNormalizers for fitness calculation
var DrainNormalizers = map[string]fitness.NormalizeFunc{
	"time_" + DrainMetricInShield: fitness.NormalizeCap(30.0), // 30s = excellent
	tracking.MetricTicksAlive:     fitness.NormalizeCap(parameter.GAFitnessMaxTicksDefault),
	DrainMetricPositioning:        fitness.NormalizeInverse(100.0), // Closer = better
	tracking.MetricDeathAtTarget:  nil,                             // Raw 0/1
}

// NewDrainAggregator creates the fitness aggregator for drains
func NewDrainAggregator() fitness.Aggregator {
	return &fitness.WeightedAggregator{
		Weights:     DrainFitnessWeights,
		Normalizers: DrainNormalizers,
		ContextAdjuster: func(w map[string]float64, ctx fitness.Context) map[string]float64 {
			if ctx == nil {
				return w
			}

			threat, ok := ctx.Get(fitness.ContextThreatLevel)
			if !ok {
				return w
			}

			adjusted := make(map[string]float64, len(w))
			for k, v := range w {
				adjusted[k] = v
			}

			// Skilled player: reward survival; weaker player: reward drain
			if threat > 0.7 {
				adjusted[tracking.MetricTicksAlive] *= 1.2
				adjusted[DrainMetricPositioning] *= 1.1
			} else if threat < 0.3 {
				adjusted["time_"+DrainMetricInShield] *= 1.3
			}

			return adjusted
		},
	}
}