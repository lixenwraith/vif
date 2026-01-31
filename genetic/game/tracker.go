package game

import (
	"sync"
	"time"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/genetic"
)

type EntityTracker struct {
	mu       sync.RWMutex
	entities map[core.Entity]*TrackedEntity

	engine     *genetic.StreamingEngine[[]float64, float64]
	aggregator *CombatFitnessAggregator
}

type TrackedEntity struct {
	EvalID           genetic.EvalID
	Genotype         []float64
	SpawnTime        time.Time
	EnergyDrained    int64
	HeatReduced      int
	TicksAlive       int
	CumulativeDistSq float64
	DistSamples      int
	TimeInShield     time.Duration
	CoordCharges     int
}

func NewEntityTracker(
	engine *genetic.StreamingEngine[[]float64, float64],
	aggregator *CombatFitnessAggregator,
) *EntityTracker {
	return &EntityTracker{
		entities:   make(map[core.Entity]*TrackedEntity),
		engine:     engine,
		aggregator: aggregator,
	}
}

func (t *EntityTracker) BeginTracking(entity core.Entity, genotype []float64, spawnTime time.Time) {
	evalID := t.engine.BeginEvaluation(genotype)

	t.mu.Lock()
	t.entities[entity] = &TrackedEntity{
		EvalID:    evalID,
		Genotype:  genotype,
		SpawnTime: spawnTime,
	}
	t.mu.Unlock()
}

func (t *EntityTracker) RecordTick(entity core.Entity, distToCursor float64, inShield bool, dt time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	tracked, ok := t.entities[entity]
	if !ok {
		return
	}

	tracked.TicksAlive++
	tracked.CumulativeDistSq += distToCursor * distToCursor
	tracked.DistSamples++

	if inShield {
		tracked.TimeInShield += dt
	}
}

func (t *EntityTracker) RecordEnergyDrain(entity core.Entity, amount int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if tracked, ok := t.entities[entity]; ok {
		tracked.EnergyDrained += amount
	}
}

func (t *EntityTracker) RecordHeatChange(entity core.Entity, delta int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if tracked, ok := t.entities[entity]; ok {
		tracked.HeatReduced += delta
	}
}

func (t *EntityTracker) RecordCoordinatedCharge(entity core.Entity) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if tracked, ok := t.entities[entity]; ok {
		tracked.CoordCharges++
	}
}

func (t *EntityTracker) EndTracking(entity core.Entity, deathTime time.Time) {
	t.mu.Lock()
	tracked, ok := t.entities[entity]
	if !ok {
		t.mu.Unlock()
		return
	}
	delete(t.entities, entity)
	t.mu.Unlock()

	avgDist := 0.0
	if tracked.DistSamples > 0 {
		avgDist = tracked.CumulativeDistSq / float64(tracked.DistSamples)
	}

	outcome := CombatOutcome{
		EntityID:           entity,
		EvalID:             tracked.EvalID,
		SpawnTime:          tracked.SpawnTime,
		DeathTime:          deathTime,
		EnergyDrained:      tracked.EnergyDrained,
		HeatReduced:        tracked.HeatReduced,
		TicksAlive:         tracked.TicksAlive,
		TimeInShield:       tracked.TimeInShield,
		AvgDistToCursor:    avgDist,
		CoordinatedCharges: tracked.CoordCharges,
	}

	fitness := t.aggregator.Calculate(outcome)
	t.engine.CompleteEvaluation(tracked.EvalID, fitness)
}

func (t *EntityTracker) Clear() {
	t.mu.Lock()
	t.entities = make(map[core.Entity]*TrackedEntity)
	t.mu.Unlock()
}