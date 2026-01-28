package system

import (
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
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
	s.enabled = true
}

// Name returns system's name
func (s *FuseSystem) Name() string {
	return "fuse"
}

func (s *FuseSystem) Priority() int {
	return constant.PriorityFuse
}

func (s *FuseSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventFuseQuasarRequest,
		event.EventQuasarDestroyed, // Listen for cleanup to resume drains
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

	case event.EventQuasarDestroyed:
		// FuseSystem manages the drain lifecycle during the Quasar phase: when Quasar dies (for any reason), resume drains
		s.world.PushEvent(event.EventDrainResume, nil)

	case event.EventFuseSwarmRequest:
		if payload, ok := ev.Payload.(*event.FuseSwarmRequestPayload); ok {
			s.handleSwarmFuse(payload.DrainA, payload.DrainB)
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

			// Remove completed fusion
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

// handleQuasarFuse initiates the mass fusion of all drains
func (s *FuseSystem) handleQuasarFuse() {
	// 1. Signal DrainSystem to stop spawning
	s.world.PushEvent(event.EventDrainPause, nil)

	// 2. Collect active drains and calculate centroid
	drains := s.world.Components.Drain.GetAllEntities()
	coords := make([]int, 0, len(drains)*2)

	// Only calculate centroid of valid, positioned drains
	for _, e := range drains {
		if pos, ok := s.world.Positions.GetPosition(e); ok {
			coords = append(coords, pos.X, pos.Y)
		}
	}

	cX, cY := vmath.CalculateCentroid(coords)

	// Fallback to center screen if no drains found
	if len(coords) == 0 {
		config := s.world.Resources.Config
		cX = config.GameWidth / 2
		cY = config.GameHeight / 2
	}

	// 3. Visuals: Spawn spirits from drains to centroid
	// Re-iterate to spawn spirits (using valid coords gathered previously or fresh query)
	// Fresh query safer in case of concurrent mods, though unlikely in single thread
	for _, e := range drains {
		if pos, ok := s.world.Positions.GetPosition(e); ok {
			s.world.PushEvent(event.EventSpiritSpawn, &event.SpiritSpawnRequestPayload{
				StartX:    pos.X,
				StartY:    pos.Y,
				TargetX:   cX,
				TargetY:   cY,
				Char:      constant.DrainChar,
				BaseColor: component.SpiritCyan,
			})
		}
	}

	// 4. Cleanup source entities
	// Cleanup Pending Materializers (Drains that were about to spawn)
	mats := s.world.Components.Materialize.GetAllEntities()
	for _, e := range mats {
		if m, ok := s.world.Components.Materialize.GetComponent(e); ok && m.Type == component.SpawnTypeDrain {
			s.world.DestroyEntity(e)
		}
	}

	// Destroy all existing drains (silent death)
	if len(drains) > 0 {
		event.EmitDeathBatch(s.world.Resources.Event.Queue, 0, drains)
	}

	// 5. Queue Fusion
	s.fusions = append(s.fusions, pendingFusion{
		Type:    FuseQuasar,
		TargetX: cX,
		TargetY: cY,
		Timer:   constant.SpiritAnimationDuration + constant.SpiritSafetyBuffer,
	})
}

// handleSwarmFuse initiates fusion of two specific drains
func (s *FuseSystem) handleSwarmFuse(drainA, drainB core.Entity) {
	// Get positions before destruction
	posA, okA := s.world.Positions.GetPosition(drainA)
	posB, okB := s.world.Positions.GetPosition(drainB)
	if !okA || !okB {
		return
	}

	// Calculate midpoint
	midX := (posA.X + posB.X) / 2
	midY := (posA.Y + posB.Y) / 2

	// Destroy both drains silently
	event.EmitDeathBatch(s.world.Resources.Event.Queue, 0, []core.Entity{drainA, drainB})

	// Visuals: Spawn spirits converging to midpoint
	s.spawnConvergenceSpirit(posA.X, posA.Y, midX, midY)
	s.spawnConvergenceSpirit(posB.X, posB.Y, midX, midY)

	// Queue Fusion
	s.fusions = append(s.fusions, pendingFusion{
		Type:    FuseSwarm,
		TargetX: midX,
		TargetY: midY,
		Timer:   constant.SwarmFuseAnimationDuration,
	})
}

func (s *FuseSystem) spawnConvergenceSpirit(startX, startY, targetX, targetY int) {
	s.world.PushEvent(event.EventSpiritSpawn, &event.SpiritSpawnRequestPayload{
		StartX:    startX,
		StartY:    startY,
		TargetX:   targetX,
		TargetY:   targetY,
		Char:      constant.DrainChar,
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