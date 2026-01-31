package game

import (
	"sync"
	"time"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/genetic"
	"github.com/lixenwraith/vi-fighter/parameter"
)

type CombatOutcome struct {
	EntityID           core.Entity
	EvalID             genetic.EvalID
	SpawnTime          time.Time
	DeathTime          time.Time
	EnergyDrained      int64
	HeatReduced        int
	TicksAlive         int
	TimeInShield       time.Duration
	AvgDistToCursor    float64
	CoordinatedCharges int
}

type FitnessWeights struct {
	EnergyDrain  float64
	Survival     float64
	Positioning  float64
	Coordination float64
	HeatPenalty  float64
}

var DefaultDrainWeights = FitnessWeights{
	EnergyDrain:  parameter.GADrainFitnessWeightEnergyDrain,
	Survival:     parameter.GADrainFitnessWeightSurvival,
	Positioning:  parameter.GADrainFitnessWeightPositioning,
	Coordination: parameter.GADrainFitnessWeightCoordination,
	HeatPenalty:  parameter.GADrainFitnessWeightHeatPenalty,
}

var DefaultSwarmWeights = FitnessWeights{
	EnergyDrain:  0.35,
	Survival:     0.25,
	Positioning:  0.15,
	Coordination: 0.15,
	HeatPenalty:  0.1,
}

type CombatFitnessAggregator struct {
	weights FitnessWeights

	mu          sync.RWMutex
	maxEnergy   int64
	maxTicks    int
	sampleCount int
}

func NewCombatFitnessAggregator(weights FitnessWeights) *CombatFitnessAggregator {
	return &CombatFitnessAggregator{
		weights:   weights,
		maxEnergy: parameter.GAFitnessMaxEnergyDefault,
		maxTicks:  parameter.GAFitnessMaxTicksDefault,
	}
}

func (a *CombatFitnessAggregator) Calculate(outcome CombatOutcome) float64 {
	a.mu.RLock()
	maxE := a.maxEnergy
	maxT := a.maxTicks
	a.mu.RUnlock()

	energyScore := float64(outcome.EnergyDrained) / float64(maxE)
	if energyScore > 1.0 {
		energyScore = 1.0
	}

	survivalScore := float64(outcome.TicksAlive) / float64(maxT)
	if survivalScore > 1.0 {
		survivalScore = 1.0
	}

	posScore := 1.0 / (1.0 + outcome.AvgDistToCursor/10.0)

	coordScore := float64(outcome.CoordinatedCharges) / 5.0
	if coordScore > 1.0 {
		coordScore = 1.0
	}

	heatPenalty := float64(outcome.HeatReduced) / 100.0

	fitness := a.weights.EnergyDrain*energyScore +
		a.weights.Survival*survivalScore +
		a.weights.Positioning*posScore +
		a.weights.Coordination*coordScore -
		a.weights.HeatPenalty*heatPenalty

	a.updateNormalization(outcome)

	return fitness
}

func (a *CombatFitnessAggregator) updateNormalization(outcome CombatOutcome) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.sampleCount++

	if outcome.EnergyDrained > a.maxEnergy {
		a.maxEnergy = outcome.EnergyDrained
	}
	if outcome.TicksAlive > a.maxTicks {
		a.maxTicks = outcome.TicksAlive
	}
}