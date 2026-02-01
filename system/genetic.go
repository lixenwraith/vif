package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/genetic/game"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// trackedEntity holds cached data for fitness calculation
// Component data cached at tracking start since entity may be destroyed before observation
type trackedEntity struct {
	species  component.SpeciesType
	evalID   uint64
	genoComp component.GenotypeComponent
}

// GeneticSystem observes entity lifecycle and reports fitness
type GeneticSystem struct {
	world *engine.World

	// Active tracking: entity -> cached tracking data
	activeTracking map[core.Entity]*trackedEntity

	// Fitness calculator
	aggregator *game.CombatFitnessAggregator

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
		world:          world,
		activeTracking: make(map[core.Entity]*trackedEntity),
		aggregator:     game.NewCombatFitnessAggregator(game.DefaultDrainWeights),
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
	cursorEntity := s.world.Resources.Player.Entity
	cursorPos, cursorOk := s.world.Positions.GetPosition(cursorEntity)

	// Shield state for time-in-shield tracking
	shieldComp, shieldOk := s.world.Components.Shield.GetComponent(cursorEntity)
	shieldActive := shieldOk && shieldComp.Active

	// Process tracked entities: update metrics or handle death
	for entity, tracked := range s.activeTracking {
		if !s.isEntityAlive(entity, tracked.species) {
			s.handleDeath(entity, tracked, cursorPos, cursorOk)
			delete(s.activeTracking, entity)
			continue
		}

		// Update metrics in cached component
		tracked.genoComp.TicksAlive++

		// Distance tracking
		if cursorOk {
			if pos, ok := s.world.Positions.GetPosition(entity); ok {
				dx := float64(pos.X - cursorPos.X)
				dy := float64(pos.Y - cursorPos.Y)
				tracked.genoComp.CumulativeDistSq += dx*dx + dy*dy
				tracked.genoComp.DistSamples++

				// Shield overlap check
				if shieldActive {
					if vmath.EllipseContainsPoint(pos.X, pos.Y, cursorPos.X, cursorPos.Y,
						shieldComp.InvRxSq, shieldComp.InvRySq) {
						tracked.genoComp.TimeInShield += dt
					}
				}
			}
		}
	}

	// Detect new entities (have Genotype, not tracked)
	for _, entity := range s.world.Components.Genotype.GetAllEntities() {
		if _, tracked := s.activeTracking[entity]; !tracked {
			genoComp, ok := s.world.Components.Genotype.GetComponent(entity)
			if ok && genoComp.EvalID != 0 {
				s.activeTracking[entity] = &trackedEntity{
					species:  genoComp.Species,
					evalID:   genoComp.EvalID,
					genoComp: genoComp, // Cache component data
				}
			}
		}
	}

	// Update telemetry
	stats := genetic.Provider.Stats(component.SpeciesDrain)
	s.statGeneration.Store(int64(stats.Generation))
	s.statBest.Store(int64(stats.Best * 1000))
	s.statAvg.Store(int64(stats.Avg * 1000))
	s.statPending.Store(int64(stats.PendingCount))
	s.statOutcomes.Store(int64(stats.OutcomesTotal))
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

func (s *GeneticSystem) handleDeath(entity core.Entity, tracked *trackedEntity, cursorPos component.PositionComponent, cursorOk bool) {
	// Check death at cursor using last known position
	deathAtCursor := false
	if cursorOk {
		if lastPos, ok := s.world.Positions.GetPosition(entity); ok {
			deathAtCursor = lastPos.X == cursorPos.X && lastPos.Y == cursorPos.Y
		}
	}

	outcome := game.CombatOutcome{
		TicksAlive:       tracked.genoComp.TicksAlive,
		CumulativeDistSq: tracked.genoComp.CumulativeDistSq,
		DistSamples:      tracked.genoComp.DistSamples,
		TimeInShield:     tracked.genoComp.TimeInShield.Seconds(),
		DeathAtCursor:    deathAtCursor,
	}

	fitness := s.aggregator.Calculate(outcome)

	genetic := s.world.Resources.Genetic
	if genetic != nil && genetic.Provider != nil {
		genetic.Provider.Complete(tracked.species, tracked.evalID, fitness)
	}
}