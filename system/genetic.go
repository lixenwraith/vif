package system

import (
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/genetic"
	"github.com/lixenwraith/vi-fighter/genetic/fitness"
	"github.com/lixenwraith/vi-fighter/genetic/registry"
	"github.com/lixenwraith/vi-fighter/genetic/tracking"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// --- Navigation Gene Configuration ---

const (
	geneNavTurnThreshold = iota
	geneNavBrakeIntensity
	geneNavFlowLookahead
	geneNavCount
)

var navGeneBounds = []genetic.ParameterBounds{
	{Min: parameter.GADrainTurnThresholdMin, Max: parameter.GADrainTurnThresholdMax},
	{Min: parameter.GADrainBrakeIntensityMin, Max: parameter.GADrainBrakeIntensityMax},
	{Min: parameter.GADrainFlowLookaheadMin, Max: parameter.GADrainFlowLookaheadMax},
}

var navGeneDefaults = []float64{
	parameter.GADrainTurnThresholdDefault,
	parameter.GADrainBrakeIntensityDefault,
	parameter.GADrainFlowLookaheadDefault,
}

// --- Fitness Metric Keys ---

const (
	metricInShield   = "in_shield"
	metricDistanceSq = "distance_sq"
)

// --- Player Behavior Model ---

type playerModel struct {
	mu sync.RWMutex

	avgReactionTime  time.Duration
	energyManagement float64
	heatManagement   float64
	typingAccuracy   float64
	emaAlpha         float64
}

func newPlayerModel() *playerModel {
	return &playerModel{
		avgReactionTime:  500 * time.Millisecond,
		energyManagement: 0.5,
		heatManagement:   0.3,
		typingAccuracy:   0.8,
		emaAlpha:         0.1,
	}
}

func (m *playerModel) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.avgReactionTime = 500 * time.Millisecond
	m.energyManagement = 0.5
	m.heatManagement = 0.3
	m.typingAccuracy = 0.8
}

func (m *playerModel) recordEnergy(current int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	magnitude := math.Abs(float64(current))
	normalized := math.Min(magnitude/10000.0, 1.0)
	m.energyManagement = m.emaAlpha*normalized + (1-m.emaAlpha)*m.energyManagement
}

func (m *playerModel) recordHeat(current, max int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if max <= 0 {
		return
	}
	normalized := math.Max(0, math.Min(float64(current)/float64(max), 1.0))
	m.heatManagement = m.emaAlpha*normalized + (1-m.emaAlpha)*m.heatManagement
}

func (m *playerModel) threatLevel() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	reactionScore := 1.0 - float64(m.avgReactionTime)/(2*float64(time.Second))
	if reactionScore < 0 {
		reactionScore = 0
	}
	return 0.3*reactionScore + 0.3*m.typingAccuracy + 0.2*m.energyManagement + 0.2*m.heatManagement
}

func (m *playerModel) context() fitness.Context {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return fitness.MapContext{
		fitness.ContextThreatLevel:      m.threatLevel(),
		fitness.ContextEnergyManagement: m.energyManagement,
		fitness.ContextHeatManagement:   m.heatManagement,
		fitness.ContextTypingAccuracy:   m.typingAccuracy,
	}
}

// --- Tracked Entity ---

type trackedEntity struct {
	species   component.SpeciesType
	evalID    uint64
	collector *tracking.StandardCollector
}

// --- Genetic System ---

type GeneticSystem struct {
	world *engine.World

	registry      *registry.Registry
	playerModel   *playerModel
	collectorPool *tracking.CollectorPool

	tracking      map[core.Entity]*trackedEntity
	pendingDeaths []event.EnemyKilledPayload

	cursorPos                    component.PositionComponent
	cursorEnergy                 int64
	cursorHeat                   int
	shieldActive                 bool
	shieldInvRxSq, shieldInvRySq int64

	statGeneration *atomic.Int64
	statBest       *atomic.Int64
	statAvg        *atomic.Int64
	statPending    *atomic.Int64
	statOutcomes   *atomic.Int64

	enabled bool
}

