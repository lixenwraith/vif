package system

import (
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// SpiritSystem manages converging visual effect entities
// Spirits travel from start to target position over their lifetime timer, which
// ends them after the arrival frame; EventSpiritDespawn provides safety cleanup
// Stamped: spirits are created in the requesting event's domain (D-7)
type SpiritSystem struct {
	world *engine.World

	toggle
}

func NewSpiritSystem(world *engine.World) engine.System {
	s := &SpiritSystem{
		world: world,
	}
	s.Init()
	return s
}

func (s *SpiritSystem) Init() {
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

// Update is empty: a spirit's lifetime timer drives and ends its flight
func (s *SpiritSystem) Update() {}

// spawnSpirit creates spirit entities and their components in the requesting domain, without position store registration (vfx only, no world interaction)
func (s *SpiritSystem) spawnSpirit(p *event.SpiritSpawnRequestPayload, domain core.Domain) {
	entity := s.world.CreateEntity(domain)

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
		Spin:      spinMag,
		Rune:      p.Char,
		BaseColor: p.BaseColor,
	})
	// One flight time for every spirit so they arrive together; Lerp handles
	// distance. The lifetime holds one tick past arrival so the arrival frame draws
	// before the death batch settles between ticks.
	armTimer(s.world, entity, component.TimerLifetime, parameter.SpiritAnimationDuration+parameter.GameUpdateInterval)
}

func (s *SpiritSystem) destroyAllSpirits() {
	// Batch destruction mutates the live spirit slice, so detach it first.
	entities := s.world.Components.Spirit.GetAllEntities()
	s.world.DestroyEntitiesBatch(entities)
}
