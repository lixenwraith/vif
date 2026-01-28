package system

import (
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
		event.EventCombatAttackDirectRequest,
		event.EventCombatAttackAreaRequest,
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
	// case event.EventCombatFullKnockbackRequest:
	// 	if payload, ok := ev.Payload.(*event.CombatKnockbackRequestPayload); ok {
	// 		s.applyFullKnockback(payload.OriginEntity, payload.TargetEntity)
	// 	}

	case event.EventCombatAttackDirectRequest:
		if payload, ok := ev.Payload.(*event.CombatAttackDirectRequestPayload); ok {
			s.applyHitDirect(payload)
		}

	case event.EventCombatAttackAreaRequest:
		if payload, ok := ev.Payload.(*event.CombatAttackAreaRequestPayload); ok {
			s.applyHitArea(payload)
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
		if combatComp.RemainingKineticImmunity > 0 {
			combatComp.RemainingKineticImmunity -= dt
			if combatComp.RemainingKineticImmunity < 0 {
				combatComp.RemainingKineticImmunity = 0
			}
		}

		// Update damage immunity timer
		if combatComp.RemainingDamageImmunity > 0 {
			combatComp.RemainingDamageImmunity -= dt
			if combatComp.RemainingDamageImmunity < 0 {
				combatComp.RemainingDamageImmunity = 0
			}
		}

		// Update hit flash timer
		if combatComp.RemainingHitFlash > 0 {
			combatComp.RemainingHitFlash -= dt
			if combatComp.RemainingHitFlash < 0 {
				combatComp.RemainingHitFlash = 0
			}
		}

		s.world.Components.Combat.SetComponent(combatEntity, combatComp)
	}

}

// applyHitDirect applies combat hit to a target
func (s *CombatSystem) applyHitDirect(payload *event.CombatAttackDirectRequestPayload) {
	// TODO: telemetry
	targetCombatComp, ok := s.world.Components.Combat.GetComponent(payload.TargetEntity)
	if !ok {
		return
	}
	attackerCombatComp, ok := s.world.Components.Combat.GetComponent(payload.OriginEntity)
	if !ok {
		return
	}

	// Generate combat matrix key
	attackerType := attackerCombatComp.CombatEntityType
	var targetCombatType component.CombatEntityType
	// Target defensive checks and type determination
	if payload.TargetEntity != payload.HitEntity {
		// Composite hit check
		headerComp, ok := s.world.Components.Header.GetComponent(payload.TargetEntity)
		if !ok {
			return
		}

		switch headerComp.Behavior {
		case component.BehaviorQuasar:
			targetCombatType = component.CombatEntityQuasar
		case component.BehaviorSwarm:
			targetCombatType = component.CombatEntitySwarm
		case component.BehaviorStorm:
			return // Future
		default:
			return
		}

		memberComp, ok := s.world.Components.Member.GetComponent(payload.HitEntity)
		if !ok || memberComp.HeaderEntity != payload.TargetEntity {
			return
		}
	} else {
		switch {
		case payload.TargetEntity == s.world.Resources.Cursor.Entity:
			targetCombatType = component.CombatEntityCursor
		case s.world.Components.Drain.HasEntity(payload.TargetEntity):
			targetCombatType = component.CombatEntityDrain
		default:
			return
		}
	}
	combatMatrixKey := component.CombatMatrixKey{attackerType, targetCombatType}
	combatProfile, ok := component.CombatMatrix[payload.AttackType][combatMatrixKey]
	if !ok {
		return
	}

	if combatProfile.DamageType != component.CombatDamageDirect {
		return
	}

	// Apply damage hit
	var targetDead bool
	if targetCombatComp.RemainingDamageImmunity == 0 && combatProfile.DamageValue != 0 {

		targetCombatComp.HitPoints -= combatProfile.DamageValue
		if targetCombatComp.HitPoints < 0 {
			targetCombatComp.HitPoints = 0
		}

		targetCombatComp.RemainingHitFlash = constant.CombatHitFlashDuration
		targetCombatComp.RemainingDamageImmunity = constant.CombatDamageImmunityDuration

		if targetCombatComp.HitPoints == 0 {
			// Not killing to let the chain attack to trigger
			targetDead = true
		}
	}

	// Emit chain attack if present
	chainAttack := combatProfile.ChainAttack
	if chainAttack != nil {
		s.world.PushEvent(event.EventCombatAttackDirectRequest, &event.CombatAttackDirectRequestPayload{
			AttackType:   chainAttack.AttackType,
			OwnerEntity:  payload.OwnerEntity,
			OriginEntity: payload.OwnerEntity,
			TargetEntity: payload.TargetEntity,
			HitEntity:    payload.HitEntity,
		})
	}

	// Apply effects
	switch {
	case combatProfile.EffectMask&component.CombatEffectVampireDrain != 0:
		s.world.PushEvent(event.EventVampireDrainRequest, &event.VampireDrainRequestPayload{
			TargetEntity: payload.HitEntity,
			Delta:        constant.VampireDrainEnergyValue,
		})
	case combatProfile.EffectMask&component.CombatEffectKinetic != 0:
		if !targetDead && targetCombatComp.RemainingKineticImmunity == 0 && !targetCombatComp.IsEnraged {
			s.applyCollision(payload.OriginEntity, payload.TargetEntity, payload.HitEntity, combatProfile.CollisionProfile)
		}
	}

	s.world.Components.Combat.SetComponent(payload.TargetEntity, targetCombatComp)
}

