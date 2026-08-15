package system

import (
	"fmt"
	"math"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/status"
	"github.com/lixenwraith/vi-fighter/pkg/genetic"
	"github.com/lixenwraith/vi-fighter/pkg/genetic/registry"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// --- Tracked Entity ---

// trackedEntity accumulates fitness signal between spawn and death
type trackedEntity struct {
	evalID        uint64
	species       component.SpeciesType
	subType       uint8
	targetGroupID uint8
	dealtDamage   float64
	minDistSq     float64 // Closest approach to the target group over the lifetime
}

// --- Genetic System ---

type GeneticSystem struct {
	world *engine.World

	mu sync.Mutex

	registry *registry.Registry

	tracking      map[core.Entity]*trackedEntity
	pendingDeaths []event.EnemyKilledPayload

	eyeTracked     int64
	telemetryTicks int

	statGeneration *atomic.Int64
	statBest       *atomic.Int64
	statAvg        *atomic.Int64
	statPending    *atomic.Int64
	statOutcomes   *atomic.Int64
	statTracked    *atomic.Int64

	typeFitEMA  [parameter.EyeTypeCount]float64
	statTypeFit *status.AtomicString
	typeFitBuf  []byte

	enabled bool
}

func NewGeneticSystem(world *engine.World) engine.System {
	reg := registry.NewRegistry(nil) // Populations are per-run; maps are generated fresh

	world.Resources.Genetics = &engine.GeneticResource{Registry: reg}

	s := &GeneticSystem{
		world:         world,
		registry:      reg,
		tracking:      make(map[core.Entity]*trackedEntity, 64),
		pendingDeaths: make([]event.EnemyKilledPayload, 0, 16),
		typeFitBuf:    make([]byte, 0, 64),
	}

	s.statGeneration = world.Resources.Status.Ints.Get("eye.ga.generation")
	s.statBest = world.Resources.Status.Ints.Get("eye.ga.best")
	s.statAvg = world.Resources.Status.Ints.Get("eye.ga.avg")
	s.statPending = world.Resources.Status.Ints.Get("eye.ga.pending")
	s.statOutcomes = world.Resources.Status.Ints.Get("eye.ga.outcomes")
	s.statTracked = world.Resources.Status.Ints.Get("eye.ga.tracked")
	s.statTypeFit = world.Resources.Status.Strings.Get("eye.ga.typefit")

	s.Init()
	return s
}

func (s *GeneticSystem) Init() {
	clear(s.tracking)
	s.pendingDeaths = s.pendingDeaths[:0]
	clear(s.typeFitEMA[:])
	s.eyeTracked = 0
	s.telemetryTicks = 0
	s.enabled = true

	s.registry.Reset() // Drop evaluations belonging to the previous run
	_ = s.registry.Start()
}

func (s *GeneticSystem) Name() string { return "genetic" }

func (s *GeneticSystem) Priority() int { return parameter.PriorityGenetic }

func (s *GeneticSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventGameResetRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGeneticRegisterSpecies,
		event.EventGeneticAbandonEval,
		event.EventEnemyCreated,
		event.EventEnemyKilled,
		event.EventCombatAttackAreaRequest,
	}
}

