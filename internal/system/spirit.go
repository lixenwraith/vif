package system

import (
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// SpiritSystem manages converging visual effect entities
// Spirits travel from start to target position over a duration
// Self-destruct on arrival; EventSpiritDespawn provides safety cleanup
// Stamped: spirits are created in the requesting event's domain (D-7)
type SpiritSystem struct {
	world *engine.World

	// Deferred destruction for final frame visibility
	destroyNextTick []core.Entity
	buffers         bufferTelemetry

	enabled bool
}

func NewSpiritSystem(world *engine.World) engine.System {
	s := &SpiritSystem{
		world: world,
	}
	s.buffers = newBufferTelemetry(world.Resources.Status, "spirit", "destroy_next_tick")
	s.Init()
	return s
}

func (s *SpiritSystem) Init() {
	s.destroyNextTick = s.destroyNextTick[:0]
	s.buffers.Reset()
	s.enabled = true
}

// Name returns system's name
func (s *SpiritSystem) Name() string {
	return "spirit"
}

func (s *SpiritSystem) Priority() int {
	return parameter.PrioritySpirit
}

func (s *SpiritSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventSpiritSpawnRequest,
		event.EventSpiritDespawnRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

func (s *SpiritSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameResetRequest {
		s.destroyAllSpirits()
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
	case event.EventSpiritSpawnRequest:
		if payload, ok := ev.Payload.(*event.SpiritSpawnRequestPayload); ok {
			s.spawnSpirit(payload, ev.Domain)
		}

	case event.EventSpiritDespawnRequest:
		s.destroyAllSpirits()
	}
}

func (s *SpiritSystem) Update() {
	if !s.enabled {
		return
	}

	// Destroy entities marked last tick
	s.world.DestroyEntitiesBatch(s.destroyNextTick)
	s.destroyNextTick = s.destroyNextTick[:0]

	spirits := s.world.Components.Spirit
	if spirits.CountEntities() == 0 {
		return
	}

	for _, entity := range spirits.Entities() {
		spirit, ok := spirits.GetPtr(entity)
		if !ok {
			continue
		}

		// Advance progress
		spirit.Progress += spirit.Speed
		if spirit.Progress >= 1.0 {
			spirit.Progress = 1.0
			// Mark for destruction next tick - allows final frame render
			s.destroyNextTick = append(s.destroyNextTick, entity)
		}
	}
	s.buffers.Observe(0, len(s.destroyNextTick))
}

// spawnSpirit creates spirit entities and their components in the requesting domain, without position store registration (vfx only, no world interaction)
func (s *SpiritSystem) spawnSpirit(p *event.SpiritSpawnRequestPayload, domain core.Domain) {
	entity := s.world.CreateEntity(domain)

	// Speed = Progress increment per tick for all spirits to arrive together
	// Lerp handles distance normalization - progress 0→1 over duration
	durationTicks := int64(parameter.SpiritAnimationDuration / parameter.GameUpdateInterval)
	if durationTicks <= 0 {
		durationTicks = 1
	}
	speed := 1.0 / float64(durationTicks)

	// Calculate spin: 1.5 rotations in radians.
	// Alternating direction based on position parity to create chaotic implosion
	spinMag := 1.5 * vmath.TwoPi
	if (p.StartX^p.StartY)&1 != 0 {
		spinMag = -spinMag
	}

	s.world.Components.Protection.SetComponent(entity, component.ProtectionComponent{
		Mask: component.ProtectAll ^ component.ProtectFromDeath,
	})

	startX, startY := (vmath.Point{X: p.StartX, Y: p.StartY}).CenterF()
	targetX, targetY := (vmath.Point{X: p.TargetX, Y: p.TargetY}).CenterF()

	s.world.Components.Spirit.SetComponent(entity, component.SpiritComponent{
		StartX:    startX,
		StartY:    startY,
		TargetX:   targetX,
		TargetY:   targetY,
		Progress:  0,
		Speed:     speed,
		Spin:      spinMag,
		Rune:      p.Char,
		BaseColor: p.BaseColor,
	})
}

func (s *SpiritSystem) destroyAllSpirits() {
	// Batch destruction mutates the live spirit slice, so detach it first.
	entities := s.world.Components.Spirit.GetAllEntities()
	s.world.DestroyEntitiesBatch(entities)
}
