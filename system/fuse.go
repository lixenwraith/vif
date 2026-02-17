package system

import (
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/vmath"
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
	s.rng = vmath.NewFastRand(uint64(s.world.Resources.Time.RealTime.UnixNano()))
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
func (s *FuseSystem) applyEffect(effect event.FuseEffect, sources []core.Point, area core.Area, spiritColor component.SpiritColor) {
	switch effect {
	case event.FuseEffectSpirit:
		s.effectSpiritArea(sources, area, spiritColor)
	case event.FuseEffectMaterialize:
		s.effectMaterialize(area)
	default:
		s.effectSpiritArea(sources, area, spiritColor)
	}
}

func (s *FuseSystem) effectSpiritArea(sources []core.Point, area core.Area, color component.SpiritColor) {
	for i, src := range sources {
		dest := vmath.AreaDistributePoint(area, i, s.rng)

		s.world.PushEvent(event.EventSpiritSpawn, &event.SpiritSpawnRequestPayload{
			StartX:    src.X,
			StartY:    src.Y,
			TargetX:   dest.X,
			TargetY:   dest.Y,
			Char:      visual.DrainChar,
			BaseColor: color,
		})
	}
}

func (s *FuseSystem) effectMaterialize(area core.Area) {
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

	event.EmitDeathBatch(s.world.Resources.Event.Queue, 0, []core.Entity{drainA, drainB})

	sources := []core.Point{{X: posA.X, Y: posA.Y}, {X: posB.X, Y: posB.Y}}
	area := core.Area{X: topLeftX, Y: topLeftY, Width: parameter.SwarmWidth, Height: parameter.SwarmHeight}
	s.applyEffect(effect, sources, area, component.SpiritCyan)

	s.fusions = append(s.fusions, pendingFusion{
		Type:    FuseSwarm,
		TargetX: midX,
		TargetY: midY,
		Timer:   parameter.SwarmFuseAnimationDuration,
	})
}

func (s *FuseSystem) handleQuasarFuse() {
	drains := s.world.Components.Drain.GetAllEntities()
	sources := make([]core.Point, 0, len(drains))

	for _, e := range drains {
		if pos, ok := s.world.Positions.GetPosition(e); ok {
			sources = append(sources, core.Point{X: pos.X, Y: pos.Y})
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

	area := core.Area{X: topLeftX, Y: topLeftY, Width: parameter.QuasarWidth, Height: parameter.QuasarHeight}
	s.applyEffect(event.FuseEffectMaterialize, sources, area, component.SpiritCyan)

	// Cleanup materializers and drains
	mats := s.world.Components.Materialize.GetAllEntities()
	for _, e := range mats {
		if m, ok := s.world.Components.Materialize.GetComponent(e); ok && m.Type == component.SpawnTypeDrain {
			s.world.DestroyEntity(e)
		}
	}

	// Emit EventEnemyKilled for each drain (enables loot drops)
	for _, e := range drains {
		if pos, ok := s.world.Positions.GetPosition(e); ok {
			s.world.PushEvent(event.EventEnemyKilled, &event.EnemyKilledPayload{
				Entity:  e,
				Species: component.SpeciesDrain,
				X:       pos.X,
				Y:       pos.Y,
			})
		}
	}

	if len(drains) > 0 {
		event.EmitDeathBatch(s.world.Resources.Event.Queue, 0, drains)
	}

	s.fusions = append(s.fusions, pendingFusion{
		Type:    FuseQuasar,
		TargetX: cX,
		TargetY: cY,
		Timer:   parameter.SpiritAnimationDuration + parameter.SpiritSafetyBuffer,
	})
}

func (s *FuseSystem) spawnConvergenceSpirit(startX, startY, targetX, targetY int) {
	s.world.PushEvent(event.EventSpiritSpawn, &event.SpiritSpawnRequestPayload{
		StartX:    startX,
		StartY:    startY,
		TargetX:   targetX,
		TargetY:   targetY,
		Char:      visual.DrainChar,
		BaseColor: component.SpiritCyan,
	})
}

// completeFusion triggers the creation event in the destination system
func (s *FuseSystem) completeFusion(f pendingFusion) {
	switch f.Type {
	case FuseQuasar:
		s.world.PushEvent(event.EventSpiritDespawn, nil) // Clean up spirits
		// Request spawn, delegation of creation to QuasarSystem
		s.world.PushEvent(event.EventQuasarSpawnRequest, &event.QuasarSpawnRequestPayload{
			SpawnX: f.TargetX,
			SpawnY: f.TargetY,
		})

	case FuseSwarm:
		// Request spawn, delegation of creation to SwarmSystem
		s.world.PushEvent(event.EventSwarmSpawnRequest, &event.SwarmSpawnRequestPayload{
			SpawnX: f.TargetX,
			SpawnY: f.TargetY,
		})
	}
}