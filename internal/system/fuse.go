package system

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

type FuseType int

const (
	FuseQuasar FuseType = iota
	FuseSwarm
)

// pendingFusion tracks any in-progress fusion
type pendingFusion struct {
	Type    FuseType
	TargetX int
	TargetY int
	Timer   time.Duration
}

// FuseSystem orchestrates the visual and timing transition of entity fusions and manages fusion source system
// Player-domain: it consumes drains and their visuals; the crossing is the geometry-only spawn request.
type FuseSystem struct {
	world *engine.World

	rng *vmath.FastRand

	fusions           []pendingFusion
	statSpawnFailures *atomic.Int64
	statDisabled      *atomic.Int64
	buffers           bufferTelemetry
	enabled           bool
}

// NewFuseSystem creates a new fuse system
func NewFuseSystem(world *engine.World) engine.System {
	s := &FuseSystem{
		world: world,
	}
	s.statSpawnFailures = world.Resources.Status.Ints.Get("fuse.spawn_failures")
	s.statDisabled = world.Resources.Status.Ints.Get("fuse.disabled_rejects")
	s.buffers = newBufferTelemetry(world.Resources.Status, "fuse", "pending")
	s.Init()
	return s
}

func (s *FuseSystem) Init() {
	s.fusions = make([]pendingFusion, 0, 16)
	s.statSpawnFailures.Store(0)
	s.statDisabled.Store(0)
	s.buffers.Reset()
	// Spirit scatter is a player-domain effect, so the draw must not touch the shared stream
	s.rng = s.world.Rand(core.DomainPlayer, s.Name())
	s.enabled = true
}

// Name returns system's name
func (s *FuseSystem) Name() string {
	return "fuse"
}

// Domain reports player: it draws the player stream and crosses as a spawn request (D-3).
func (s *FuseSystem) Domain() engine.SystemDomain { return engine.SystemPlayer }

// Requires the drains it consumes and the spawn path it fuses into.
func (s *FuseSystem) Requires() engine.SystemDependencies {
	return append(engine.Require("drain", "materialize", "spirit"), engine.Optional("quasar", "swarm")...)
}

func (s *FuseSystem) Priority() int {
	return parameter.PriorityFuse
}

