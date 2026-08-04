package system

import (
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
// Actual entity creation is delegated to target systems (QuasarSystem and SwarmSystem)
type FuseSystem struct {
	world *engine.World

	rng *vmath.FastRand

	fusions []pendingFusion
	enabled bool
}

// NewFuseSystem creates a new fuse system
func NewFuseSystem(world *engine.World) engine.System {
	s := &FuseSystem{
		world: world,
	}
	s.Init()
	return s
}

func (s *FuseSystem) Init() {
	s.fusions = make([]pendingFusion, 0, 16)
	s.rng = vmath.NewFastRand(uint64(s.world.Resources.Time.RealTimeNano()))
	s.enabled = true
}

// Name returns system's name
func (s *FuseSystem) Name() string {
	return "fuse"
}

func (s *FuseSystem) Priority() int {
	return parameter.PriorityFuse
}

func (s *FuseSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventFuseQuasarRequest,
		event.EventFuseSwarmRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *FuseSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		if s.hasQuasarFusion() {
			s.world.PushEvent(event.EventSpiritDespawn, nil)
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

func (s *FuseSystem) effectSpiritArea(sources []vmath.Point, area vmath.Area, c component.SpiritColor) {
	for i, src := range sources {
		dest := area.DistributePoint(i, s.rng)

		s.world.PushEvent(event.EventSpiritSpawn, &event.SpiritSpawnRequestPayload{
			StartX:    src.X,
			StartY:    src.Y,
			TargetX:   dest.X,
			TargetY:   dest.Y,
			Char:      visual.DrainChar,
			BaseColor: c,
		})
	}
}

func (s *FuseSystem) effectMaterialize(area vmath.Area) {
	s.world.PushEvent(event.EventMaterializeAreaRequest, &event.MaterializeAreaRequestPayload{
		X:          area.X,
		Y:          area.Y,
		AreaWidth:  area.Width,
		AreaHeight: area.Height,
		Type:       component.SpawnTypeSwarm,
	})
}

func (s *FuseSystem) handleSwarmFuse(drainA, drainB core.Entity, effect event.FuseEffect) {
	posA, okA := s.world.Positions.GetPosition(drainA)
	posB, okB := s.world.Positions.GetPosition(drainB)
	if !okA || !okB {
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
		return // No valid position - cancel fusion
	}

	// Update midpoint to found position's header location
	midX = topLeftX + parameter.SwarmHeaderOffsetX
	midY = topLeftY + parameter.SwarmHeaderOffsetY

	// Emit death drain events
	for _, drainEntity := range []core.Entity{drainA, drainB} {
		if pos, ok := s.world.Positions.GetPosition(drainEntity); ok {
			s.world.PushEvent(event.EventEnemyKilled, &event.EnemyKilledPayload{
				Entity:  drainEntity,
				Species: component.SpeciesDrain,
				X:       pos.X,
				Y:       pos.Y,
			})
		}
	}

	event.EmitDeathBatch(s.world.Resources.Event.Queue, 0, []core.Entity{drainA, drainB})

	sources := []vmath.Point{{X: posA.X, Y: posA.Y}, {X: posB.X, Y: posB.Y}}
	area := vmath.Area{X: topLeftX, Y: topLeftY, Width: parameter.SwarmWidth, Height: parameter.SwarmHeight}
	s.applyEffect(effect, sources, area, component.SpiritCyan)

	s.fusions = append(s.fusions, pendingFusion{
		Type:    FuseSwarm,
		TargetX: midX,
		TargetY: midY,
		Timer:   parameter.SwarmFuseAnimationDuration,
	})
}

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
		return // No valid position - cancel fusion
	}

	// Update centroid to found position's header location
	cX = topLeftX + parameter.QuasarHeaderOffsetX
	cY = topLeftY + parameter.QuasarHeaderOffsetY

	area := vmath.Area{X: topLeftX, Y: topLeftY, Width: parameter.QuasarWidth, Height: parameter.QuasarHeight}
	s.applyEffect(event.FuseEffectMaterialize, sources, area, component.SpiritCyan)

	// Emit EventEnemyKilled for each drain (enables loot drops)
	for _, drainEntity := range drainEntities {
		if pos, ok := s.world.Positions.GetPosition(drainEntity); ok {
			s.world.PushEvent(event.EventEnemyKilled, &event.EnemyKilledPayload{
				Entity:  drainEntity,
				Species: component.SpeciesDrain,
				X:       pos.X,
				Y:       pos.Y,
			})
		}
	}

	if len(drainEntities) > 0 {
		event.EmitDeathBatch(s.world.Resources.Event.Queue, 0, drainEntities)
	}

	s.fusions = append(s.fusions, pendingFusion{
		Type:    FuseQuasar,
		TargetX: cX,
		TargetY: cY,
		Timer:   parameter.SpiritAnimationDuration + parameter.SpiritSafetyBuffer,
	})
}

// completeFusion triggers the creation event in the destination system
func (s *FuseSystem) completeFusion(f pendingFusion) {
	switch f.Type {
	case FuseQuasar:
		s.world.PushEvent(event.EventSpiritDespawn, nil) // Clean up spirits
		// Request spawn, delegation of creation to QuasarSystem
		s.world.PushEvent(event.EventQuasarSpawnRequest, &event.QuasarSpawnRequestPayload{
			X: f.TargetX,
			Y: f.TargetY,
		})

	case FuseSwarm:
		// Request spawn, delegation of creation to SwarmSystem
		s.world.PushEvent(event.EventSwarmSpawnRequest, &event.SwarmSpawnRequestPayload{
			X: f.TargetX,
			Y: f.TargetY,
		})
	}
}
