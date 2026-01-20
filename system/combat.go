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
		event.EventCombatFullKnockbackRequest,
		event.EventCombatAttackRequest,
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

	case event.EventCombatAttackRequest:
		if payload, ok := ev.Payload.(*event.CombatAttackRequestPayload); ok {
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

// applyFullKnockback applies radial impulse when drain overlaps shield
func (s *CombatSystem) applyFullKnockback(
	originEntity, targetEntity core.Entity,
) {
	targetCombatComp, ok := s.world.Components.Combat.GetComponent(targetEntity)
	if !ok {
		return
	}
	if targetCombatComp.RemainingKineticImmunity > 0 {
		return
	}

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
	targetCombatComp.RemainingKineticImmunity = constant.CombatKnockbackImmunityInterval
	s.world.Components.Combat.SetComponent(targetEntity, targetCombatComp)
}

// applyHit applies combat hit to a target
func (s *CombatSystem) applyHit(payload *event.CombatAttackRequestPayload) {
	// TODO: telemetry
	targetCombatComp, ok := s.world.Components.Combat.GetComponent(payload.TargetEntity)
	if !ok {
		return
	}
	var attackerCombatComp component.CombatComponent
	if payload.OriginEntity != payload.OwnerEntity {
		attackerCombatComp, ok = s.world.Components.Combat.GetComponent(payload.OriginEntity)
		if !ok {
			return
		}
	} else {
		attackerCombatComp, ok = s.world.Components.Combat.GetComponent(payload.OwnerEntity)
		if !ok {
			return
		}
	}

	// Generate combat matrix key
	attackerCombatType := attackerCombatComp.CombatEntityType
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
			targetCombatType = component.CombatEntityQuasar
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
			targetCombatType = component.CombatEntityCursor
		case s.world.Components.Drain.HasEntity(payload.TargetEntity):
			targetCombatType = component.CombatEntityDrain
		default:
			return
		}
	}
	combatMatrixKey := component.CombatMatrixKey{attackerCombatType, targetCombatType}
	combatProfile, ok := component.CombatMatrix[payload.AttackType][combatMatrixKey]
	if !ok {
		return
	}

	// Apply damage hit
	var targetDead bool
	if targetCombatComp.RemainingDamageImmunity == 0 && combatProfile.DamageValue != 0 {
		switch combatProfile.DamageType {
		case component.CombatDamageDirect:
			targetCombatComp.HitPoints -= combatProfile.DamageValue
			if targetCombatComp.HitPoints < 0 {
				targetCombatComp.HitPoints = 0
			}

		}

		targetCombatComp.RemainingHitFlash = constant.CombatHitFlashDuration
		targetCombatComp.RemainingDamageImmunity = constant.CombatDamageImmunityDuration

		if targetCombatComp.HitPoints == 0 {
			s.world.Components.Combat.SetComponent(payload.TargetEntity, targetCombatComp)
			// Not killing to let the chain attack to trigger, but
			targetDead = true
		}
	}

	// Emit chain attack if present
	chainAttack := combatProfile.ChainAttack
	if chainAttack != nil {
		s.world.PushEvent(event.EventCombatAttackRequest, &event.CombatAttackRequestPayload{
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
			s.applyCollision(payload.OriginEntity, payload.HitEntity, payload.TargetEntity, combatProfile.CollisionProfile)
		}
	}

	s.world.Components.Combat.SetComponent(payload.TargetEntity, targetCombatComp)
}

func (s *CombatSystem) applyCollision(originEntity, hitEntity, targetEntity core.Entity, collisionProfile *physics.CollisionProfile) {
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
		if physics.ApplyCollision(&targetKineticComp.Kinetic, originVelX, originVelY, collisionProfile, s.rng) {
			s.world.Components.Kinetic.SetComponent(targetEntity, targetKineticComp)
		}
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
		if physics.ApplyOffsetCollision(
			&targetKineticComp.Kinetic,
			originVelX, originVelY,
			offsetX, offsetY,
			collisionProfile,
			s.rng,
		) {
			s.world.Components.Kinetic.SetComponent(targetEntity, targetKineticComp)
		}
	}
}