func (s *GeneticSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameResetRequest {
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

	if ev.Type == event.EventGeneticAbandonEval {
		if payload, ok := ev.Payload.(*event.GeneticAbandonEvalPayload); ok {
			s.registry.AbandonFitness(registry.SpeciesID(payload.Species), payload.EvalID)
		}
		return
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventGeneticRegisterSpecies:
		if payload, ok := ev.Payload.(*event.GeneticRegisterSpeciesPayload); ok {
			s.mu.Lock()
			s.handleRegistration(payload)
			s.mu.Unlock()
		}

	case event.EventEnemyCreated:
		if payload, ok := ev.Payload.(*event.EnemyCreatedPayload); ok {
			s.mu.Lock()
			s.handleEnemyCreated(payload.Entity, payload.Species, payload.SubType, payload.EvalID, payload.Genes)
			s.mu.Unlock()
		}

	case event.EventEnemyKilled:
		if payload, ok := ev.Payload.(*event.EnemyKilledPayload); ok {
			s.mu.Lock()
			s.pendingDeaths = append(s.pendingDeaths, *payload)
			s.mu.Unlock()
		}

	case event.EventCombatAttackAreaRequest:
		if payload, ok := ev.Payload.(*event.CombatAttackAreaRequestPayload); ok {
			s.mu.Lock()
			// Only reward successful self-destructs against targets (ignores cursor shield bumps)
			if tracked, ok := s.tracking[payload.OwnerEntity]; ok &&
				payload.AttackType == component.CombatAttackSelfDestruct {
				tracked.dealtDamage += float64(parameter.CombatDamageEyeSelfDestruct)
			}
			s.mu.Unlock()
		}
	}
}

func (s *GeneticSystem) handleRegistration(payload *event.GeneticRegisterSpeciesPayload) {
	bounds := make([]genetic.ParameterBounds, len(payload.Bounds))
	for i, b := range payload.Bounds {
		bounds[i] = genetic.ParameterBounds{Min: b.Min, Max: b.Max}
	}

	cfg := parameter.GAStreamingConfig()
	// Per-species seed: one root, independent streams, stable across runs
	cfg.Seed = vmath.DeriveSeed(s.world.Resources.Rand.SessionRoot(),
		"genetic:"+strconv.Itoa(int(payload.Species)))
	config := registry.SpeciesConfig{
		ID:                 registry.SpeciesID(payload.Species),
		Name:               fmt.Sprintf("species_%d", payload.Species),
		GeneCount:          payload.GeneCount,
		ProbeBins:          payload.ProbeBins,
		Bounds:             bounds,
		Boundary:           parameter.GABoundaryMode,
		PerturbationStdDev: payload.PerturbationStdDev,
		TournamentSize:     parameter.GATournamentSize,
		MixProbability:     parameter.GACrossoverMixProbability,
		IsComposite:        payload.IsComposite,
		EngineConfig:       &cfg,
	}

	// Start is idempotent; run it even when this species was already registered
	_ = s.registry.Register(config, nil)
	_ = s.registry.Start()
}

func (s *GeneticSystem) handleEnemyCreated(entity core.Entity, speciesType component.SpeciesType, subType uint8, evalID uint64, genes []float64) {
	if evalID == 0 {
		return // Not GA-managed
	}
	if _, exists := s.tracking[entity]; exists {
		return
	}
	if speciesType >= component.SpeciesCount {
		speciesType = component.SpeciesNone
	}

	groupID := uint8(0)
	if tc, ok := s.world.Components.Target.GetComponent(entity); ok {
		groupID = tc.GroupID
	}

	s.tracking[entity] = &trackedEntity{
		evalID:        evalID,
		species:       speciesType,
		subType:       subType,
		targetGroupID: groupID,
		minDistSq:     math.MaxFloat64,
	}

	s.world.Components.Genotype.SetComponent(entity, component.GenotypeComponent{
		Genes:     genes,
		EvalID:    evalID,
		Species:   speciesType,
		SubType:   subType,
		SpawnTime: s.world.Resources.Time.GameTime,
	})
}

func (s *GeneticSystem) Update() {
	if !s.enabled {
		return
	}

	s.mu.Lock()
	s.processPendingDeaths()
	s.processTracking()
	s.mu.Unlock()

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

// processTracking updates closest approach and retires entities that lost
// NavigationComponent (OOB, resize, level change)
func (s *GeneticSystem) processTracking() {
	eyes := 0

	for entity, tracked := range s.tracking {
		if !s.world.Components.Navigation.HasEntity(entity) {
			if pos, ok := s.world.Positions.GetPosition(entity); ok {
				s.completeTracking(tracked, pos.X, pos.Y)
			} else {
				s.completeTracking(tracked, -1, -1)
			}
			delete(s.tracking, entity)
			continue
		}

		if tracked.species == component.SpeciesEye {
			eyes++
		}

		pos, ok := s.world.Positions.GetPosition(entity)
		if !ok {
			continue
		}
		group := s.world.Resources.Target.GetGroup(tracked.targetGroupID)
		if !group.Valid || group.Count == 0 {
			continue
		}

		for i := range group.Count {
			dx := float64(pos.X - group.Targets[i].PosX)
			dy := float64(pos.Y - group.Targets[i].PosY)
			if d := dx*dx + dy*dy; d < tracked.minDistSq {
				tracked.minDistSq = d
			}
		}
	}

	s.eyeTracked = int64(eyes)
}

// completeTracking converts closest approach and dealt damage into fitness.
// Evaluations with no positional signal are abandoned rather than scored zero
func (s *GeneticSystem) completeTracking(tracked *trackedEntity, deathX, deathY int) {
	speciesID := registry.SpeciesID(tracked.species)
	bestDistSq := tracked.minDistSq

	if deathX >= 0 && deathY >= 0 {
		group := s.world.Resources.Target.GetGroup(tracked.targetGroupID)
		if group.Valid {
			for i := range group.Count {
				dx := float64(deathX - group.Targets[i].PosX)
				dy := float64(deathY - group.Targets[i].PosY)
				if d := dx*dx + dy*dy; d < bestDistSq {
					bestDistSq = d
				}
			}
		}
	}

	if bestDistSq == math.MaxFloat64 {
		s.registry.AbandonFitness(speciesID, tracked.evalID)
		return
	}

	config := s.world.Resources.Config
	maxDistSq := float64(config.MapWidth*config.MapWidth + config.MapHeight*config.MapHeight)

	proximity := 0.0
	if maxDistSq > 0 {
		if proximity = 1.0 - bestDistSq/maxDistSq; proximity < 0 {
			proximity = 0
		}
	}

	damage := tracked.dealtDamage / parameter.GAFitnessDamageRef
	if damage > 1 {
		damage = 1
	}
	fitness := proximity + parameter.GAFitnessDamageWeight*damage

	// Per-subtype fitness EMA for probe/distribution evaluation
	if tracked.species == component.SpeciesEye && int(tracked.subType) < parameter.EyeTypeCount {
		const alpha = 0.1
		s.typeFitEMA[tracked.subType] = (1-alpha)*s.typeFitEMA[tracked.subType] + alpha*fitness
	}

	s.registry.ReportFitness(speciesID, tracked.evalID, fitness)
}

func (s *GeneticSystem) updateTelemetry() {
	st := s.registry.Stats(registry.SpeciesID(component.SpeciesEye))
	s.statGeneration.Store(int64(st.Generation))
	s.statBest.Store(int64(st.BestFitness * 1000))
	s.statAvg.Store(int64(st.AvgFitness * 1000))
	s.statPending.Store(int64(st.PendingCount))
	s.statOutcomes.Store(int64(st.TotalEvals))
	s.statTracked.Store(s.eyeTracked)

	// Per-type EMA is string-formatted; publish at the snapshot rate, not per tick
	if parameter.StatSnapshotTicks == 0 {
		return
	}
	s.telemetryTicks++
	if s.telemetryTicks < parameter.StatSnapshotTicks {
		return
	}
	s.telemetryTicks = 0

	buf := s.typeFitBuf[:0]
	for i, v := range s.typeFitEMA {
		if i > 0 {
			buf = append(buf, '/')
		}
		buf = strconv.AppendInt(buf, int64(v*100), 10)
	}
	s.typeFitBuf = buf
	s.statTypeFit.Store(string(buf))
}