func (s *CombatSystem) applyHitArea(payload *event.CombatAttackAreaRequestPayload) {
	targetCombatComp, ok := s.world.Components.Combat.GetComponent(payload.TargetEntity)
	if !ok {
		return
	}
	attackerCombatComp, ok := s.world.Components.Combat.GetComponent(payload.OriginEntity)
	if !ok {
		return
	}

	// Generate combat matrix key
	attackerType := attackerCombatComp.CombatEntityType
	var targetCombatType component.CombatEntityType
	// Target defensive checks and type determination
	if len(payload.HitEntities) == 0 {
		return
	}
	if len(payload.HitEntities) == 1 && payload.TargetEntity == payload.HitEntities[0] {
		switch {
		case payload.TargetEntity == s.world.Resources.Cursor.Entity:
			targetCombatType = component.CombatEntityCursor
		case s.world.Components.Drain.HasEntity(payload.TargetEntity):
			targetCombatType = component.CombatEntityDrain
		default:
			return
		}
	} else {
		// Composite hit check
		headerComp, ok := s.world.Components.Header.GetComponent(payload.TargetEntity)
		if !ok {
			return
		}

		switch headerComp.Behavior {
		case component.BehaviorQuasar:
			targetCombatType = component.CombatEntityQuasar
		case component.BehaviorSwarm:
			return // Future
		case component.BehaviorStorm:
			return // Future
		default:
			return
		}
	}

	combatMatrixKey := component.CombatMatrixKey{attackerType, targetCombatType}
	combatProfile, ok := component.CombatMatrix[payload.AttackType][combatMatrixKey]
	if !ok {
		return
	}

	if combatProfile.DamageType != component.CombatDamageArea {
		return
	}

	// Apply damage hit
	var targetDead bool
	if targetCombatComp.RemainingDamageImmunity == 0 && combatProfile.DamageValue != 0 {

		damageValue := combatProfile.DamageValue * len(payload.HitEntities)
		targetCombatComp.HitPoints -= damageValue
		if targetCombatComp.HitPoints < 0 {
			targetCombatComp.HitPoints = 0
		}

		targetCombatComp.RemainingHitFlash = constant.CombatHitFlashDuration
		targetCombatComp.RemainingDamageImmunity = constant.CombatDamageImmunityDuration

		if targetCombatComp.HitPoints == 0 {
			// Not killing to let the chain attack to trigger
			targetDead = true
		}
	}

	// TODO: chain attack

	// Apply effects
	switch {
	// case combatProfile.EffectMask&component.CombatEffectVampireDrain != 0:
	// 	s.world.PushEvent(event.EventVampireDrainRequest, &event.VampireDrainRequestPayload{
	// 		TargetEntity: payload.HitEntity,
	// 		Delta:        constant.VampireDrainEnergyValue,
	// 	})
	case combatProfile.EffectMask&component.CombatEffectKinetic != 0:
		if !targetDead && targetCombatComp.RemainingKineticImmunity == 0 && !targetCombatComp.IsEnraged {
			s.applyFieldCollision(payload.OriginEntity, payload.TargetEntity, payload.HitEntities, combatProfile.CollisionProfile)
		}
	}

	s.world.Components.Combat.SetComponent(payload.TargetEntity, targetCombatComp)

}

