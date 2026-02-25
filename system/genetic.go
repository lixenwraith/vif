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
	"github.com/lixenwraith/vi-fighter/genetic/fitness"
	"github.com/lixenwraith/vi-fighter/genetic/registry"
	"github.com/lixenwraith/vi-fighter/genetic/tracking"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// --- Species Configuration ---
// Currently registered as a generic tracked species with no active genes
// Future: route-selection gene per gateway population
//   - Gene encodes path index from navigation-computed route set
//   - Per-gateway populations replace single global species registration
//   - Initial weight distribution: inverse route distance, minimum floor guaranteed
//   - Fitness: distance-to-target at death (already implemented below)

// --- Player Behavior Model ---
// Tracks player metrics via exponential moving average
// Retained for future adaptive difficulty; currently supplies context to fitness pipeline

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
	// Future: gatewayID for per-gateway population isolation
}

// --- Genetic System ---

type GeneticSystem struct {
	world *engine.World

	registry      *registry.Registry
	playerModel   *playerModel
	collectorPool *tracking.CollectorPool

	tracking      map[core.Entity]*trackedEntity
	pendingDeaths []event.EnemyKilledPayload

	// Telemetry (generic species, currently mapped to eye)
	statGeneration *atomic.Int64
	statBest       *atomic.Int64
	statAvg        *atomic.Int64
	statPending    *atomic.Int64
	statOutcomes   *atomic.Int64
	statTracked    *atomic.Int64

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

	s.statGeneration = world.Resources.Status.Ints.Get("eye.ga.generation")
	s.statBest = world.Resources.Status.Ints.Get("eye.ga.best")
	s.statAvg = world.Resources.Status.Ints.Get("eye.ga.avg")
	s.statPending = world.Resources.Status.Ints.Get("eye.ga.pending")
	s.statOutcomes = world.Resources.Status.Ints.Get("eye.ga.outcomes")
	s.statTracked = world.Resources.Status.Ints.Get("eye.ga.tracked")

	s.registerSpecies()
	s.Init()
	return s
}

// registerSpecies registers tracked species with no active genes
// Enables the tracking/fitness pipeline for distance-at-death measurement
// Future: re-register with route-selection gene bounds when navigation computes alternative routes
func (s *GeneticSystem) registerSpecies() {
	eyeConfig := registry.SpeciesConfig{
		ID:          registry.SpeciesID(component.SpeciesEye),
		Name:        "eye",
		GeneCount:   0,
		Bounds:      nil,
		IsComposite: true,
	}
	_ = s.registry.Register(eyeConfig, nil)
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

	if speciesType >= component.SpeciesCount {
		speciesType = component.SpeciesNone
	}

	// Future: apply route-selection gene to entity here
	// Route index from genes[0] maps to navigation-computed path set
	// NavigationComponent.TargetGroupID or new RouteComponent assigned based on gene value

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

	s.world.Components.Genotype.SetComponent(entity, component.GenotypeComponent{
		Genes:     genes,
		EvalID:    evalID,
		Species:   speciesType,
		SpawnTime: s.world.Resources.Time.GameTime,
	})
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
		s.completeTracking(tracked, death.X, death.Y)
		delete(s.tracking, death.Entity)
	}
	s.pendingDeaths = s.pendingDeaths[:0]
}

// cleanupStaleTracking ends tracking for entities that lost NavigationComponent (OOB, resize, level change)
// Uses last known position if available; reports zero fitness otherwise
func (s *GeneticSystem) cleanupStaleTracking() {
	for entity, tracked := range s.tracking {
		if !s.world.Components.Navigation.HasEntity(entity) {
			if pos, ok := s.world.Positions.GetPosition(entity); ok {
				s.completeTracking(tracked, pos.X, pos.Y)
			} else {
				s.completeTracking(tracked, -1, -1)
			}
			delete(s.tracking, entity)
		}
	}
}

// processTracking collects per-tick metrics for all tracked entities
// Distance and member count recorded for telemetry and future route-fitness analysis
func (s *GeneticSystem) processTracking(dt time.Duration) {
	for entity, tracked := range s.tracking {
		pos, ok := s.world.Positions.GetPosition(entity)
		if !ok {
			continue
		}

		groupState := s.world.Resources.Target.GetGroup(tracked.targetGroupID)
		if !groupState.Valid || groupState.Count == 0 {
			continue
		}

		bestDistSq := -1.0
		for i := 0; i < groupState.Count; i++ {
			dx := float64(pos.X - groupState.Targets[i].PosX)
			dy := float64(pos.Y - groupState.Targets[i].PosY)
			distSq := dx*dx + dy*dy
			if bestDistSq < 0 || distSq < bestDistSq {
				bestDistSq = distSq
			}
		}

		metrics := tracking.MetricBundle{
			"distance_sq": bestDistSq,
		}

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

// completeTracking finalizes entity tracking and reports distance-at-death fitness
// Fitness = 1.0 at target, 0.0 at maximum map distance
// deathX/deathY < 0 signals unknown position (zero fitness)
func (s *GeneticSystem) completeTracking(tracked *trackedEntity, deathX, deathY int) {
	// Finalize collector, release accumulated metrics
	_ = tracked.collector.Finalize(tracking.MetricBundle{})

	fitnessVal := 0.0

	if deathX >= 0 && deathY >= 0 {
		groupState := s.world.Resources.Target.GetGroup(tracked.targetGroupID)
		if groupState.Valid && groupState.Count > 0 {
			bestDistSq := math.MaxFloat64
			for i := 0; i < groupState.Count; i++ {
				dx := float64(deathX - groupState.Targets[i].PosX)
				dy := float64(deathY - groupState.Targets[i].PosY)
				distSq := dx*dx + dy*dy
				if distSq < bestDistSq {
					bestDistSq = distSq
				}
			}

			config := s.world.Resources.Config
			maxDistSq := float64(config.MapWidth*config.MapWidth + config.MapHeight*config.MapHeight)
			if maxDistSq > 0 {
				fitnessVal = 1.0 - (bestDistSq / maxDistSq)
				if fitnessVal < 0 {
					fitnessVal = 0
				}
			}
		}
	}

	s.registry.ReportFitness(registry.SpeciesID(tracked.species), tracked.evalID, fitnessVal)

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
	stats := s.registry.Stats(registry.SpeciesID(component.SpeciesEye))
	s.statGeneration.Store(int64(stats.Generation))
	s.statBest.Store(int64(stats.BestFitness * 1000))
	s.statAvg.Store(int64(stats.AvgFitness * 1000))
	s.statPending.Store(int64(stats.PendingCount))
	s.statOutcomes.Store(int64(stats.TotalEvals))

	eyeTracked := int64(0)
	for _, t := range s.tracking {
		if t.species == component.SpeciesEye {
			eyeTracked++
		}
	}
	s.statTracked.Store(eyeTracked)
}

func boolToFloat(b bool) float64 {
	if b {
		return 1.0
	}
	return 0.0
}