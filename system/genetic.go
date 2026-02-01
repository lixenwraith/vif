package system

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/genetic/game"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// trackedEntity holds tracking state for single entities (drain)
type trackedEntity struct {
	species   component.SpeciesType
	evalID    uint64
	collector *game.StandardCollector
}

// trackedComposite holds tracking state for composite entities (swarm, quasar)
type trackedComposite struct {
	species      component.SpeciesType
	evalID       uint64
	headerEntity core.Entity
	collector    *game.CompositeCollector
}

// GeneticSystem observes entity lifecycle and reports fitness
type GeneticSystem struct {
	world *engine.World

	activeTracking    map[core.Entity]*trackedEntity
	compositeTracking map[core.Entity]*trackedComposite

	// Cached cursor state for metric collection
	cursorPos                    component.PositionComponent
	cursorEnergy                 int64
	cursorHeat                   int
	shieldActive                 bool
	shieldInvRxSq, shieldInvRySq int64

	// Telemetry
	statGeneration *atomic.Int64
	statBest       *atomic.Int64
	statAvg        *atomic.Int64
	statPending    *atomic.Int64
	statOutcomes   *atomic.Int64

	enabled bool
}

func NewGeneticSystem(world *engine.World) engine.System {
	s := &GeneticSystem{
		world:             world,
		activeTracking:    make(map[core.Entity]*trackedEntity),
		compositeTracking: make(map[core.Entity]*trackedComposite),
	}

	s.statGeneration = world.Resources.Status.Ints.Get("ga.generation")
	s.statBest = world.Resources.Status.Ints.Get("ga.best")
	s.statAvg = world.Resources.Status.Ints.Get("ga.avg")
	s.statPending = world.Resources.Status.Ints.Get("ga.pending")
	s.statOutcomes = world.Resources.Status.Ints.Get("ga.outcomes")

	s.Init()
	return s
}

func (s *GeneticSystem) Init() {
	clear(s.activeTracking)
	s.enabled = true

	// Reset GA tracker on game reset (population retained, pending evals cleared)
	if genetic := s.world.Resources.Genetic; genetic != nil && genetic.Provider != nil {
		genetic.Provider.Reset()
		genetic.Provider.Start()
	}
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
		event.EventSwarmSpawned,
		event.EventSwarmDespawned,
		event.EventQuasarSpawned,
		event.EventQuasarDestroyed,
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
	case event.EventSwarmSpawned:
		if payload, ok := ev.Payload.(*event.SwarmSpawnedPayload); ok {
			s.beginCompositeTracking(payload.HeaderEntity, component.SpeciesSwarm)
		}

	case event.EventSwarmDespawned:
		if payload, ok := ev.Payload.(*event.SwarmDespawnedPayload); ok {
			s.completeCompositeTracking(payload.HeaderEntity, false)
		}

	case event.EventQuasarSpawned:
		if payload, ok := ev.Payload.(*event.QuasarSpawnedPayload); ok {
			s.beginCompositeTracking(payload.HeaderEntity, component.SpeciesQuasar)
		}

	case event.EventQuasarDestroyed:
		s.completeAllCompositeOfSpecies(component.SpeciesQuasar)
	}
}

