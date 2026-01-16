package system

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// SpiritSystem manages converging visual effect entities
// Spirits travel from start to target position over a duration
// Self-destruct on arrival; EventSpiritDespawn provides safety cleanup
type SpiritSystem struct {
	world *engine.World

	// Deferred destruction for final frame visibility
	destroyNextTick []core.Entity

	enabled bool
}

func NewSpiritSystem(world *engine.World) engine.System {
	s := &SpiritSystem{
		world: world,
	}
	s.Init()
	return s
}

func (s *SpiritSystem) Init() {
	s.destroyNextTick = s.destroyNextTick[:0]
	s.enabled = true
}

func (s *SpiritSystem) Priority() int {
	return constant.PrioritySpirit
}

func (s *SpiritSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventSpiritSpawn,
		event.EventSpiritDespawn,
		event.EventGameReset,
	}
}

func (s *SpiritSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.destroyAllSpirits()
		s.Init()
		return
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventSpiritSpawn:
		if payload, ok := ev.Payload.(*event.SpiritSpawnPayload); ok {
			s.spawnSpirit(payload)
		}

	case event.EventSpiritDespawn:
		s.destroyAllSpirits()
	}
}

func (s *SpiritSystem) Update() {
	if !s.enabled {
		return
	}

	// Destroy entities marked last tick
	for _, entity := range s.destroyNextTick {
		s.destroySpirit(entity)
	}
	s.destroyNextTick = s.destroyNextTick[:0]

	entities := s.world.Components.Spirit.GetAllEntities()
	if len(entities) == 0 {
		return
	}

	var toDestroy []core.Entity

	for _, entity := range entities {
		spirit, ok := s.world.Components.Spirit.GetComponent(entity)
		if !ok {
			continue
		}

		// Advance progress
		spirit.Progress += spirit.Speed
		if spirit.Progress >= vmath.Scale {
			spirit.Progress = vmath.Scale
			// Mark for destruction next tick - allows final frame render
			s.destroyNextTick = append(s.destroyNextTick, entity)
		}
		s.world.Components.Spirit.SetComponent(entity, spirit)
	}

	// Destroy completed spirits
	for _, entity := range toDestroy {
		s.destroySpirit(entity)
	}
}

// spawnSpirit creates spirit entities and their components, without position store registration (vfx only, no world interaction)
func (s *SpiritSystem) spawnSpirit(p *event.SpiritSpawnPayload) {
	entity := s.world.CreateEntity()

	// Speed = Progress increment per tick for all spirits to arrive together
	// Lerp handles distance normalization - progress 0→1 over duration
	durationTicks := int64(constant.SpiritAnimationDuration / constant.GameUpdateInterval)
	if durationTicks == 0 {
		durationTicks = 1
	}
	// Adding one extra tick for the last position frame to be visible
	// speed := vmath.Scale / (durationTicks + 1)
	speed := vmath.Scale / durationTicks

	// Calculate Spin: ~1.5 rotations (Scale * 1.5)
	// Alternating direction based on position parity to create chaotic implosion
	spinMag := int64(vmath.Scale*3) / 2
	if (p.StartX^p.StartY)&1 != 0 {
		spinMag = -spinMag
	}

	s.world.Components.Protection.SetComponent(entity, component.ProtectionComponent{
		Mask: component.ProtectAll,
	})

	s.world.Components.Spirit.SetComponent(entity, component.SpiritComponent{
		StartX:     vmath.FromInt(p.StartX),
		StartY:     vmath.FromInt(p.StartY),
		TargetX:    vmath.FromInt(p.TargetX),
		TargetY:    vmath.FromInt(p.TargetY),
		Progress:   0,
		Speed:      speed,
		Spin:       spinMag,
		Rune:       p.Char,
		BaseColor:  p.BaseColor,
		BlinkColor: p.BlinkColor,
	})
}

func (s *SpiritSystem) destroySpirit(entity core.Entity) {
	s.world.Components.Protection.RemoveEntity(entity)
	s.world.Components.Spirit.RemoveEntity(entity)
	s.world.DestroyEntity(entity)
}

func (s *SpiritSystem) destroyAllSpirits() {
	entities := s.world.Components.Spirit.GetAllEntities()
	for _, entity := range entities {
		s.destroySpirit(entity)
	}
}