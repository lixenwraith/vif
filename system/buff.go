package system

import (
	"slices"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/vmath"
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
	cursorPos, ok := s.world.Positions.GetPosition(cursorEntity)
	if !ok {
		return
	}
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

			// 1. Filter eligible targets
			combatEntities := s.world.Components.Combat.GetAllEntities()
			candidateTargetEntities := make([]core.Entity, 0, len(combatEntities))
			for _, combatEntity := range combatEntities {
				combatComp, ok := s.world.Components.Combat.GetComponent(combatEntity)
				if !ok {
					continue
				}
				if combatComp.OwnerEntity == cursorEntity {
					continue
				}
				candidateTargetEntities = append(candidateTargetEntities, combatEntity)
			}

			// 2. Prioritize composite targets
			compositeIndex := 0
			for scanIndex := range len(candidateTargetEntities) {
				if s.world.Components.Header.HasEntity(candidateTargetEntities[scanIndex]) {
					compositeIndex++
					if compositeIndex < scanIndex {
						candidateTargetEntities[scanIndex], candidateTargetEntities[compositeIndex] = candidateTargetEntities[compositeIndex], candidateTargetEntities[scanIndex]
					}
				}
			}

			// 3. Set hit entity of composite targets (closest member entity)
			finalTargetEntities := make([]core.Entity, 0, len(candidateTargetEntities))
			finalHitEntities := make([]core.Entity, 0, len(candidateTargetEntities))
			for i := range min(shots, compositeIndex) {
				headerComp, ok := s.world.Components.Header.GetComponent(candidateTargetEntities[i])
				if !ok {
					continue
				}
				var hitEntityCandidate core.Entity
				var shortestMemberDistance int64
				for _, memberEntry := range headerComp.MemberEntries {
					memberPos, ok := s.world.Positions.GetPosition(memberEntry.Entity)
					if !ok {
						continue
					}
					memberDistance := vmath.MagnitudeEuclidean(
						vmath.FromInt(cursorPos.X-memberPos.X),
						vmath.FromInt(cursorPos.Y-memberPos.Y),
					)
					if hitEntityCandidate == 0 || memberDistance < shortestMemberDistance {
						hitEntityCandidate = memberEntry.Entity
						shortestMemberDistance = memberDistance
					}
				}
				if hitEntityCandidate != 0 {
					finalTargetEntities = append(finalTargetEntities, candidateTargetEntities[i])
					finalHitEntities = append(finalHitEntities, hitEntityCandidate)
				}
			}

			// 4. Fill the rest with closest non-composite entities
			type entityDistance struct {
				entity   core.Entity
				distance int64
			}
			nonCompositeTargetEntities := make([]entityDistance, 0, len(candidateTargetEntities))
			for i := compositeIndex; i < len(candidateTargetEntities); i++ {
				targetPos, ok := s.world.Positions.GetPosition(candidateTargetEntities[i])
				if !ok {
					continue
				}
				nonCompositeTargetEntities = append(nonCompositeTargetEntities, entityDistance{
					entity: candidateTargetEntities[i],
					distance: vmath.MagnitudeEuclidean(
						vmath.FromInt(cursorPos.X-targetPos.X),
						vmath.FromInt(cursorPos.Y-targetPos.Y),
					),
				})
			}
			slices.SortStableFunc(nonCompositeTargetEntities, func(i, j entityDistance) int {
				if i.distance < j.distance {
					return -1
				} else if i.distance > j.distance {
					return 1
				}
				return 0
			})
			for i := range min(shots, len(candidateTargetEntities)) - len(finalTargetEntities) {
				finalTargetEntities = append(finalTargetEntities, nonCompositeTargetEntities[i].entity)
				finalHitEntities = append(finalHitEntities, nonCompositeTargetEntities[i].entity)
			}

			// 5. Fire lightning to targets
			for i := range len(finalTargetEntities) {
				s.world.PushEvent(event.EventCombatAttackDirectRequest, &event.CombatAttackDirectRequestPayload{
					AttackType:   component.CombatAttackLightning,
					OwnerEntity:  cursorEntity,
					OriginEntity: cursorEntity,
					TargetEntity: finalTargetEntities[i],
					HitEntity:    finalHitEntities[i],
				})
			}

		}
		s.world.Components.Buff.SetComponent(cursorEntity, buffComp)
	}
}