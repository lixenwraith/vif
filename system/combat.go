package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
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
	return parameter.PriorityCombat
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

		// Update stun timer
		if combatComp.StunnedRemaining > 0 {
			combatComp.StunnedRemaining -= dt
			if combatComp.StunnedRemaining < 0 {
				combatComp.StunnedRemaining = 0
			}
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

	// Resolve attacker type: prefer OriginEntity, fallback to OwnerEntity if origin doesn't have combat component (e.g. visual-only entity like buff orb)
	var attackerType component.CombatEntityType
	if attackerCombatComp, ok := s.world.Components.Combat.GetComponent(payload.OriginEntity); ok {
		attackerType = attackerCombatComp.CombatEntityType
	} else if ownerCombatComp, ok := s.world.Components.Combat.GetComponent(payload.OwnerEntity); ok {
		attackerType = ownerCombatComp.CombatEntityType
	} else {
		return // No valid attacker
	}

	targetEntity := payload.TargetEntity
	hitEntity := payload.HitEntity

	targetCombatComp, ok := s.world.Components.Combat.GetComponent(payload.TargetEntity)
	if !ok {
		return
	}

	// Generate combat matrix key
	var targetCombatType component.CombatEntityType
	// Target type resolution using CompositeType with validation
	headerComp, isComposite := s.world.Components.Header.GetComponent(targetEntity)

	if isComposite {
		// Validation: If hit entity is different from target (sub-part hit), ensure HitEntity is a member of TargetEntity
		if hitEntity != targetEntity {
			memberComp, isMember := s.world.Components.Member.GetComponent(hitEntity)
			if !isMember || memberComp.HeaderEntity != targetEntity {
				return // Safety: HitEntity is not a child of the TargetEntity
			}
		}

		switch headerComp.Behavior {
		case component.BehaviorQuasar:
			targetCombatType = component.CombatEntityQuasar
		case component.BehaviorSwarm:
			targetCombatType = component.CombatEntitySwarm
		case component.BehaviorStorm:
			targetCombatType = component.CombatEntityStorm
		default:
			return
		}
	} else {
		// Non-composite logic
		switch {
		case targetEntity == s.world.Resources.Player.Entity:
			targetCombatType = component.CombatEntityCursor
		case s.world.Components.Drain.HasEntity(targetEntity):
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

	// Damage routing based on CompositeType
	var damageTargetDead bool

	if isComposite && headerComp.Type == component.CompositeTypeAblative {
		// Ablative: Damage specifically the HitEntity (Member)
		// We already validated above that HitEntity is a member of TargetEntity or TargetEntity itself.
		memberCombat, ok := s.world.Components.Combat.GetComponent(hitEntity)

		// If the header itself was hit directly (Target == Hit), we skip damage for Ablative types
		// (usually anchors/containers). Damage only applies to members.
		if ok && hitEntity != targetEntity {
			if memberCombat.RemainingDamageImmunity == 0 && combatProfile.DamageValue != 0 {
				memberCombat.HitPoints -= combatProfile.DamageValue
				if memberCombat.HitPoints < 0 {
					memberCombat.HitPoints = 0
				}
				memberCombat.RemainingHitFlash = parameter.CombatHitFlashDuration
				memberCombat.RemainingDamageImmunity = parameter.CombatDamageImmunityDuration
				damageTargetDead = memberCombat.HitPoints == 0
			}
			s.world.Components.Combat.SetComponent(hitEntity, memberCombat)
		}
	} else {
		// Unit (Swarm/Quasar) or Simple Entity: Damage the TargetEntity (Header/Self)
		if targetCombatComp.RemainingDamageImmunity == 0 && combatProfile.DamageValue != 0 {
			targetCombatComp.HitPoints -= combatProfile.DamageValue
			if targetCombatComp.HitPoints < 0 {
				targetCombatComp.HitPoints = 0
			}
			targetCombatComp.RemainingHitFlash = parameter.CombatHitFlashDuration
			targetCombatComp.RemainingDamageImmunity = parameter.CombatDamageImmunityDuration
			damageTargetDead = targetCombatComp.HitPoints == 0
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
		s.applyVampireDrain(payload.OwnerEntity, payload.OriginEntity, payload.HitEntity)
	case combatProfile.EffectMask&component.CombatEffectKinetic != 0:
		// Kinetic applies to header (composite moves as unit), check header immunity
		if !damageTargetDead && targetCombatComp.RemainingKineticImmunity == 0 && !targetCombatComp.IsEnraged {
			s.applyCollision(payload.OriginEntity, payload.TargetEntity, payload.HitEntity, combatProfile.CollisionProfile)
			targetCombatComp.RemainingKineticImmunity = combatProfile.CollisionProfile.ImmunityDuration
		}
	}

	// Save header (kinetic immunity; non-ablative damage already applied to targetCombatComp)
	s.world.Components.Combat.SetComponent(payload.TargetEntity, targetCombatComp)
}

func (s *CombatSystem) applyHitArea(payload *event.CombatAttackAreaRequestPayload) {
	targetEntity := payload.TargetEntity
	targetCombatComp, ok := s.world.Components.Combat.GetComponent(payload.TargetEntity)
	if !ok {
		return
	}

	// Generate combat matrix key
	var attackerType component.CombatEntityType
	if payload.OriginEntity == s.world.Resources.Player.Entity {
		attackerType = component.CombatEntityCursor
	} else if attackerComp, ok := s.world.Components.Combat.GetComponent(payload.OriginEntity); ok {
		attackerType = attackerComp.CombatEntityType
	} else {
		return
	}

	if len(payload.HitEntities) == 0 {
		return
	}

	// Resolve Target Type via Component (More robust than payload structure check)
	var targetCombatType component.CombatEntityType
	headerComp, isComposite := s.world.Components.Header.GetComponent(targetEntity)

	if isComposite {
		switch headerComp.Behavior {
		case component.BehaviorQuasar:
			targetCombatType = component.CombatEntityQuasar
		case component.BehaviorSwarm:
			targetCombatType = component.CombatEntitySwarm
		case component.BehaviorStorm:
			targetCombatType = component.CombatEntityStorm
		default:
			return
		}
	} else {
		// Single non-composite
		switch {
		case targetEntity == s.world.Resources.Player.Entity:
			targetCombatType = component.CombatEntityCursor
		case s.world.Components.Drain.HasEntity(targetEntity):
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

	if combatProfile.DamageType != component.CombatDamageArea {
		return
	}

	// Logic split by CompositeType with validation
	var targetDead bool

	if isComposite && headerComp.Type == component.CompositeTypeAblative {
		// Ablative (e.g., Storm): Damage individual hit members
		if combatProfile.DamageValue != 0 {
			for _, hitEntity := range payload.HitEntities {
				// 1. Don't damage the anchor/header itself in ablative mode
				if hitEntity == targetEntity {
					continue
				}

				// 2. Validation: ensure hit entity belongs to this target
				memberComp, isMember := s.world.Components.Member.GetComponent(hitEntity)
				if !isMember || memberComp.HeaderEntity != targetEntity {
					continue
				}

				// 3. Apply Damage to Member
				memberCombat, ok := s.world.Components.Combat.GetComponent(hitEntity)
				if !ok || memberCombat.RemainingDamageImmunity > 0 {
					continue
				}

				memberCombat.HitPoints -= combatProfile.DamageValue
				if memberCombat.HitPoints < 0 {
					memberCombat.HitPoints = 0
				}
				memberCombat.RemainingHitFlash = parameter.CombatHitFlashDuration
				memberCombat.RemainingDamageImmunity = parameter.CombatDamageImmunityDuration

				s.world.Components.Combat.SetComponent(hitEntity, memberCombat)
			}
		}
	} else {
		// Unit (e.g. Swarm) or Simple Entity: Damage Header/Self
		if targetCombatComp.RemainingDamageImmunity == 0 && combatProfile.DamageValue != 0 {

			// Validation: Filter hit entities to ensure they are valid parts of this target
			validHitCount := 0
			for _, hitEntity := range payload.HitEntities {
				// Hit is valid if it is the target itself
				if hitEntity == targetEntity {
					validHitCount++
					continue
				}
				// Valid if member of the target
				if isComposite {
					if member, ok := s.world.Components.Member.GetComponent(hitEntity); ok && member.HeaderEntity == targetEntity {
						validHitCount++
					}
				}
			}

			if validHitCount > 0 {
				damageValue := combatProfile.DamageValue * validHitCount
				targetCombatComp.HitPoints -= damageValue
				if targetCombatComp.HitPoints < 0 {
					targetCombatComp.HitPoints = 0
				}

				targetCombatComp.RemainingHitFlash = parameter.CombatHitFlashDuration
				targetCombatComp.RemainingDamageImmunity = parameter.CombatDamageImmunityDuration

				if targetCombatComp.HitPoints == 0 {
					targetDead = true
				}
			}
		}
	}

	// Apply kinetic effect
	if combatProfile.EffectMask&component.CombatEffectKinetic != 0 {
		if !targetDead && targetCombatComp.RemainingKineticImmunity == 0 && !targetCombatComp.IsEnraged {
			s.applyAreaKnockback(payload, combatProfile.CollisionProfile)
			targetCombatComp.RemainingKineticImmunity = combatProfile.CollisionProfile.ImmunityDuration
		}
	}

	// Apply stun effect
	if combatProfile.EffectMask&component.CombatEffectStun != 0 {
		if !targetDead {
			s.applyStunEffect(targetEntity, &targetCombatComp)
		}
	}

	// Chain attack for area attacks - emit per hit entity as direct attacks
	if chainAttack := combatProfile.ChainAttack; chainAttack != nil {
		for _, hitEntity := range payload.HitEntities {
			s.world.PushEvent(event.EventCombatAttackDirectRequest, &event.CombatAttackDirectRequestPayload{
				AttackType:   chainAttack.AttackType,
				OwnerEntity:  payload.OwnerEntity,
				OriginEntity: payload.OwnerEntity,
				TargetEntity: payload.TargetEntity,
				HitEntity:    hitEntity,
			})
		}
	}

	s.world.Components.Combat.SetComponent(payload.TargetEntity, targetCombatComp)
}

// applyVampireDrain handles energy steal and lightning VFX directly
// ownerEntity: receives energy (cursor)
// originEntity: lightning origin (orb or cursor)
// targetEntity: lightning target (hit entity)
func (s *CombatSystem) applyVampireDrain(ownerEntity, originEntity, targetEntity core.Entity) {
	energyComp, ok := s.world.Components.Energy.GetComponent(ownerEntity)
	if !ok {
		return
	}
	currentEnergy := energyComp.Current

	// Energy reward
	s.world.PushEvent(event.EventEnergyAddRequest, &event.EnergyAddPayload{
		Delta:      parameter.VampireDrainEnergyValue,
		Percentage: false,
		Type:       event.EnergyDeltaReward,
	})

	// Lightning VFX
	originPos, ok := s.world.Positions.GetPosition(originEntity)
	if !ok {
		return
	}
	targetPos, ok := s.world.Positions.GetPosition(targetEntity)
	if !ok {
		return
	}

	lightningColor := component.LightningGold
	if currentEnergy < 0 {
		lightningColor = component.LightningPurple
	}

	s.world.PushEvent(event.EventLightningSpawnRequest, &event.LightningSpawnRequestPayload{
		Owner:        ownerEntity,
		OriginX:      originPos.X,
		OriginY:      originPos.Y,
		TargetX:      targetPos.X,
		TargetY:      targetPos.Y,
		OriginEntity: originEntity,
		TargetEntity: targetEntity,
		ColorType:    lightningColor,
		Duration:     parameter.LightningZapDuration,
		Tracked:      false,
	})
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

// applyAreaKnockback calculates radial knockback for area attacks
// Uses explicit OriginX/OriginY if set, otherwise falls back to OriginEntity position
func (s *CombatSystem) applyAreaKnockback(payload *event.CombatAttackAreaRequestPayload, collisionProfile *physics.CollisionProfile) {
	targetPos, ok := s.world.Positions.GetPosition(payload.TargetEntity)
	if !ok {
		return
	}
	targetKineticComp, ok := s.world.Components.Kinetic.GetComponent(payload.TargetEntity)
	if !ok {
		return
	}

	// Determine origin position for radial direction
	var originX, originY int
	if payload.OriginX != 0 || payload.OriginY != 0 {
		// Explicit coordinates (explosion center)
		originX = payload.OriginX
		originY = payload.OriginY
	} else {
		// Fall back to entity position (shield, etc.)
		originPos, ok := s.world.Positions.GetPosition(payload.OriginEntity)
		if !ok {
			return
		}
		originX = originPos.X
		originY = originPos.Y
	}

	// Radial direction: origin → target (pushes outward)
	radialX := vmath.FromInt(targetPos.X - originX)
	radialY := vmath.FromInt(targetPos.Y - originY)

	if radialX == 0 && radialY == 0 {
		radialX = vmath.Scale // Fallback direction
	}

	// Single entity - direct radial knockback
	if len(payload.HitEntities) == 1 && payload.TargetEntity == payload.HitEntities[0] {
		physics.ApplyCollision(&targetKineticComp.Kinetic, radialX, radialY, collisionProfile, s.rng)
		s.world.Components.Kinetic.SetComponent(payload.TargetEntity, targetKineticComp)
		return
	}

	// Composite - calculate centroid offset for angled knockback
	headerComp, ok := s.world.Components.Header.GetComponent(payload.TargetEntity)
	if !ok {
		physics.ApplyCollision(&targetKineticComp.Kinetic, radialX, radialY, collisionProfile, s.rng)
		s.world.Components.Kinetic.SetComponent(payload.TargetEntity, targetKineticComp)
		return
	}

	// Build offset centroid from hit members
	sumX, sumY := 0, 0
	hitCount := 0
	for _, hitEntity := range payload.HitEntities {
		for _, member := range headerComp.MemberEntries {
			if hitEntity == member.Entity {
				sumX += member.OffsetX
				sumY += member.OffsetY
				hitCount++
				break
			}
		}
	}

	if hitCount == 0 {
		physics.ApplyCollision(&targetKineticComp.Kinetic, radialX, radialY, collisionProfile, s.rng)
	} else {
		centroidX := sumX / hitCount
		centroidY := sumY / hitCount
		physics.ApplyOffsetCollision(&targetKineticComp.Kinetic, radialX, radialY, centroidX, centroidY, collisionProfile, s.rng)
	}

	s.world.Components.Kinetic.SetComponent(payload.TargetEntity, targetKineticComp)
}

// applyStunEffect applies stun to target entity
// Returns false if target is immune to stun
func (s *CombatSystem) applyStunEffect(targetEntity core.Entity, targetCombatComp *component.CombatComponent) bool {
	// Quasar immunity: shielded state
	if quasarComp, ok := s.world.Components.Quasar.GetComponent(targetEntity); ok {
		if quasarComp.IsShielded {
			return false
		}
	}

	// Storm circle immunity: concave (invulnerable) state
	if circleComp, ok := s.world.Components.StormCircle.GetComponent(targetEntity); ok {
		if !circleComp.IsConvex() {
			return false
		}
	}

	// Apply stun
	targetCombatComp.StunnedRemaining = parameter.PulseStunDuration

	// Clear enrage state
	targetCombatComp.IsEnraged = false

	// Zero velocity
	if kineticComp, ok := s.world.Components.Kinetic.GetComponent(targetEntity); ok {
		kineticComp.VelX = 0
		kineticComp.VelY = 0
		s.world.Components.Kinetic.SetComponent(targetEntity, kineticComp)
	}

	return true
}