func NewGeneticSystem(world *engine.World) engine.System {
	s := &GeneticSystem{
		world:         world,
		registry:      registry.NewRegistry(parameter.GeneticPersistencePath),
		playerModel:   newPlayerModel(),
		collectorPool: tracking.NewCollectorPool(32),
		tracking:      make(map[core.Entity]*trackedEntity),
		pendingDeaths: make([]event.EnemyKilledPayload, 0, 16),
	}

	s.statGeneration = world.Resources.Status.Ints.Get("ga.generation")
	s.statBest = world.Resources.Status.Ints.Get("ga.best")
	s.statAvg = world.Resources.Status.Ints.Get("ga.avg")
	s.statPending = world.Resources.Status.Ints.Get("ga.pending")
	s.statOutcomes = world.Resources.Status.Ints.Get("ga.outcomes")

	s.registerSpecies()
	s.Init()
	return s
}

func (s *GeneticSystem) registerSpecies() {
	config := registry.SpeciesConfig{
		ID:                 registry.SpeciesID(component.SpeciesDrain),
		Name:               "navigation",
		GeneCount:          geneNavCount,
		Bounds:             navGeneBounds,
		PerturbationStdDev: parameter.GADrainPerturbationStdDev,
		IsComposite:        false,
	}
	_ = s.registry.Register(config, s.createAggregator())
}

func (s *GeneticSystem) createAggregator() fitness.Aggregator {
	return &fitness.WeightedAggregator{
		Weights: map[string]float64{
			"time_" + metricInShield:     parameter.GADrainFitnessWeightEnergyDrain,
			tracking.MetricTicksAlive:    parameter.GADrainFitnessWeightSurvival,
			"avg_" + metricDistanceSq:    parameter.GADrainFitnessWeightPositioning,
			tracking.MetricDeathAtTarget: -parameter.GADrainFitnessWeightHeatPenalty,
		},
		Normalizers: map[string]fitness.NormalizeFunc{
			"time_" + metricInShield:  fitness.NormalizeCap(30.0),
			tracking.MetricTicksAlive: fitness.NormalizeCap(parameter.GAFitnessMaxTicksDefault),
			"avg_" + metricDistanceSq: fitness.NormalizeInverse(100.0),
		},
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
				adjusted["avg_"+metricDistanceSq] *= 1.1
			} else if threat < 0.3 {
				adjusted["time_"+metricInShield] *= 1.3
			}
			return adjusted
		},
	}
}

func (s *GeneticSystem) Init() {
	clear(s.tracking)
	s.pendingDeaths = s.pendingDeaths[:0]
	s.playerModel.reset()
	s.enabled = true
	_ = s.registry.Start()
}

func (s *GeneticSystem) Name() string {
	return "genetic"
}

func (s *GeneticSystem) Priority() int {
	return parameter.PriorityGenetic
}

func (s *GeneticSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventGameReset,
		event.EventMetaSystemCommandRequest,
		event.EventEnemyCreated,
		event.EventEnemyKilled,
	}
}

func (s *GeneticSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
		return
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventEnemyCreated:
		if payload, ok := ev.Payload.(*event.EnemyCreatedPayload); ok {
			s.handleEnemyCreated(payload.Entity, payload.Species)
		}

	case event.EventEnemyKilled:
		if payload, ok := ev.Payload.(*event.EnemyKilledPayload); ok {
			s.pendingDeaths = append(s.pendingDeaths, *payload)
		}
	}
}

func (s *GeneticSystem) handleEnemyCreated(entity core.Entity, speciesType component.SpeciesType) {
	if _, exists := s.tracking[entity]; exists {
		return
	}

	genes, evalID := s.registry.Sample(registry.SpeciesID(speciesType))
	if evalID == 0 {
		return
	}

	// Get species dimensions for area LOS
	if speciesType >= component.SpeciesCount {
		speciesType = component.SpeciesNone
	}
	dims := component.SpeciesDimensionsLUT[speciesType]

	// Apply genes and dimensions to NavigationComponent
	if navComp, hasNav := s.world.Components.Navigation.GetComponent(entity); hasNav {
		navComp.Width = dims.Width
		navComp.Height = dims.Height

		if len(genes) >= geneNavCount {
			navComp.TurnThreshold = vmath.FromFloat(genes[geneNavTurnThreshold])
			navComp.BrakeIntensity = vmath.FromFloat(genes[geneNavBrakeIntensity])
			navComp.FlowLookahead = vmath.FromFloat(genes[geneNavFlowLookahead])
		} else {
			navComp.TurnThreshold = vmath.FromFloat(navGeneDefaults[geneNavTurnThreshold])
			navComp.BrakeIntensity = vmath.FromFloat(navGeneDefaults[geneNavBrakeIntensity])
			navComp.FlowLookahead = vmath.FromFloat(navGeneDefaults[geneNavFlowLookahead])
		}
		s.world.Components.Navigation.SetComponent(entity, navComp)
	}

	collector := s.collectorPool.AcquireStandard()
	s.tracking[entity] = &trackedEntity{
		species:   speciesType,
		evalID:    evalID,
		collector: collector,
	}
}

