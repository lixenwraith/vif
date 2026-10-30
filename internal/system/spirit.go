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
// ends them after the arrival frame; EventSpiritDespawn provides safety cleanup.
// A spirit naming a species lands it as it arrives, so the spawn rides its spirit.
// Stamped: spirits are created in the requesting event's domain (D-7)
type SpiritSystem struct {
	world *engine.World

	toggle
}

func NewSpiritSystem(world *engine.World) engine.System {
	s := &SpiritSystem{
		world: world,
	}
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
	}
}

func (s *SpiritSystem) HandleEvent(ev event.GameEvent) {

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

// Update sends the arrival of each spirit naming a species, the tick its deadline
// ends its flight, in the spirit's domain; its lifetime timer drives and ends the
// flight's frames
func (s *SpiritSystem) Update() {
	if !s.enabled {
		return
	}

	spirits := s.world.Components.Spirit
	if spirits.CountEntities() == 0 {
		return
	}
	for _, e := range spirits.Entities() {
		spirit, ok := spirits.GetPtr(e)
		if !ok || spirit.Species == component.SpeciesNone {
			continue
		}
		if timer, ok := s.world.Components.Timer.GetPtr(e); !ok || !timer.Expired(component.TimerDeadline) {
			continue
		}
		s.world.PushEventDomain(event.EventSpiritArrived, &event.SpiritArrivedPayload{
			X:       spirit.LandX,
			Y:       spirit.LandY,
			Species: spirit.Species,
		}, e.Domain())
		// Landed: the arrival frame still draws, and nothing lands twice
		spirit.Species = component.SpeciesNone
	}
}

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

	startX, startY := (vmath.Point{X: p.StartX, Y: p.StartY}).Center()
	targetX, targetY := (vmath.Point{X: p.TargetX, Y: p.TargetY}).Center()

	s.world.Components.Spirit.SetComponent(entity, component.SpiritComponent{
		StartX:    startX,
		StartY:    startY,
		TargetX:   targetX,
		TargetY:   targetY,
		Spin:      spinMag,
		Rune:      p.Char,
		BaseColor: p.BaseColor,
		Species:   p.Species,
		LandX:     p.LandX,
		LandY:     p.LandY,
	})
	// One flight time for every spirit so they arrive together; Lerp handles
	// distance. The lifetime holds one tick past arrival so the arrival frame draws
	// before the tick that expires it settles the death batch.
	armTimer(s.world, entity, component.TimerLifetime, parameter.SpiritAnimationDuration+s.world.Resources.Config.TickInterval)
	// The deadline ends the flight on the arrival frame's tick, which lands the species
	if p.Species != component.SpeciesNone {
		armTimer(s.world, entity, component.TimerDeadline, parameter.SpiritAnimationDuration)
	}
}

func (s *SpiritSystem) destroyAllSpirits() {
	// Batch destruction mutates the live spirit slice, so detach it first.
	entities := s.world.Components.Spirit.GetAllEntities()
	s.world.DestroyEntitiesBatch(entities)
}