func (s *GeneticSystem) Update() {
	if !s.enabled {
		return
	}

	genetic := s.world.Resources.Genetic
	if genetic == nil || genetic.Provider == nil {
		return
	}

	dt := s.world.Resources.Time.DeltaTime
	s.updateCursorState()
	s.updatePlayerModel(genetic)
	s.processEntityTracking(dt, genetic)
	s.processCompositeTracking(dt, genetic)
	s.detectNewEntities()
	s.updateTelemetry(genetic)
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

func (s *GeneticSystem) updatePlayerModel(genetic *engine.GeneticResource) {
	res, ok := genetic.Provider.(*game.GeneticResource)
	if !ok {
		return
	}

	model := res.PlayerModel()
	// TODO: EnergyMax doesn't make sense, energy can have polarity
	model.RecordEnergyLevel(s.cursorEnergy, parameter.EnergyMax)
	model.RecordHeatLevel(s.cursorHeat, parameter.HeatMax)
}

func (s *GeneticSystem) processEntityTracking(dt time.Duration, genetic *engine.GeneticResource) {
	res, ok := genetic.Provider.(*game.GeneticResource)
	if !ok {
		return
	}

	playerSnapshot := res.PlayerModel().Snapshot()

	for entity, tracked := range s.activeTracking {
		if !s.isEntityAlive(entity, tracked.species) {
			s.completeEntityTracking(entity, tracked, playerSnapshot, genetic)
			delete(s.activeTracking, entity)
			continue
		}

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

		metrics := game.EntityMetrics{
			DistanceToCursor: dx*dx + dy*dy,
			InsideShield:     insideShield,
			CursorEnergy:     s.cursorEnergy,
			CursorHeat:       s.cursorHeat,
		}

		tracked.collector.Collect(metrics, dt)
	}
}

func (s *GeneticSystem) processCompositeTracking(dt time.Duration, genetic *engine.GeneticResource) {
	res, ok := genetic.Provider.(*game.GeneticResource)
	if !ok {
		return
	}

	playerSnapshot := res.PlayerModel().Snapshot()

	for headerEntity, tracked := range s.compositeTracking {
		header, ok := s.world.Components.Header.GetComponent(headerEntity)
		if !ok {
			s.completeCompositeTrackingInternal(headerEntity, tracked, playerSnapshot, genetic, false)
			delete(s.compositeTracking, headerEntity)
			continue
		}

		pos, ok := s.world.Positions.GetPosition(headerEntity)
		if !ok {
			continue
		}

		dx := float64(pos.X - s.cursorPos.X)
		dy := float64(pos.Y - s.cursorPos.Y)

		memberCount := 0
		for _, m := range header.MemberEntries {
			if m.Entity != 0 {
				memberCount++
			}
		}

		metrics := game.EntityMetrics{
			DistanceToCursor: dx*dx + dy*dy,
			InsideShield:     false,
			CursorEnergy:     s.cursorEnergy,
			CursorHeat:       s.cursorHeat,
			MemberCount:      memberCount,
		}

		tracked.collector.Collect(metrics, dt)
	}
}

func (s *GeneticSystem) detectNewEntities() {
	for _, entity := range s.world.Components.Genotype.GetAllEntities() {
		if _, tracked := s.activeTracking[entity]; tracked {
			continue
		}
		if _, tracked := s.compositeTracking[entity]; tracked {
			continue
		}

		genoComp, ok := s.world.Components.Genotype.GetComponent(entity)
		if !ok || genoComp.EvalID == 0 {
			continue
		}

		// Determine if this is a composite header or single entity
		if s.world.Components.Header.HasEntity(entity) {
			// Composite - tracked via events, skip here
			continue
		}

		genetic := s.world.Resources.Genetic
		res, ok := genetic.Provider.(*game.GeneticResource)
		if !ok {
			continue
		}

		tracker := res.Tracker(genoComp.Species)
		if tracker == nil {
			continue
		}

		s.activeTracking[entity] = &trackedEntity{
			species:   genoComp.Species,
			evalID:    genoComp.EvalID,
			collector: tracker.AcquireCollector(),
		}
	}
}

func (s *GeneticSystem) isEntityAlive(entity core.Entity, species component.SpeciesType) bool {
	switch species {
	case component.SpeciesDrain:
		return s.world.Components.Drain.HasEntity(entity)
	case component.SpeciesSwarm:
		return s.world.Components.Swarm.HasEntity(entity)
	case component.SpeciesQuasar:
		return s.world.Components.Quasar.HasEntity(entity)
	}
	return false
}

func (s *GeneticSystem) completeEntityTracking(
	entity core.Entity,
	tracked *trackedEntity,
	playerSnapshot game.PlayerBehaviorSnapshot,
	genetic *engine.GeneticResource,
) {
	res, ok := genetic.Provider.(*game.GeneticResource)
	if !ok {
		return
	}

	deathAtCursor := false
	if pos, ok := s.world.Positions.GetPosition(entity); ok {
		deathAtCursor = pos.X == s.cursorPos.X && pos.Y == s.cursorPos.Y
	}

	snapshot := tracked.collector.Finalize(deathAtCursor)

	tracker := res.Tracker(tracked.species)
	if tracker != nil && tracker.Aggregator != nil {
		fitness := tracker.Aggregator.Calculate(snapshot, playerSnapshot)
		genetic.Provider.Complete(tracked.species, tracked.evalID, fitness)
		tracker.ReleaseCollector(tracked.collector)
	}
}

func (s *GeneticSystem) beginCompositeTracking(headerEntity core.Entity, species component.SpeciesType) {
	if _, exists := s.compositeTracking[headerEntity]; exists {
		return
	}

	genoComp, ok := s.world.Components.Genotype.GetComponent(headerEntity)
	if !ok || genoComp.EvalID == 0 {
		return
	}

	genetic := s.world.Resources.Genetic
	res, ok := genetic.Provider.(*game.GeneticResource)
	if !ok {
		return
	}

	tracker := res.Tracker(species)
	if tracker == nil {
		return
	}

	s.compositeTracking[headerEntity] = &trackedComposite{
		species:      species,
		evalID:       genoComp.EvalID,
		headerEntity: headerEntity,
		collector:    tracker.AcquireCompositeCollector(),
	}
}

func (s *GeneticSystem) completeCompositeTracking(headerEntity core.Entity, deathAtCursor bool) {
	tracked, ok := s.compositeTracking[headerEntity]
	if !ok {
		return
	}

	genetic := s.world.Resources.Genetic
	res, resOk := genetic.Provider.(*game.GeneticResource)
	if resOk {
		playerSnapshot := res.PlayerModel().Snapshot()
		s.completeCompositeTrackingInternal(headerEntity, tracked, playerSnapshot, genetic, deathAtCursor)
	}

	delete(s.compositeTracking, headerEntity)
}

func (s *GeneticSystem) completeCompositeTrackingInternal(
	headerEntity core.Entity,
	tracked *trackedComposite,
	playerSnapshot game.PlayerBehaviorSnapshot,
	genetic *engine.GeneticResource,
	deathAtCursor bool,
) {
	res, ok := genetic.Provider.(*game.GeneticResource)
	if !ok {
		return
	}

	snapshot := tracked.collector.Finalize(deathAtCursor)

	tracker := res.Tracker(tracked.species)
	if tracker != nil && tracker.Aggregator != nil {
		fitness := tracker.Aggregator.Calculate(snapshot, playerSnapshot)
		genetic.Provider.Complete(tracked.species, tracked.evalID, fitness)
		tracker.ReleaseCompositeCollector(tracked.collector)
	}
}

func (s *GeneticSystem) completeAllCompositeOfSpecies(species component.SpeciesType) {
	genetic := s.world.Resources.Genetic
	res, ok := genetic.Provider.(*game.GeneticResource)
	if !ok {
		return
	}

	playerSnapshot := res.PlayerModel().Snapshot()

	for headerEntity, tracked := range s.compositeTracking {
		if tracked.species == species {
			s.completeCompositeTrackingInternal(headerEntity, tracked, playerSnapshot, genetic, false)
			delete(s.compositeTracking, headerEntity)
		}
	}
}

func (s *GeneticSystem) updateTelemetry(genetic *engine.GeneticResource) {
	stats := genetic.Provider.Stats(component.SpeciesDrain)
	s.statGeneration.Store(int64(stats.Generation))
	s.statBest.Store(int64(stats.Best * 1000))
	s.statAvg.Store(int64(stats.Avg * 1000))
	s.statPending.Store(int64(stats.PendingCount))
	s.statOutcomes.Store(int64(stats.OutcomesTotal))
}