func (s *FuseSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventFuseQuasarRequest,
		event.EventFuseSwarmRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

func (s *FuseSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameResetRequest {
		if s.hasQuasarFusion() {
			s.world.PushEventDomain(event.EventSpiritDespawnRequest, nil, core.DomainPlayer)
		}
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

	if !s.enabled {
		if ev.Type == event.EventFuseQuasarRequest || ev.Type == event.EventFuseSwarmRequest {
			s.statDisabled.Add(1)
		}
		return
	}

	switch ev.Type {
	case event.EventFuseQuasarRequest:
		if !s.hasQuasarFusion() {
			s.handleQuasarFuse()
		}

	case event.EventFuseSwarmRequest:
		if payload, ok := ev.Payload.(*event.FuseSwarmRequestPayload); ok {
			s.handleSwarmFuse(payload.DrainA, payload.DrainB, payload.Effect)
		}
	}
}

func (s *FuseSystem) Update() {
	if !s.enabled || len(s.fusions) == 0 {
		return
	}

	dt := s.world.Resources.Time.DeltaTime

	// Process in reverse to allow safe removal
	for i := len(s.fusions) - 1; i >= 0; i-- {
		s.fusions[i].Timer -= dt

		if s.fusions[i].Timer <= 0 {
			s.completeFusion(s.fusions[i])

			// RemoveEntityAt completed fusion
			s.fusions[i] = s.fusions[len(s.fusions)-1]
			s.fusions = s.fusions[:len(s.fusions)-1]
		}
	}
}

func (s *FuseSystem) hasQuasarFusion() bool {
	for _, f := range s.fusions {
		if f.Type == FuseQuasar {
			return true
		}
	}
	return false
}

// applyEffect dispatches to effect-specific implementation
func (s *FuseSystem) applyEffect(effect event.FuseEffect, sources []vmath.Point, area vmath.Area, spiritColor component.SpiritColor) {
	switch effect {
	case event.FuseEffectSpirit:
		s.effectSpiritArea(sources, area, spiritColor)
	case event.FuseEffectMaterialize:
		s.effectMaterialize(area)
	default:
		s.effectSpiritArea(sources, area, spiritColor)
	}
}

// effectSpiritArea flies one spirit per source into the target area; player-domain visuals
func (s *FuseSystem) effectSpiritArea(sources []vmath.Point, area vmath.Area, c component.SpiritColor) {
	s.world.WithDomain(core.DomainPlayer, func() {
		for i, src := range sources {
			dest := area.DistributePoint(i, s.rng)

			s.world.PushEvent(event.EventSpiritSpawnRequest, &event.SpiritSpawnRequestPayload{
				StartX:    src.X,
				StartY:    src.Y,
				TargetX:   dest.X,
				TargetY:   dest.Y,
				Char:      visual.DrainChar,
				BaseColor: c,
			})
		}
	})
}

// effectMaterialize gates the shared spawn, so it crosses as shared even though the producer is player-domain
func (s *FuseSystem) effectMaterialize(area vmath.Area) {
	s.world.PushEventDomain(event.EventMaterializeAreaRequest, &event.MaterializeAreaRequestPayload{
		X:          area.X,
		Y:          area.Y,
		AreaWidth:  area.Width,
		AreaHeight: area.Height,
		Type:       component.SpawnTypeSwarm,
	}, core.DomainShared)
}

// killDrains reports and retires the consumed drains; the batch is domain-pure, so a
// shared record never names a player entity.
func (s *FuseSystem) killDrains(drains []core.Entity) {
	if len(drains) == 0 {
		return
	}
	// TODO(phase6): EmitDeath bypasses PushEvent, so the tag does not reach the death record yet.
	s.world.WithDomain(core.DomainPlayer, func() {
		for _, drainEntity := range drains {
			killX, killY := -1, -1
			if pos, ok := s.world.Positions.GetPosition(drainEntity); ok {
				killX, killY = pos.X, pos.Y
			}
			s.world.PushEvent(event.EventSpeciesKilled, &event.SpeciesKilledPayload{
				Entity:  drainEntity,
				Species: component.SpeciesDrain,
				X:       killX,
				Y:       killY,
			})
		}
		event.EmitDeath(s.world.Resources.Event.Queue, 0, drains...)
	})
}

func (s *FuseSystem) handleSwarmFuse(drainA, drainB core.Entity, effect event.FuseEffect) {
	posA, okA := s.world.Positions.GetPosition(drainA)
	posB, okB := s.world.Positions.GetPosition(drainB)
	if !okA || !okB {
		s.statSpawnFailures.Add(1)
		return
	}

	midX := (posA.X + posB.X) / 2
	midY := (posA.Y + posB.Y) / 2

	// Find valid spawn position via spiral search
	topLeftX, topLeftY, found := s.world.Positions.FindFreeAreaSpiral(
		midX, midY,
		parameter.SwarmWidth, parameter.SwarmHeight,
		parameter.SwarmHeaderOffsetX, parameter.SwarmHeaderOffsetY,
		component.WallBlockSpawn,
		0,
	)
	if !found {
		s.statSpawnFailures.Add(1)
		return // No valid position - cancel fusion
	}

	// Update midpoint to found position's header location
	midX = topLeftX + parameter.SwarmHeaderOffsetX
	midY = topLeftY + parameter.SwarmHeaderOffsetY

	s.killDrains([]core.Entity{drainA, drainB})

	event.EmitDeath(s.world.Resources.Event.Queue, 0, drainA, drainB)

	sources := []vmath.Point{{X: posA.X, Y: posA.Y}, {X: posB.X, Y: posB.Y}}
	area := vmath.Area{X: topLeftX, Y: topLeftY, Width: parameter.SwarmWidth, Height: parameter.SwarmHeight}
	s.applyEffect(effect, sources, area, component.SpiritCyan)

	s.fusions = append(s.fusions, pendingFusion{
		Type:    FuseSwarm,
		TargetX: midX,
		TargetY: midY,
		Timer:   parameter.SwarmFuseAnimationDuration,
	})
	s.buffers.Observe(0, len(s.fusions))
}

// handleQuasarFuse consumes every drain into one quasar at their centroid
func (s *FuseSystem) handleQuasarFuse() {
	drainEntities := s.world.Components.Drain.Entities()
	sources := make([]vmath.Point, 0, len(drainEntities))

	for _, drainEntity := range drainEntities {
		if pos, ok := s.world.Positions.GetPosition(drainEntity); ok {
			sources = append(sources, vmath.Point{X: pos.X, Y: pos.Y})
		}
	}

	// Calculate centroid
	sumX, sumY := 0, 0
	for _, p := range sources {
		sumX += p.X
		sumY += p.Y
	}

	var cX, cY int
	if len(sources) > 0 {
		cX = sumX / len(sources)
		cY = sumY / len(sources)
	} else {
		config := s.world.Resources.Config
		cX = config.MapWidth / 2
		cY = config.MapHeight / 2
	}

	// Find valid spawn position via spiral search
	topLeftX, topLeftY, found := s.world.Positions.FindFreeAreaSpiral(
		cX, cY,
		parameter.QuasarWidth, parameter.QuasarHeight,
		parameter.QuasarHeaderOffsetX, parameter.QuasarHeaderOffsetY,
		component.WallBlockSpawn,
		0,
	)
	if !found {
		s.statSpawnFailures.Add(1)
		return // No valid position - cancel fusion
	}

	// Update centroid to found position's header location
	cX = topLeftX + parameter.QuasarHeaderOffsetX
	cY = topLeftY + parameter.QuasarHeaderOffsetY

	area := vmath.Area{X: topLeftX, Y: topLeftY, Width: parameter.QuasarWidth, Height: parameter.QuasarHeight}
	s.applyEffect(event.FuseEffectMaterialize, sources, area, component.SpiritCyan)
	s.killDrains(drainEntities)

	s.fusions = append(s.fusions, pendingFusion{
		Type:    FuseQuasar,
		TargetX: cX,
		TargetY: cY,
		Timer:   parameter.SpiritAnimationDuration + parameter.SpiritSafetyBuffer,
	})
	s.buffers.Observe(0, len(s.fusions))
}

// completeFusion triggers the creation event in the destination system.
// Spirits are player-domain visuals; the species spawn is the shared result.
func (s *FuseSystem) completeFusion(f pendingFusion) {
	switch f.Type {
	case FuseQuasar:
		s.world.PushEventDomain(event.EventSpiritDespawnRequest, nil, core.DomainPlayer)
		s.world.PushEventDomain(event.EventQuasarSpawnRequest, &event.QuasarSpawnRequestPayload{
			X: f.TargetX,
			Y: f.TargetY,
		}, core.DomainShared)

	case FuseSwarm:
		s.world.PushEventDomain(event.EventSwarmSpawnRequest, &event.SwarmSpawnRequestPayload{
			X: f.TargetX,
			Y: f.TargetY,
		}, core.DomainShared)
	}
}
