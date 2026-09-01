package system

import (
	"encoding/json"
	"errors"
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
	trackKeys     []core.Entity
	pendingDeaths []event.SpeciesKilledPayload

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
	buffers     bufferTelemetry

	enabled bool
}

func NewGeneticSystem(world *engine.World) engine.System {
	reg := registry.NewRegistry(nil) // Populations are per-run; maps are generated fresh

	world.Resources.Genetics = &engine.GeneticResource{Registry: reg}

	s := &GeneticSystem{
		world:         world,
		registry:      reg,
		tracking:      make(map[core.Entity]*trackedEntity, 64),
		pendingDeaths: make([]event.SpeciesKilledPayload, 0, 16),
		typeFitBuf:    make([]byte, 0, 64),
	}

	s.statGeneration = world.Resources.Status.Ints.Get("eye.ga.generation")
	s.statBest = world.Resources.Status.Ints.Get("eye.ga.best")
	s.statAvg = world.Resources.Status.Ints.Get("eye.ga.avg")
	s.statPending = world.Resources.Status.Ints.Get("eye.ga.pending")
	s.statOutcomes = world.Resources.Status.Ints.Get("eye.ga.outcomes")
	s.statTracked = world.Resources.Status.Ints.Get("eye.ga.tracked")
	s.statTypeFit = world.Resources.Status.Strings.Get("eye.ga.typefit")
	s.buffers = newBufferTelemetry(world.Resources.Status, "eye", "ga_track_keys", "ga_pending_deaths", "ga_typefit", "ga_tracking")

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
	s.statGeneration.Store(0)
	s.statBest.Store(0)
	s.statAvg.Store(0)
	s.statPending.Store(0)
	s.statOutcomes.Store(0)
	s.statTracked.Store(0)
	s.statTypeFit.Store("-")
	s.buffers.Reset()

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
		event.EventSpeciesCreated,
		event.EventSpeciesKilled,
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

	case event.EventSpeciesCreated:
		if payload, ok := ev.Payload.(*event.SpeciesCreatedPayload); ok {
			s.mu.Lock()
			s.handleSpeciesCreated(payload.Entity, payload.Species, payload.SubType, payload.EvalID, payload.Genes)
			s.mu.Unlock()
		}

	case event.EventSpeciesKilled:
		if payload, ok := ev.Payload.(*event.SpeciesKilledPayload); ok {
			s.mu.Lock()
			s.pendingDeaths = append(s.pendingDeaths, *payload)
			s.buffers.Observe(1, len(s.pendingDeaths))
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
	cfg.Seed = vmath.DeriveSeed(s.world.Resources.Rand.DomainRoot(core.DomainShared),
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

func (s *GeneticSystem) handleSpeciesCreated(entity core.Entity, speciesType component.SpeciesType, subType uint8, evalID uint64, genes []float64) {
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
	s.buffers.Observe(3, len(s.tracking))

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
// NavigationComponent (OOB, resize, level change).
// Sorted iteration: completeTracking folds a non-commutative EMA and reports
// fitness in call order.
func (s *GeneticSystem) processTracking() {
	eyes := 0

	s.trackKeys = sortedKeys(s.trackKeys, s.tracking)
	s.buffers.Observe(0, len(s.trackKeys))
	for _, entity := range s.trackKeys {
		tracked := s.tracking[entity]

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
	s.publishTypeFit()
}

// geneticSnapshot is this system's D-19 record: the populations, plus the two
// pieces of per-system scratch that decide a published value.
//
// The telemetry throttle is here for the reason §4.1 predicted when it listed
// "per-system scratch: throttles" as state with no inventory. eye.ga.typefit is
// part of the compared shared surface, and it is published once every
// StatSnapshotTicks — so two instances whose throttles stand at different counts
// publish on different ticks and disagree until both have cycled. The running
// per-type average travels for the same reason: it is what the published value is
// computed from.
type geneticSnapshot struct {
	Registry       []registry.SpeciesState         `json:"registry"`
	Tracking       []geneticTrackedState           `json:"tracking,omitempty"`
	PendingDeaths  []event.SpeciesKilledPayload    `json:"pending_deaths,omitempty"`
	TelemetryTicks int                             `json:"telemetry_ticks"`
	TypeFitEMA     [parameter.EyeTypeCount]float64 `json:"type_fit_ema"`
	EyeTracked     int64                           `json:"eye_tracked"`
	Enabled        bool                            `json:"enabled"`
}

// geneticTrackedState is the canonical, serializable form of one live fitness
// evaluation. The map itself is system-private state; the entity and EvalID tie it
// back to the captured GenotypeComponent and streaming pending table.
type geneticTrackedState struct {
	Entity        core.Entity           `json:"entity"`
	EvalID        uint64                `json:"eval_id"`
	Species       component.SpeciesType `json:"species"`
	SubType       uint8                 `json:"sub_type"`
	TargetGroupID uint8                 `json:"target_group_id"`
	DealtDamage   float64               `json:"dealt_damage"`
	MinDistSq     float64               `json:"min_dist_sq"`
}

// SaveShared carries the complete genetic continuation point (D-19).
//
// The second learned resource the hidden-state survey named. The registry lives
// behind a mutex and per-slot atomics in pkg/genetic, which is why it needed an
// export contract it did not have: persistence could write one species at a time
// to a caller-supplied store, and a transfer needs all of them as one value with
// no store involved and no lock reaching here.
func (s *GeneticSystem) SaveShared() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap := geneticSnapshot{
		TelemetryTicks: s.telemetryTicks,
		TypeFitEMA:     s.typeFitEMA,
		EyeTracked:     s.eyeTracked,
		Enabled:        s.enabled,
		PendingDeaths:  append([]event.SpeciesKilledPayload(nil), s.pendingDeaths...),
	}
	if s.registry != nil {
		var err error
		snap.Registry, err = s.registry.Export()
		if err != nil {
			return nil, err
		}
	}
	keys := sortedKeys(make([]core.Entity, 0, len(s.tracking)), s.tracking)
	for _, entity := range keys {
		tracked := s.tracking[entity]
		snap.Tracking = append(snap.Tracking, geneticTrackedState{
			Entity:        entity,
			EvalID:        tracked.evalID,
			Species:       tracked.species,
			SubType:       tracked.subType,
			TargetGroupID: tracked.targetGroupID,
			DealtDamage:   tracked.dealtDamage,
			MinDistSq:     tracked.minDistSq,
		})
	}
	return json.Marshal(snap)
}

// LoadShared installs exported populations. A species this build does not
// register is an error rather than a skip: a population that silently fails to
// install leaves this instance evolving from its own archive while believing it
// adopted the capture's.
func (s *GeneticSystem) LoadShared(data []byte) error {
	var snap geneticSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("genetic: %w", err)
	}
	tracking := make(map[core.Entity]*trackedEntity, len(snap.Tracking))
	for _, state := range snap.Tracking {
		if state.Entity == 0 {
			return errors.New("genetic: tracked state names entity zero")
		}
		if _, exists := tracking[state.Entity]; exists {
			return fmt.Errorf("genetic: tracked entity %d appears more than once", state.Entity)
		}
		tracking[state.Entity] = &trackedEntity{
			evalID:        state.EvalID,
			species:       state.Species,
			subType:       state.SubType,
			targetGroupID: state.TargetGroupID,
			dealtDamage:   state.DealtDamage,
			minDistSq:     state.MinDistSq,
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.registry == nil {
		if len(snap.Registry) == 0 {
			return nil
		}
		return errors.New("genetic: registry is not initialized")
	}
	if err := s.registry.Import(snap.Registry); err != nil {
		return err
	}
	s.tracking = tracking
	s.pendingDeaths = append(s.pendingDeaths[:0], snap.PendingDeaths...)
	s.telemetryTicks = snap.TelemetryTicks
	s.typeFitEMA = snap.TypeFitEMA
	s.eyeTracked = snap.EyeTracked
	s.enabled = snap.Enabled
	s.publishTypeFit()
	return nil
}

// publishTypeFit writes the per-type average into its cell without touching the
// throttle. Called on install so the compared surface reports the populations
// just adopted rather than the ones this instance had.
func (s *GeneticSystem) publishTypeFit() {
	buf := s.typeFitBuf[:0]
	for i, v := range s.typeFitEMA {
		if i > 0 {
			buf = append(buf, '/')
		}
		buf = strconv.AppendInt(buf, int64(v*100), 10)
	}
	s.typeFitBuf = buf
	s.buffers.Observe(2, len(s.typeFitBuf))
	s.statTypeFit.Store(string(buf))
}