func (s *CombatSystem) applyFieldCollision(originEntity, targetEntity core.Entity, hitEntities []core.Entity, collisionProfile *physics.CollisionProfile) {
	originPos, ok := s.world.Positions.GetPosition(originEntity)
	if !ok {
		return
	}
	targetPos, ok := s.world.Positions.GetPosition(targetEntity)
	if !ok {
		return
	}
	targetKineticComp, ok := s.world.Components.Kinetic.GetComponent(targetEntity)
	if !ok {
		return
	}

	// Radial direction: origin → target (e.g. cursor shield pushes drain outward)
	// TODO: change to cell-center physics from grid coords
	radialX := vmath.FromInt(targetPos.X - originPos.X)
	radialY := vmath.FromInt(targetPos.Y - originPos.Y)

	// Zero vector fallback (quasarComp centered on cursor)
	if radialX == 0 && radialY == 0 {
		radialX = vmath.Scale // Push right by default
	}

	if !s.world.Components.Header.HasEntity(targetEntity) {
		physics.ApplyCollision(&targetKineticComp.Kinetic, radialX, radialY, &physics.ShieldToDrain, s.rng)
		s.world.Components.Kinetic.SetComponent(targetEntity, targetKineticComp)
	} else {
		headerComp, ok := s.world.Components.Header.GetComponent(targetEntity)
		if !ok {
			return
		}

		// Centroid of overlapping member offsets (integer arithmetic)
		sumX, sumY := 0, 0
		for _, hitEntity := range hitEntities {
			for _, memberEntry := range headerComp.MemberEntries {
				if hitEntity == memberEntry.Entity {
					sumX += memberEntry.OffsetX
					sumY += memberEntry.OffsetX
				}
			}
		}
		centroidX := sumX / len(hitEntities)
		centroidY := sumY / len(hitEntities)

		physics.ApplyOffsetCollision(
			&targetKineticComp.Kinetic,
			radialX, radialY,
			centroidX, centroidY,
			collisionProfile,
			s.rng,
		)
		s.world.Components.Kinetic.SetComponent(targetEntity, targetKineticComp)

	}

}

func (s *CombatSystem) applyCollision(originEntity, targetEntity, hitEntity core.Entity, collisionProfile *physics.CollisionProfile) {
	// Apply kinetic hit/collision
	targetKineticComp, ok := s.world.Components.Kinetic.GetComponent(targetEntity)
	if !ok {
		return
	}
	originKineticComp, ok := s.world.Components.Kinetic.GetComponent(originEntity)
	if !ok {
		return
	}

	originVelX := originKineticComp.VelX
	originVelY := originKineticComp.VelY

	if targetEntity == hitEntity {
		// Non-composite collision
		physics.ApplyCollision(&targetKineticComp.Kinetic, originVelX, originVelY, collisionProfile, s.rng)
		s.world.Components.Kinetic.SetComponent(targetEntity, targetKineticComp)
	} else {
		headerPos, ok := s.world.Positions.GetPosition(targetEntity)
		if !ok {
			return
		}
		hitPos, ok := s.world.Positions.GetPosition(hitEntity)
		if !ok {
			return
		}

		offsetX := hitPos.X - headerPos.X
		offsetY := hitPos.Y - headerPos.Y

		// Composite collision
		physics.ApplyOffsetCollision(
			&targetKineticComp.Kinetic,
			originVelX, originVelY,
			offsetX, offsetY,
			collisionProfile,
			s.rng,
		)
		s.world.Components.Kinetic.SetComponent(targetEntity, targetKineticComp)

	}
}