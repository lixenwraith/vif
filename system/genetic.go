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

// --- Eye Gene Configuration ---

const (
	geneEyeFlowLookahead = iota
	geneEyeCount
)

var eyeGeneBounds = []genetic.ParameterBounds{
	{Min: parameter.GAEyeFlowLookaheadMin, Max: parameter.GAEyeFlowLookaheadMax},
}

// --- Fitness Metric Keys ---

const (
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
	species       component.SpeciesType
	evalID        uint64
	collector     tracking.Collector
	isComposite   bool
	targetGroupID uint8 // Target group for distance measurement (0 = cursor)
}

// --- Genetic System ---

type GeneticSystem struct {
	world *engine.World

	registry      *registry.Registry
	playerModel   *playerModel
	collectorPool *tracking.CollectorPool

	tracking      map[core.Entity]*trackedEntity
	pendingDeaths []event.EnemyKilledPayload

	// Eye telemetry
	statEyeGeneration *atomic.Int64
	statEyeBest       *atomic.Int64
	statEyeAvg        *atomic.Int64
	statEyePending    *atomic.Int64
	statEyeOutcomes   *atomic.Int64
	statEyeTracked    *atomic.Int64

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

	// Eye telemetry
	s.statEyeGeneration = world.Resources.Status.Ints.Get("eye.ga.generation")
	s.statEyeBest = world.Resources.Status.Ints.Get("eye.ga.best")
	s.statEyeAvg = world.Resources.Status.Ints.Get("eye.ga.avg")
	s.statEyePending = world.Resources.Status.Ints.Get("eye.ga.pending")
	s.statEyeOutcomes = world.Resources.Status.Ints.Get("eye.ga.outcomes")
	s.statEyeTracked = world.Resources.Status.Ints.Get("eye.ga.tracked")

	s.registerSpecies()
	s.Init()
	return s
}

func (s *GeneticSystem) registerSpecies() {
	// Eye species — group-targeting composite entities with path diversity genes
	eyeConfig := registry.SpeciesConfig{
		ID:                 registry.SpeciesID(component.SpeciesEye),
		Name:               "eye_navigation",
		GeneCount:          geneEyeCount,
		Bounds:             eyeGeneBounds,
		PerturbationStdDev: parameter.GAEyePerturbationStdDev,
		IsComposite:        true,
	}
	_ = s.registry.Register(eyeConfig, s.createEyeAggregator())
}

func (s *GeneticSystem) createEyeAggregator() fitness.Aggregator {
	return &fitness.WeightedAggregator{
		Weights: map[string]float64{
			tracking.MetricDeathAtTarget: parameter.GAEyeFitnessWeightReachedTarget,
			tracking.MetricTicksAlive:    parameter.GAEyeFitnessWeightSpeed,
			"avg_" + metricDistanceSq:    parameter.GAEyeFitnessWeightPositioning,
		},
		Normalizers: map[string]fitness.NormalizeFunc{
			// Inverse: fewer ticks = higher fitness (eye should arrive fast)
			tracking.MetricTicksAlive: fitness.NormalizeInverse(float64(parameter.GAEyeFitnessMaxTicks)),
			// Inverse: closer average distance = higher fitness
			"avg_" + metricDistanceSq: fitness.NormalizeInverse(200.0),
			// MetricDeathAtTarget: no normalizer needed (already 0 or 1)
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

		switch speciesType {
		case component.SpeciesEye:
			if len(genes) >= geneEyeCount {
				navComp.FlowLookahead = vmath.FromFloat(genes[geneEyeFlowLookahead])
			}
		}

		s.world.Components.Navigation.SetComponent(entity, navComp)
	}

	// Resolve target group for distance tracking
	groupID := uint8(0)
	if tc, ok := s.world.Components.Target.GetComponent(entity); ok {
		groupID = tc.GroupID
	}

	isComposite := s.world.Components.Header.HasEntity(entity)

	var collector tracking.Collector
	if isComposite {
		collector = s.collectorPool.AcquireComposite()
	} else {
		collector = s.collectorPool.AcquireStandard()
	}

	s.tracking[entity] = &trackedEntity{
		species:       speciesType,
		evalID:        evalID,
		collector:     collector,
		isComposite:   isComposite,
		targetGroupID: groupID,
	}
}

func (s *GeneticSystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime
	s.processPendingDeaths()
	s.cleanupStaleTracking()
	s.processTracking(dt)
	s.updateTelemetry()
}

func (s *GeneticSystem) processPendingDeaths() {
	for _, death := range s.pendingDeaths {
		tracked, ok := s.tracking[death.Entity]
		if !ok {
			continue
		}

		// Target-reach detection: proximity-based for group targets, exact for cursor
		deathAtTarget := false
		groupState := s.world.Resources.Target.GetGroup(tracked.targetGroupID)
		if groupState.Valid {
			dx := death.X - groupState.PosX
			dy := death.Y - groupState.PosY
			deathAtTarget = dx*dx+dy*dy <= parameter.GAEyeReachedTargetDistSq
		}

		s.completeTracking(tracked, deathAtTarget)
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

		// Resolve target position from entity's target group
		groupState := s.world.Resources.Target.GetGroup(tracked.targetGroupID)
		if !groupState.Valid {
			continue
		}
		targetX, targetY := groupState.PosX, groupState.PosY

		dx := float64(pos.X - targetX)
		dy := float64(pos.Y - targetY)

		metrics := tracking.MetricBundle{
			metricDistanceSq: dx*dx + dy*dy,
		}

		// Composite entities: track live member count from HeaderComponent
		if tracked.isComposite {
			if header, ok := s.world.Components.Header.GetComponent(entity); ok {
				liveMembers := 0
				for _, m := range header.MemberEntries {
					if m.Entity != 0 {
						liveMembers++
					}
				}
				metrics[tracking.MetricMemberCount] = float64(liveMembers)
			}
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

	// Release collector to correct pool by type
	if tracked.isComposite {
		if c, ok := tracked.collector.(*tracking.CompositeCollector); ok {
			s.collectorPool.ReleaseComposite(c)
		}
	} else {
		if c, ok := tracked.collector.(*tracking.StandardCollector); ok {
			s.collectorPool.ReleaseStandard(c)
		}
	}
}

func (s *GeneticSystem) updateTelemetry() {
	// Eye stats
	eyeStats := s.registry.Stats(registry.SpeciesID(component.SpeciesEye))
	s.statEyeGeneration.Store(int64(eyeStats.Generation))
	s.statEyeBest.Store(int64(eyeStats.BestFitness * 1000))
	s.statEyeAvg.Store(int64(eyeStats.AvgFitness * 1000))
	s.statEyePending.Store(int64(eyeStats.PendingCount))
	s.statEyeOutcomes.Store(int64(eyeStats.TotalEvals))

	// Eye tracked count
	eyeTracked := int64(0)
	for _, t := range s.tracking {
		if t.species == component.SpeciesEye {
			eyeTracked++
		}
	}
	s.statEyeTracked.Store(eyeTracked)
}

func boolToFloat(b bool) float64 {
	if b {
		return 1.0
	}
	return 0.0
}