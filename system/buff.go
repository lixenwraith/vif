package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
)

// BuffSystem manages the cursor gained effects and abilities, it resets on energy getting to or crossing zero
type BuffSystem struct {
	world *engine.World

	// Runtime state
	active bool

	// Telemetry
	statRod      *atomic.Bool
	statLauncher *atomic.Bool
	statChain    *atomic.Bool

	enabled bool
}

// NewBuffSystem creates a new quasar system
func NewBuffSystem(world *engine.World) engine.System {
	s := &BuffSystem{
		world: world,
	}

	s.statRod = world.Resources.Status.Bools.Get("buff.rod")
	s.statLauncher = world.Resources.Status.Bools.Get("buff.launcher")
	s.statChain = world.Resources.Status.Bools.Get("buff.chain")

	s.Init()
	return s
}

func (s *BuffSystem) Init() {
	s.statRod.Store(false)
	s.statLauncher.Store(false)
	s.statChain.Store(false)
	s.enabled = true
}

// Name returns system's name
func (s *BuffSystem) Name() string {
	return "buff"
}

func (s *BuffSystem) Priority() int {
	return constant.PriorityBuff
}

func (s *BuffSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventBuffAddRequest,
		event.EventEnergyCrossedZeroNotification,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *BuffSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
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
	case event.EventBuffAddRequest:
		if payload, ok := ev.Payload.(*event.BuffAddRequestPayload); ok {
			s.addBuff(payload.Buff)
		}

	case event.EventEnergyCrossedZeroNotification:
	}
}

func (s *BuffSystem) Update() {
	if !s.enabled {
		return
	}
}

func (s *BuffSystem) addBuff(buff component.BuffType) {
	cursorEntity := s.world.Resources.Cursor.Entity
	buffComp, ok := s.world.Components.Buff.GetComponent(cursorEntity)
	if !ok {
		return
	}

	switch buff {
	case component.BuffRod:
		s.statRod.Store(true)
	case component.BuffLauncher:
		s.statLauncher.Store(true)
	case component.BuffChain:
		s.statChain.Store(true)
	default:
		return
	}

	buffComp.Active[buff] = true
	s.world.Components.Buff.SetComponent(cursorEntity, buffComp)
}