func (s *GeneticSystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime
	s.updateCursorState()
	s.updatePlayerModel()
	s.processPendingDeaths()
	s.cleanupStaleTracking()
	s.processTracking(dt)
	s.updateTelemetry()
}

func (s *GeneticSystem) updateCursorState() {
	cursorEntity := s.world.Resources.Player.Entity

	if pos, ok := s.world.Positions.GetPosition(cursorEntity); ok {
		s.cursorPos = pos
	}

	if energy, ok := s.world.Components.Energy.GetComponent(cursorEntity); ok {
		s.cursorEnergy = energy.Current
	}

	if heat, ok := s.world.Components.Heat.GetComponent(cursorEntity); ok {
		s.cursorHeat = heat.Current
	}

	if shield, ok := s.world.Components.Shield.GetComponent(cursorEntity); ok {
		s.shieldActive = shield.Active
		s.shieldInvRxSq = shield.InvRxSq
		s.shieldInvRySq = shield.InvRySq
	} else {
		s.shieldActive = false
	}
}

func (s *GeneticSystem) updatePlayerModel() {
	s.playerModel.recordEnergy(s.cursorEnergy)
	s.playerModel.recordHeat(s.cursorHeat, parameter.HeatMax)
}

func (s *GeneticSystem) processPendingDeaths() {
	for _, death := range s.pendingDeaths {
		tracked, ok := s.tracking[death.Entity]
		if !ok {
			continue
		}

		deathAtCursor := death.X == s.cursorPos.X && death.Y == s.cursorPos.Y
		s.completeTracking(tracked, deathAtCursor)
		delete(s.tracking, death.Entity)
	}
	s.pendingDeaths = s.pendingDeaths[:0]
}

// cleanupStaleTracking ends tracking of entities that no longer exist (OOB, resize, level change)
func (s *GeneticSystem) cleanupStaleTracking() {
	for entity, tracked := range s.tracking {
		if !s.world.Components.Navigation.HasEntity(entity) {
			// Complete with neutral fitness — no death position available
			s.completeTracking(tracked, false)
			delete(s.tracking, entity)
		}
	}
}

func (s *GeneticSystem) processTracking(dt time.Duration) {
	for entity, tracked := range s.tracking {
		pos, ok := s.world.Positions.GetPosition(entity)
		if !ok {
			continue
		}

		dx := float64(pos.X - s.cursorPos.X)
		dy := float64(pos.Y - s.cursorPos.Y)

		insideShield := false
		if s.shieldActive {
			insideShield = vmath.EllipseContainsPoint(
				pos.X, pos.Y,
				s.cursorPos.X, s.cursorPos.Y,
				s.shieldInvRxSq, s.shieldInvRySq,
			)
		}

		metrics := tracking.MetricBundle{
			metricDistanceSq: dx*dx + dy*dy,
			metricInShield:   boolToFloat(insideShield),
		}

		tracked.collector.Collect(metrics, dt)
	}
}

func (s *GeneticSystem) completeTracking(tracked *trackedEntity, deathAtCursor bool) {
	deathCondition := tracking.MetricBundle{
		tracking.MetricDeathAtTarget: boolToFloat(deathAtCursor),
	}

	snapshot := tracked.collector.Finalize(deathCondition)
	ctx := s.playerModel.context()

	ts := s.registry.GetTracker(registry.SpeciesID(tracked.species))
	if ts != nil && ts.Aggregator != nil {
		fitnessVal := ts.Aggregator.Calculate(snapshot, ctx)
		s.registry.ReportFitness(registry.SpeciesID(tracked.species), tracked.evalID, fitnessVal)
	}

	s.collectorPool.ReleaseStandard(tracked.collector)
}

func (s *GeneticSystem) updateTelemetry() {
	stats := s.registry.Stats(registry.SpeciesID(component.SpeciesDrain))
	s.statGeneration.Store(int64(stats.Generation))
	s.statBest.Store(int64(stats.BestFitness * 1000))
	s.statAvg.Store(int64(stats.AvgFitness * 1000))
	s.statPending.Store(int64(stats.PendingCount))
	s.statOutcomes.Store(int64(stats.TotalEvals))
}

func boolToFloat(b bool) float64 {
	if b {
		return 1.0
	}
	return 0.0
}