package system

import (
	"fmt"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/physics"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// CombatSystem manages interaction logic with combat entities
type CombatSystem struct {
	world *engine.World

	// Random source for knockback impulse randomization
	rng *vmath.FastRand

	// Telemetry
	statActive *atomic.Bool
	statCount  *atomic.Int64

	enabled bool
}

// NewCombatSystem creates a new quasar system
func NewCombatSystem(world *engine.World) engine.System {
	s := &CombatSystem{
		world: world,
	}

	s.statActive = world.Resources.Status.Bools.Get("combat.active")
	s.statCount = world.Resources.Status.Ints.Get("combat.count")

	s.Init()
	return s
}

func (s *CombatSystem) Init() {
	s.rng = vmath.NewFastRand(uint64(s.world.Resources.Time.RealTime.UnixNano()))
	s.statActive.Store(false)
	s.statCount.Store(0)
	s.enabled = true
}

// Name returns system's name
func (s *CombatSystem) Name() string {
	return "combat"
}

func (s *CombatSystem) Priority() int {
	return constant.PriorityCombat
}

func (s *CombatSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventCombatFullKnockbackRequest,
		event.EventCombatHitRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *CombatSystem) HandleEvent(ev event.GameEvent) {
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
	case event.EventCombatFullKnockbackRequest:
		if payload, ok := ev.Payload.(*event.CombatKnockbackRequestPayload); ok {
			s.applyFullKnockback(payload.OriginEntity, payload.TargetEntity)
		}

	case event.EventCombatHitRequest:
		if payload, ok := ev.Payload.(*event.CombatHitRequestPayload); ok {
			s.applyHit(payload)
		}
	}
}

func (s *CombatSystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime

	combatEntities := s.world.Components.Combat.GetAllEntities()
	for _, combatEntity := range combatEntities {
		combatComp, ok := s.world.Components.Combat.GetComponent(combatEntity)
		if !ok {
			continue
		}

		// Update kinetic immunity timer
		if combatComp.KineticImmunityRemaining > 0 {
			combatComp.KineticImmunityRemaining -= dt
			if combatComp.KineticImmunityRemaining < 0 {
				combatComp.KineticImmunityRemaining = 0
			}
		}

		// Update damage immunity timer
		if combatComp.DamageImmunityRemaining > 0 {
			combatComp.DamageImmunityRemaining -= dt
			if combatComp.DamageImmunityRemaining < 0 {
				combatComp.DamageImmunityRemaining = 0
			}
		}

		// Update hit flash timer
		if combatComp.HitFlashRemaining > 0 {
			combatComp.HitFlashRemaining -= dt
			if combatComp.HitFlashRemaining < 0 {
				combatComp.HitFlashRemaining = 0
			}
		}

		s.world.Components.Combat.SetComponent(combatEntity, combatComp)
	}

}

// applyFullKnockback applies radial impulse when drain overlaps shield
func (s *CombatSystem) applyFullKnockback(
	originEntity, targetEntity core.Entity,
) {
	targetCombatComp, ok := s.world.Components.Combat.GetComponent(targetEntity)
	if !ok {
		return
	}
	if targetCombatComp.KineticImmunityRemaining > 0 {
		return
	}

	s.world.DebugPrint(fmt.Sprintf("%s", targetCombatComp.KineticImmunityRemaining))

	originPos, ok := s.world.Positions.GetPosition(originEntity)
	if !ok {
		return
	}
	targetPos, ok := s.world.Positions.GetPosition(targetEntity)
	if !ok {
		return
	}

	// Radial direction: origin → target (e.g. cursor shield pushes drain outward)
	// TODO: change to cell-center physics from grid coords
	radialX := vmath.FromInt(targetPos.X - originPos.X)
	radialY := vmath.FromInt(targetPos.Y - originPos.Y)

	// TODO: fix this
	kineticComp, ok := s.world.Components.Kinetic.GetComponent(targetEntity)
	if !ok {
		return
	}

	if physics.ApplyCollision(&kineticComp.Kinetic, radialX, radialY, &physics.ShieldToDrain, s.rng) {
		s.world.Components.Kinetic.SetComponent(targetEntity, kineticComp)
	}
	// TODO: check above condition implications
	targetCombatComp.KineticImmunityRemaining = constant.CombatKnockbackImmunityInterval
	s.world.Components.Combat.SetComponent(targetEntity, targetCombatComp)
}

