package game

import (
	"github.com/lixenwraith/vi-fighter/parameter"
)

// CombatOutcome contains metrics for fitness calculation
type CombatOutcome struct {
	TicksAlive       int
	CumulativeDistSq float64
	DistSamples      int
	TimeInShield     float64 // Seconds
	DeathAtCursor    bool
}

// FitnessWeights defines scoring coefficients
type FitnessWeights struct {
	EnergyDrain float64
	Survival    float64
	Positioning float64
	HeatPenalty float64
}

var DefaultDrainWeights = FitnessWeights{
	EnergyDrain: parameter.GADrainFitnessWeightEnergyDrain,
	Survival:    parameter.GADrainFitnessWeightSurvival,
	Positioning: parameter.GADrainFitnessWeightPositioning,
	HeatPenalty: parameter.GADrainFitnessWeightHeatPenalty,
}

// CombatFitnessAggregator calculates fitness from combat outcomes
type CombatFitnessAggregator struct {
	weights FitnessWeights
}

func NewCombatFitnessAggregator(weights FitnessWeights) *CombatFitnessAggregator {
	return &CombatFitnessAggregator{weights: weights}
}

func (a *CombatFitnessAggregator) Calculate(outcome CombatOutcome) float64 {
	// Fixed normalization constants
	maxEnergy := float64(parameter.GAFitnessMaxEnergyDefault)
	maxTicks := float64(parameter.GAFitnessMaxTicksDefault)

	// Energy score: time in shield × drain rate (approximation)
	energyDrained := outcome.TimeInShield * float64(parameter.DrainShieldEnergyDrainAmount)
	energyScore := energyDrained / maxEnergy
	if energyScore > 1.0 {
		energyScore = 1.0
	}

	// Survival score
	survivalScore := float64(outcome.TicksAlive) / maxTicks
	if survivalScore > 1.0 {
		survivalScore = 1.0
	}

	// Positioning score (inverse of average distance)
	avgDistSq := 0.0
	if outcome.DistSamples > 0 {
		avgDistSq = outcome.CumulativeDistSq / float64(outcome.DistSamples)
	}
	posScore := 1.0 / (1.0 + avgDistSq/100.0)

	// Heat penalty (death at cursor = bad)
	heatPenalty := 0.0
	if outcome.DeathAtCursor {
		heatPenalty = 1.0
	}

	fitness := a.weights.EnergyDrain*energyScore +
		a.weights.Survival*survivalScore +
		a.weights.Positioning*posScore -
		a.weights.HeatPenalty*heatPenalty

	return fitness
}