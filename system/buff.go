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
	statRodFired *atomic.Int64
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
	s.statRodFired = world.Resources.Status.Ints.Get("buff.rod_fired")
	s.statLauncher = world.Resources.Status.Bools.Get("buff.launcher")
	s.statChain = world.Resources.Status.Bools.Get("buff.chain")

	s.Init()
	return s
}

func (s *BuffSystem) Init() {
	s.statRod.Store(false)
	s.statRodFired.Store(0)
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
		event.EventBuffFireRequest,
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
		s.removeAllBuffs()

	case event.EventBuffFireRequest:
		s.fireAllBuffs()
	}
}

func (s *BuffSystem) Update() {
	if !s.enabled {
		return
	}

	cursorEntity := s.world.Resources.Cursor.Entity
	buffComp, ok := s.world.Components.Buff.GetComponent(cursorEntity)
	if !ok {
		return
	}

	dt := s.world.Resources.Time.DeltaTime
	for buff, active := range buffComp.Active {
		if !active {
			continue
		}
		buffComp.Cooldown[buff] -= dt
		if buffComp.Cooldown[buff] < 0 {
			buffComp.Cooldown[buff] = 0
		}
	}

	s.world.Components.Buff.SetComponent(cursorEntity, buffComp)
}

func (s *BuffSystem) addBuff(buff component.BuffType) {
	cursorEntity := s.world.Resources.Cursor.Entity
	buffComp, ok := s.world.Components.Buff.GetComponent(cursorEntity)
	if !ok {
		return
	}

	buffComp.Active[buff] = true
	switch buff {
	case component.BuffRod:
		buffComp.Cooldown[buff] = constant.BuffCooldownRod
		s.statRod.Store(true)
	case component.BuffLauncher:
		buffComp.Cooldown[buff] = constant.BuffCooldownLauncher
		s.statLauncher.Store(true)
	case component.BuffChain:
		buffComp.Cooldown[buff] = constant.BuffCooldownChain
		s.statChain.Store(true)
	default:
		return
	}

	s.world.Components.Buff.SetComponent(cursorEntity, buffComp)
}

func (s *BuffSystem) removeAllBuffs() {
	cursorEntity := s.world.Resources.Cursor.Entity
	buffComp, ok := s.world.Components.Buff.GetComponent(cursorEntity)
	if !ok {
		return
	}

	clear(buffComp.Active)
	clear(buffComp.Cooldown)
	s.world.Components.Buff.SetComponent(cursorEntity, buffComp)
	s.statRod.Store(false)
	s.statLauncher.Store(false)
	s.statChain.Store(false)
}

func (s *BuffSystem) fireAllBuffs() {
	cursorEntity := s.world.Resources.Cursor.Entity
	heatComp, ok := s.world.Components.Heat.GetComponent(cursorEntity)
	if !ok {
		return
	}
	shots := heatComp.Current / 10
	if shots == 0 {
		return
	}
	buffComp, ok := s.world.Components.Buff.GetComponent(cursorEntity)
	if !ok {
		return
	}

	for buff, active := range buffComp.Active {
		if !active {
			continue
		}

		if buffComp.Cooldown[buff] > 0 {
			continue
		}

		switch buff {
		case component.BuffRod:
			buffComp.Cooldown[buff] = constant.BuffCooldownRod
			// Fire lightning to targets, corresponding to floor(heat/10)
			rodShots := shots

			// TODO: combat priority targets
			// Quasar
			quasarEntities := s.world.Components.Quasar.GetAllEntities()
			for _, quasarEntity := range quasarEntities {
				combatComp, ok := s.world.Components.Combat.GetComponent(quasarEntity)
				if !ok {
					continue
				}

				s.world.PushEvent(event.EventVampireDrainRequest, &event.VampireDrainRequestPayload{
					TargetEntity: quasarEntity,
					Delta:        constant.VampireDrainEnergyValue,
				})
				combatComp.HitPoints--

				s.world.Components.Combat.SetComponent(quasarEntity, combatComp)

				rodShots--
				if rodShots == 0 {
					break
				}
			}

			// Drains
			drainEntities := s.world.Components.Drain.GetAllEntities()
			for _, drainEntity := range drainEntities {
				if rodShots == 0 {
					break
				}
				// TODO: shoot at closest ones
				// drainPos, ok := s.world.Positions.GetPosition(drainEntity)
				// if !ok {
				// 	continue
				// }
				s.world.PushEvent(event.EventVampireDrainRequest, &event.VampireDrainRequestPayload{
					TargetEntity: drainEntity,
					Delta:        constant.VampireDrainEnergyValue,
				})
				rodShots--
				s.statRodFired.Add(1)
			}

		}
	}
}