// applyHit applies combat hit to a target
func (s *CombatSystem) applyHit(payload *event.CombatHitRequestPayload) {
	// TODO: telemetry
	targetCombatComp, ok := s.world.Components.Combat.GetComponent(payload.TargetEntity)
	if !ok {
		return
	}
	// originCombatComp, ok := s.world.Components.Combat.GetComponent(payload.OriginEntity)
	// if !ok {
	// 	return
	// }

	// Generate combat matrix key
	originCombatType := payload.OriginCombatType
	var targetCombatType component.CombatEntityType
	// Target defensive checks and type determination
	if payload.TargetEntity != payload.HitEntity {
		// Composite hit check
		memberComp, ok := s.world.Components.Member.GetComponent(payload.HitEntity)
		if !ok {
			return
		}
		headerEntity := memberComp.HeaderEntity
		headerComp, ok := s.world.Components.Header.GetComponent(headerEntity)
		if !ok {
			return
		}

		switch headerComp.Behavior {
		case component.BehaviorQuasar:
			targetCombatType = component.CombatTypeQuasar
		case component.BehaviorSwarm:
			return // Future
		case component.BehaviorStorm:
			return // Future
		default:
			return
		}
	} else {
		switch {
		case payload.TargetEntity == s.world.Resources.Cursor.Entity:
			targetCombatType = component.CombatTypeCursor
		case s.world.Components.Drain.HasEntity(payload.TargetEntity):
			targetCombatType = component.CombatTypeDrain
		default:
			return
		}
	}
	combatMatrixKey := component.CombatMatrixKey{originCombatType, targetCombatType}
	combatProfile, ok := component.CombatMatrix[combatMatrixKey]
	if !ok {
		return
	}

	// Apply damage hit
	if targetCombatComp.DamageImmunityRemaining == 0 && combatProfile.DamageValue != 0 {
		switch combatProfile.DamageType {
		case component.CombatDamageDirect:
			targetCombatComp.HitPoints -= combatProfile.DamageValue
			if targetCombatComp.HitPoints < 0 {
				targetCombatComp.HitPoints = 0
			}

			// TODO: secondary effect in combat profile?
			s.world.PushEvent(event.EventVampireDrainRequest, &event.VampireDrainRequestPayload{
				TargetEntity: payload.HitEntity,
				Delta:        constant.VampireEnergyDrainAmount,
			})
		}

		targetCombatComp.HitFlashRemaining = constant.CombatHitFlashDuration
		targetCombatComp.DamageImmunityRemaining = constant.CombatDamageImmunityDuration

		if targetCombatComp.HitPoints == 0 {
			s.world.Components.Combat.SetComponent(payload.TargetEntity, targetCombatComp)
			// TODO: kill
			return
		}
	}

	// Apply kinetic hit/collision
	if targetCombatComp.KineticImmunityRemaining == 0 && !targetCombatComp.IsEnraged {
		switch combatProfile.HitType {
		case component.CombatHitMass:
			targetKineticComp, ok := s.world.Components.Kinetic.GetComponent(payload.TargetEntity)
			if !ok {
				return
			}
			originKineticComp, ok := s.world.Components.Kinetic.GetComponent(payload.OriginEntity)
			if !ok {
				return
			}

			originVelX := originKineticComp.VelX
			originVelY := originKineticComp.VelY

			if payload.TargetEntity == payload.HitEntity {
				// Non-composite collision
				if physics.ApplyCollision(&targetKineticComp.Kinetic, originVelX, originVelY, combatProfile.CollisionProfile, s.rng) {
					s.world.Components.Kinetic.SetComponent(payload.TargetEntity, targetKineticComp)
				}
			} else {
				headerPos, ok := s.world.Positions.GetPosition(payload.TargetEntity)
				if !ok {
					return
				}
				hitPos, ok := s.world.Positions.GetPosition(payload.HitEntity)
				if !ok {
					return
				}

				offsetX := hitPos.X - headerPos.X
				offsetY := hitPos.Y - headerPos.Y

				// Composite collision
				if physics.ApplyOffsetCollision(
					&targetKineticComp.Kinetic,
					originVelX, originVelY,
					offsetX, offsetY,
					combatProfile.CollisionProfile,
					s.rng,
				) {
					s.world.Components.Kinetic.SetComponent(payload.TargetEntity, targetKineticComp)
				}
			}
		}
	}

	s.world.Components.Combat.SetComponent(payload.TargetEntity, targetCombatComp)
}