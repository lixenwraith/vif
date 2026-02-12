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
	DrainGeneTurnThreshold = iota
	DrainGeneBrakeIntensity
	DrainGeneFlowLookahead
	DrainGeneCount
)

// DrainBounds defines evolution parameter ranges
var DrainBounds = []genetic.ParameterBounds{
	{Min: parameter.GADrainTurnThresholdMin, Max: parameter.GADrainTurnThresholdMax},
	{Min: parameter.GADrainBrakeIntensityMin, Max: parameter.GADrainBrakeIntensityMax},
	{Min: parameter.GADrainFlowLookaheadMin, Max: parameter.GADrainFlowLookaheadMax},
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
	TurnThreshold  int64 // Q32.32, alignment below which cornering drag activates
	BrakeIntensity int64 // Q32.32, drag multiplier during turns
	FlowLookahead  int64 // Q32.32, field projection cells
}

// DecodeDrain converts genotype to phenotype
func DecodeDrain(genes []float64) any {
	if len(genes) < DrainGeneCount {
		return DrainPhenotype{
			TurnThreshold:  vmath.FromFloat(parameter.GADrainTurnThresholdDefault),
			BrakeIntensity: vmath.FromFloat(parameter.GADrainBrakeIntensityDefault),
			FlowLookahead:  vmath.FromFloat(parameter.GADrainFlowLookaheadDefault),
		}
	}
	return DrainPhenotype{
		TurnThreshold:  vmath.FromFloat(genes[DrainGeneTurnThreshold]),
		BrakeIntensity: vmath.FromFloat(genes[DrainGeneBrakeIntensity]),
		FlowLookahead:  vmath.FromFloat(genes[DrainGeneFlowLookahead]),
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
	"time_" + DrainMetricInShield: fitness.NormalizeCap(30.0),
	tracking.MetricTicksAlive:     fitness.NormalizeCap(parameter.GAFitnessMaxTicksDefault),
	DrainMetricPositioning:        fitness.NormalizeInverse(100.0),
	tracking.MetricDeathAtTarget:  nil,
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