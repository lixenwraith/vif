package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/profile"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
)

// CombatSystem manages interaction logic with combat entities
type CombatSystem struct {
	world *engine.World

	// Knockback impulse streams, selected by the impulse recipient's domain
	rngShared *vmath.FastRand
	rngPlayer *vmath.FastRand

	// Telemetry
	statActive        *atomic.Bool
	statCount         *atomic.Int64
	statDirect        *atomic.Int64
	statArea          *atomic.Int64
	statKnock         *atomic.Int64
	statStun          *atomic.Int64
	statDamage        *atomic.Int64
	statImmune        *atomic.Int64
	statUnprofiled    *atomic.Int64
	statDisabled      *atomic.Int64
	statAttacker      *atomic.Int64
	statTarget        *atomic.Int64
	statContainer     *atomic.Int64
	statRelation      *atomic.Int64
	statCursor        *atomic.Int64
	statKineticImmune *atomic.Int64
	statStunImmune    *atomic.Int64

	statDamageAttacker  [component.CombatEntityCount]*atomic.Int64
	statDamageDefender  [component.CombatEntityCount]*atomic.Int64
	statAbsorbAttacker  [component.CombatEntityCount]*atomic.Int64
	statAbsorbDefender  [component.CombatEntityCount]*atomic.Int64
	statEffectVampire   *atomic.Int64
	statEffectKinetic   *atomic.Int64
	statEffectStun      *atomic.Int64
	statChainFollowups  *atomic.Int64
	statChainDepthTotal *atomic.Int64
	statChainDepthMax   *atomic.Int64

	enabled bool
}

var combatEntityNames = [component.CombatEntityCount]string{
	"cursor",
	"drain",
	"quasar",
	"swarm",
	"storm",
	"pylon",
	"snake_head",
	"snake_body",
	"eye",
	"tower",
}

// NewCombatSystem creates a new quasar system
func NewCombatSystem(world *engine.World) engine.System {
	s := &CombatSystem{
		world: world,
	}

	reg := world.Resources.Status
	s.statActive = reg.Bools.Get("combat.active")
	s.statCount = reg.Ints.Get("combat.count")
	s.statDirect = reg.Ints.Get("combat.hits_direct")
	s.statArea = reg.Ints.Get("combat.hits_area")
	s.statKnock = reg.Ints.Get("combat.knockbacks")
	s.statStun = reg.Ints.Get("combat.stuns")
	s.statDamage = reg.Ints.Get("combat.damage_dealt")
	s.statImmune = reg.Ints.Get("combat.immune_rejects")
	s.statUnprofiled = reg.Ints.Get("combat.unprofiled")
	s.statDisabled = reg.Ints.Get("combat.disabled_rejects")
	s.statAttacker = reg.Ints.Get("combat.attacker_rejects")
	s.statTarget = reg.Ints.Get("combat.target_rejects")
	s.statContainer = reg.Ints.Get("combat.container_rejects")
	s.statRelation = reg.Ints.Get("combat.relation_rejects")
	s.statCursor = reg.Ints.Get("combat.cursor_rejects")
	s.statKineticImmune = reg.Ints.Get("combat.kinetic_immune_rejects")
	s.statStunImmune = reg.Ints.Get("combat.stun_immune_rejects")
	s.statEffectVampire = reg.Ints.Get("combat.effect_vampire")
	s.statEffectKinetic = reg.Ints.Get("combat.effect_kinetic")
	s.statEffectStun = reg.Ints.Get("combat.effect_stun")
	s.statChainFollowups = reg.Ints.Get("combat.chain_followups")
	s.statChainDepthTotal = reg.Ints.Get("combat.chain_depth_total")
	s.statChainDepthMax = reg.Ints.Get("combat.chain_depth_max")
	for i, name := range combatEntityNames {
		s.statDamageAttacker[i] = reg.Ints.Get("combat.damage_attacker_" + name)
		s.statDamageDefender[i] = reg.Ints.Get("combat.damage_defender_" + name)
		s.statAbsorbAttacker[i] = reg.Ints.Get("combat.absorbed_attacker_" + name)
		s.statAbsorbDefender[i] = reg.Ints.Get("combat.absorbed_defender_" + name)
	}

	s.Init()
	return s
}

func (s *CombatSystem) Init() {
	s.rngShared = s.world.Rand(core.DomainShared, s.Name())
	s.rngPlayer = s.world.Rand(core.DomainPlayer, s.Name())
	s.statActive.Store(false)
	s.statCount.Store(0)
	s.statDirect.Store(0)
	s.statArea.Store(0)
	s.statKnock.Store(0)
	s.statStun.Store(0)
	s.statDamage.Store(0)
	s.statImmune.Store(0)
	s.statUnprofiled.Store(0)
	for _, stat := range []*atomic.Int64{
		s.statDisabled,
		s.statAttacker,
		s.statTarget,
		s.statContainer,
		s.statRelation,
		s.statCursor,
		s.statKineticImmune,
		s.statStunImmune,
		s.statEffectVampire,
		s.statEffectKinetic,
		s.statEffectStun,
		s.statChainFollowups,
		s.statChainDepthTotal,
		s.statChainDepthMax,
	} {
		stat.Store(0)
	}
	for i := range component.CombatEntityCount {
		s.statDamageAttacker[i].Store(0)
		s.statDamageDefender[i].Store(0)
		s.statAbsorbAttacker[i].Store(0)
		s.statAbsorbDefender[i].Store(0)
	}
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
		event.EventCombatHealRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

func (s *CombatSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameResetRequest {
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
		if ev.Type == event.EventCombatAttackDirectRequest ||
			ev.Type == event.EventCombatAttackAreaRequest ||
			ev.Type == event.EventCombatHealRequest {
			s.statDisabled.Add(1)
		}
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

	case event.EventCombatHealRequest:
		if payload, ok := ev.Payload.(*event.CombatHealRequestPayload); ok {
			s.applyHeal(payload)
		}
	}
}

// applyHeal adds uncapped HP only while the target is still alive. This keeps
// lifecycle ownership with the species system and prevents a late bus event
// from resurrecting an entity already committed to death.
func (s *CombatSystem) applyHeal(payload *event.CombatHealRequestPayload) {
	if payload.Amount <= 0 {
		return
	}

	combatComp, ok := s.world.Components.Combat.GetPtr(payload.TargetEntity)
	if !ok || combatComp.HitPoints <= 0 {
		return
	}
	combatComp.HitPoints += payload.Amount
}

func (s *CombatSystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime

	combats := s.world.Components.Combat
	combatCount := int64(combats.CountEntities())
	s.statCount.Store(combatCount)
	s.statActive.Store(combatCount > 0)
	for _, combatEntity := range combats.Entities() {
		combatComp, ok := combats.GetPtr(combatEntity)
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

	}
}

// knockbackStream selects the impulse stream by the recipient's domain, so a
// knockback on a local drain never advances the shared sequence.
func (s *CombatSystem) knockbackStream(e core.Entity) *vmath.FastRand {
	if e.Domain() == core.DomainPlayer {
		return s.rngPlayer
	}
	return s.rngShared
}

// applyHitDirect applies combat hit to a target.
// Combat pointers stay valid: no path below inserts into or removes from the store.
func (s *CombatSystem) applyHitDirect(payload *event.CombatAttackDirectRequestPayload) {
	// Resolve attacker type: prefer OriginEntity, fallback to OwnerEntity
	var attackerType component.CombatEntityType
	if attackerCombatComp, ok := s.world.Components.Combat.GetPtr(payload.OriginEntity); ok {
		attackerType = attackerCombatComp.CombatEntityType
	} else if ownerCombatComp, ok := s.world.Components.Combat.GetPtr(payload.OwnerEntity); ok {
		attackerType = ownerCombatComp.CombatEntityType
	} else {
		s.statAttacker.Add(1)
		return
	}

	targetEntity := payload.TargetEntity
	hitEntity := payload.HitEntity

	targetCombatComp, ok := s.world.Components.Combat.GetPtr(targetEntity)
	if !ok {
		s.statTarget.Add(1)
		return
	}

	// Composite type check for damage routing
	headerComp, isComposite := s.world.Components.Header.GetPtr(targetEntity)

	// Reject containers
	if isComposite && headerComp.Type == component.CompositeTypeContainer {
		s.statContainer.Add(1)
		return
	}

	// Validate hit-to-target relationship for composites
	if isComposite && hitEntity != targetEntity {
		memberComp, isMember := s.world.Components.Member.GetPtr(hitEntity)
		if !isMember || memberComp.HeaderEntity != targetEntity {
			s.statRelation.Add(1)
			return
		}
	}

	attack := profile.Attack(payload.AttackType, attackerType, targetCombatComp.CombatEntityType)
	if attack == nil || attack.DamageType != component.CombatDamageDirect {
		s.statUnprofiled.Add(1)
		return
	}

	// Emitter geometry is carried by the payload: a direct request never resolves it
	// from an entity, so a player emitter never names itself in a crossing payload.
	originX, originY, hasOriginPos := payload.OriginX, payload.OriginY, payload.HasOrigin
	resolved := false
	damageCursor := s.world.ResolveCursor(payload.OwnerEntity)
	if damageCursor == 0 {
		s.statCursor.Add(1)
	}

	// Damage routing based on CompositeType
	var damageTargetDead bool

	if isComposite && headerComp.Type == component.CompositeTypeAblative {
		// Ablative: damage the HitEntity (member)
		if memberCombat, ok := s.world.Components.Combat.GetPtr(hitEntity); ok && hitEntity != targetEntity {
			if attack.DamageValue != 0 {
				if memberCombat.RemainingDamageImmunity != 0 {
					s.statImmune.Add(1)
					s.recordDamage(attackerType, memberCombat.CombatEntityType, 0, attack.DamageValue)
				} else {
					dealt := min(memberCombat.HitPoints, attack.DamageValue)
					memberCombat.HitPoints -= dealt
					s.recordDamage(attackerType, memberCombat.CombatEntityType, dealt, 0)
					resolved = true

					memberCombat.RemainingHitFlash = parameter.CombatHitFlashDuration
					memberCombat.RemainingDamageImmunity = parameter.CombatDamageImmunityDuration
					memberCombat.LastDamagedBy = damageCursor
					targetCombatComp.LastDamagedBy = damageCursor
					damageTargetDead = memberCombat.HitPoints == 0
				}
			}
		} else {
			s.statTarget.Add(1)
		}
	} else {
		// Unit or Simple: damage the TargetEntity
		if attack.DamageValue != 0 {
			if targetCombatComp.RemainingDamageImmunity != 0 {
				s.statImmune.Add(1)
				s.recordDamage(attackerType, targetCombatComp.CombatEntityType, 0, attack.DamageValue)
			} else {
				dealt := min(targetCombatComp.HitPoints, attack.DamageValue)
				targetCombatComp.HitPoints -= dealt
				s.recordDamage(attackerType, targetCombatComp.CombatEntityType, dealt, 0)
				resolved = true

				targetCombatComp.RemainingHitFlash = parameter.CombatHitFlashDuration
				targetCombatComp.RemainingDamageImmunity = parameter.CombatDamageImmunityDuration
				targetCombatComp.LastDamagedBy = damageCursor
				damageTargetDead = targetCombatComp.HitPoints == 0
			}
		}
	}

	// Emit chain attack if present
	if chainAttack := attack.Chain; chainAttack != nil {
		depth := payload.ChainDepth + 1
		s.world.PushEvent(event.EventCombatAttackDirectRequest, &event.CombatAttackDirectRequestPayload{
			AttackType:   chainAttack.AttackType,
			OwnerEntity:  payload.OwnerEntity,
			OriginEntity: payload.OwnerEntity,
			TargetEntity: payload.TargetEntity,
			HitEntity:    payload.HitEntity,
			HasOrigin:    hasOriginPos,
			OriginX:      originX,
			OriginY:      originY,
			ChainDepth:   depth,
		})
		s.recordChain(depth, 1)
		resolved = true
	}

	// Apply effects
	if attack.EffectMask&component.CombatEffectVampireDrain != 0 {
		if s.applyVampireDrain(payload.OwnerEntity, payload.HitEntity, originX, originY, hasOriginPos) {
			s.statEffectVampire.Add(1)
			resolved = true
		}
	}
	if attack.EffectMask&component.CombatEffectKinetic != 0 && attack.Collision != nil {
		// Kinetic applies to header (composite moves as unit), check header immunity
		if !damageTargetDead && targetCombatComp.RemainingKineticImmunity == 0 && !targetCombatComp.IsEnraged {
			if payload.HasVelocity &&
				s.applyCollision(payload.OriginVelX, payload.OriginVelY, payload.TargetEntity, payload.HitEntity, attack.Collision) {
				s.statEffectKinetic.Add(1)
				resolved = true
			}
			targetCombatComp.RemainingKineticImmunity = attack.KineticImmunity

			// Propagate to the hit member for displacement detection (snake body spring physics)
			if payload.HitEntity != payload.TargetEntity {
				if hitCombat, ok := s.world.Components.Combat.GetPtr(payload.HitEntity); ok {
					hitCombat.RemainingKineticImmunity = attack.KineticImmunity
				}
			}
		} else if !damageTargetDead {
			s.statKineticImmune.Add(1)
		}
	}
	if resolved {
		s.statDirect.Add(1)
	}
}

// applyHitArea resolves an area attack against one target.
// An empty payload.HitEntities is the implicit single-hit form; see the payload doc.
func (s *CombatSystem) applyHitArea(payload *event.CombatAttackAreaRequestPayload) {
	// Normalize before anything reads the hit set
	var hitBuf [1]core.Entity
	hits := payload.HitEntities
	if len(hits) == 0 {
		if payload.TargetEntity == 0 {
			s.statTarget.Add(1)
			return
		}
		hitBuf[0] = payload.TargetEntity
		hits = hitBuf[:]
	}

	targetEntity := payload.TargetEntity
	targetCombatComp, ok := s.world.Components.Combat.GetPtr(targetEntity)
	if !ok {
		s.statTarget.Add(1)
		return
	}

	// Resolve attacker type; any cursor on the roster attacks as a cursor
	var attackerType component.CombatEntityType
	if s.world.Components.Cursor.HasEntity(payload.OriginEntity) {
		attackerType = component.CombatEntityCursor
	} else if attackerComp, ok := s.world.Components.Combat.GetPtr(payload.OriginEntity); ok {
		attackerType = attackerComp.CombatEntityType
	} else {
		s.statAttacker.Add(1)
		return
	}

	// Resolve header if target is a member
	headerComp, isComposite := s.world.Components.Header.GetPtr(targetEntity)
	if !isComposite {
		if memberComp, isMember := s.world.Components.Member.GetPtr(targetEntity); isMember {
			headerEntity := memberComp.HeaderEntity
			if hc, ok := s.world.Components.Header.GetPtr(headerEntity); ok {
				headerComp = hc
				isComposite = true
				targetEntity = headerEntity
				targetCombatComp, ok = s.world.Components.Combat.GetPtr(headerEntity)
				if !ok {
					s.statTarget.Add(1)
					return
				}
			}
		}
	}

	// Reject containers
	if isComposite && headerComp.Type == component.CompositeTypeContainer {
		s.statContainer.Add(1)
		return
	}

	attack := profile.Attack(payload.AttackType, attackerType, targetCombatComp.CombatEntityType)
	if attack == nil || attack.DamageType != component.CombatDamageArea {
		s.statUnprofiled.Add(1)
		return
	}
	resolved := false
	damageCursor := s.world.ResolveCursor(payload.OwnerEntity)
	if damageCursor == 0 {
		s.statCursor.Add(1)
	}

	// Damage routing
	var targetDead bool
	damageApplied := false

	if isComposite && headerComp.Type == component.CompositeTypeAblative {
		if attack.DamageValue != 0 {
			for _, hitEntity := range hits {
				if hitEntity == targetEntity {
					continue
				}
				memberCombat, ok := s.world.Components.Combat.GetPtr(hitEntity)
				if !ok {
					continue
				}
				if memberCombat.RemainingDamageImmunity > 0 {
					s.statImmune.Add(1)
					s.recordDamage(attackerType, memberCombat.CombatEntityType, 0, attack.DamageValue)
					continue
				}
				dealt := min(memberCombat.HitPoints, attack.DamageValue)
				memberCombat.HitPoints -= dealt
				s.recordDamage(attackerType, memberCombat.CombatEntityType, dealt, 0)
				memberCombat.RemainingHitFlash = parameter.CombatHitFlashDuration
				memberCombat.RemainingDamageImmunity = parameter.CombatDamageImmunityDuration
				memberCombat.LastDamagedBy = damageCursor
				damageApplied = true
				resolved = true
			}
		}
	} else {
		if attack.DamageValue == 0 {
			// Zero-damage area profile (shield); effects below still apply
		} else {
			validHitCount := 0
			for _, hitEntity := range hits {
				if hitEntity == targetEntity {
					validHitCount++
					continue
				}
				if isComposite {
					if member, ok := s.world.Components.Member.GetPtr(hitEntity); ok && member.HeaderEntity == targetEntity {
						validHitCount++
					}
				}
			}
			damageValue := attack.DamageValue * validHitCount
			if targetCombatComp.RemainingDamageImmunity != 0 {
				s.statImmune.Add(1)
				s.recordDamage(attackerType, targetCombatComp.CombatEntityType, 0, damageValue)
			} else if validHitCount > 0 {
				dealt := min(targetCombatComp.HitPoints, damageValue)
				targetCombatComp.HitPoints -= dealt
				s.recordDamage(attackerType, targetCombatComp.CombatEntityType, dealt, 0)
				targetCombatComp.RemainingHitFlash = parameter.CombatHitFlashDuration
				targetCombatComp.RemainingDamageImmunity = parameter.CombatDamageImmunityDuration
				targetCombatComp.LastDamagedBy = damageCursor
				damageApplied = true
				resolved = true
				if targetCombatComp.HitPoints == 0 {
					targetDead = true
				}
			}
		}
	}
	if damageApplied {
		resolved = true
		// Ablative species read the header when deciding which cursor receives
		// whole-species kill credit; keep it synchronized with the last member hit.
		targetCombatComp.LastDamagedBy = damageCursor
	}

	// Apply kinetic effect
	if attack.EffectMask&component.CombatEffectKinetic != 0 && attack.Collision != nil {
		if !targetDead && targetCombatComp.RemainingKineticImmunity == 0 && !targetCombatComp.IsEnraged {
			if s.applyAreaKnockback(payload, targetEntity, hits, attack.Collision) {
				s.statEffectKinetic.Add(1)
				resolved = true
			}
			targetCombatComp.RemainingKineticImmunity = attack.KineticImmunity
		} else if !targetDead {
			s.statKineticImmune.Add(1)
		}
	}

	// Apply stun effect
	if attack.EffectMask&component.CombatEffectStun != 0 && !targetDead {
		if s.applyStunEffect(targetEntity, targetCombatComp) {
			s.statStun.Add(1)
			s.statEffectStun.Add(1)
			resolved = true
		} else {
			s.statStunImmune.Add(1)
		}
	}

	// Chain attack for area attacks - emit per hit entity as direct attacks
	if chainAttack := attack.Chain; chainAttack != nil {
		depth := payload.ChainDepth + 1
		for _, hitEntity := range hits {
			s.world.PushEvent(event.EventCombatAttackDirectRequest, &event.CombatAttackDirectRequestPayload{
				AttackType:   chainAttack.AttackType,
				OwnerEntity:  payload.OwnerEntity,
				OriginEntity: payload.OwnerEntity,
				TargetEntity: targetEntity,
				HitEntity:    hitEntity,
				HasOrigin:    payload.HasOrigin,
				OriginX:      payload.OriginX,
				OriginY:      payload.OriginY,
				ChainDepth:   depth,
			})
		}
		s.recordChain(depth, len(hits))
		resolved = len(hits) != 0 || resolved
	}
	if resolved {
		s.statArea.Add(1)
	}
}

// applyVampireDrain grants energy to the owner cursor and draws the zap from the
// attack's emitter to the hit entity. The zap is player-domain: only the instance
// owning the attack draws it.
func (s *CombatSystem) applyVampireDrain(ownerEntity, targetEntity core.Entity, originX, originY int, hasOrigin bool) bool {
	energyComp, ok := s.world.Components.Energy.GetPtr(ownerEntity)
	if !ok {
		return false
	}
	currentEnergy := energyComp.Current

	// Energy reward to the draining cursor
	s.world.PushEvent(event.EventEnergyAddRequest, &event.EnergyAddPayload{
		Entity:     ownerEntity,
		Delta:      parameter.VampireDrainEnergyValue,
		Percentage: false,
		Type:       component.EnergyDeltaReward,
	})

	if !hasOrigin || !s.world.Resources.Player.IsLocal(ownerEntity) {
		return true
	}
	targetPos, ok := s.world.Positions.GetPosition(targetEntity)
	if !ok {
		return true
	}

	lightningColor := component.LightningGold
	if currentEnergy < 0 {
		lightningColor = component.LightningPurple
	}

	s.world.PushEvent(event.EventLightningSpawnRequest, &event.LightningSpawnRequestPayload{
		Owner:        ownerEntity,
		OriginX:      originX,
		OriginY:      originY,
		TargetX:      targetPos.X,
		TargetY:      targetPos.Y,
		TargetEntity: targetEntity,
		ColorType:    lightningColor,
		Duration:     parameter.LightningZapDuration,
		Tracked:      false,
	})
	return true
}

func (s *CombatSystem) applyCollision(originVelX, originVelY float64, targetEntity, hitEntity core.Entity, collisionProfile *physics.CollisionProfile) bool {
	// Priority: hitEntity kinetic (ablative member with own kinetic, e.g. snake body)
	if hitEntity != targetEntity {
		if hitKinetic, ok := s.world.Components.Kinetic.GetPtr(hitEntity); ok {
			physics.ApplyCollision(&hitKinetic.Kinetic, originVelX, originVelY, collisionProfile, s.knockbackStream(hitEntity))
			s.statKnock.Add(1)
			return true
		}
	}

	// Fallback: targetEntity kinetic (header or simple entity)
	targetKineticComp, ok := s.world.Components.Kinetic.GetPtr(targetEntity)
	if !ok {
		return false
	}
	rng := s.knockbackStream(targetEntity)

	if targetEntity == hitEntity {
		// Direct hit on simple entity or header itself
		physics.ApplyCollision(&targetKineticComp.Kinetic, originVelX, originVelY, collisionProfile, rng)
		s.statKnock.Add(1)
	} else {
		// Member hit, kinetic on header — offset collision for angular impulse
		headerPos, ok := s.world.Positions.GetPosition(targetEntity)
		if !ok {
			return false
		}
		hitPos, ok := s.world.Positions.GetPosition(hitEntity)
		if !ok {
			return false
		}

		offsetX := hitPos.X - headerPos.X
		offsetY := hitPos.Y - headerPos.Y

		physics.ApplyOffsetCollision(
			&targetKineticComp.Kinetic,
			originVelX, originVelY,
			offsetX, offsetY,
			collisionProfile,
			rng,
		)
		s.statKnock.Add(1)
	}
	return true
}

// applyAreaKnockback calculates radial knockback for area attacks.
// hits is the normalized hit set; targetEntity is the resolved header.
func (s *CombatSystem) applyAreaKnockback(payload *event.CombatAttackAreaRequestPayload, targetEntity core.Entity, hits []core.Entity, collisionProfile *physics.CollisionProfile) bool {
	targetPos, ok := s.world.Positions.GetPosition(targetEntity)
	if !ok {
		return false
	}
	targetKineticComp, ok := s.world.Components.Kinetic.GetPtr(targetEntity)
	if !ok {
		return false
	}
	rng := s.knockbackStream(targetEntity)

	// Determine origin position for radial direction
	var originX, originY int
	if payload.HasOrigin {
		// Explicit coordinates (explosion center)
		originX = payload.OriginX
		originY = payload.OriginY
	} else {
		// Fall back to entity position; every producer taking this path names a
		// shared cursor or species header, never a player emitter.
		originPos, ok := s.world.Positions.GetPosition(payload.OriginEntity)
		if !ok {
			return false
		}
		originX = originPos.X
		originY = originPos.Y
	}

	// Radial direction: origin → target (pushes outward)
	radialX := float64(targetPos.X - originX)
	radialY := float64(targetPos.Y - originY)

	if radialX == 0 && radialY == 0 {
		radialX = 1.0 // Fallback direction
	}

	// Every branch below applies exactly one impulse
	s.statKnock.Add(1)

	// Single entity - direct radial knockback
	if len(hits) == 1 && targetEntity == hits[0] {
		physics.ApplyCollision(&targetKineticComp.Kinetic, radialX, radialY, collisionProfile, rng)
		return true
	}

	// Composite - calculate centroid offset for angled knockback
	headerComp, ok := s.world.Components.Header.GetPtr(targetEntity)
	if !ok {
		physics.ApplyCollision(&targetKineticComp.Kinetic, radialX, radialY, collisionProfile, rng)
		return true
	}

	// Build offset centroid from hit members
	sumX, sumY := 0, 0
	hitCount := 0
	for _, hitEntity := range hits {
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
		physics.ApplyCollision(&targetKineticComp.Kinetic, radialX, radialY, collisionProfile, rng)
	} else {
		centroidX := sumX / hitCount
		centroidY := sumY / hitCount
		physics.ApplyOffsetCollision(&targetKineticComp.Kinetic, radialX, radialY, centroidX, centroidY, collisionProfile, rng)
	}
	return true
}

// recordDamage records resolved damage and absorption by both sides of an attack.
func (s *CombatSystem) recordDamage(attackerType, defenderType component.CombatEntityType, dealt, absorbed int) {
	if attackerType < 0 || attackerType >= component.CombatEntityCount || defenderType < 0 || defenderType >= component.CombatEntityCount {
		return
	}
	if dealt > 0 {
		s.statDamage.Add(int64(dealt))
		s.statDamageAttacker[attackerType].Add(int64(dealt))
		s.statDamageDefender[defenderType].Add(int64(dealt))
	}
	if absorbed > 0 {
		s.statAbsorbAttacker[attackerType].Add(int64(absorbed))
		s.statAbsorbDefender[defenderType].Add(int64(absorbed))
	}
}

// recordChain records the number and depth of emitted follow-up attacks.
func (s *CombatSystem) recordChain(depth uint8, count int) {
	if count <= 0 {
		return
	}
	s.statChainFollowups.Add(int64(count))
	s.statChainDepthTotal.Add(int64(depth) * int64(count))
	storeMax(s.statChainDepthMax, int64(depth))
}

// applyStunEffect applies stun to target entity
// Returns false if target is immune to stun
func (s *CombatSystem) applyStunEffect(targetEntity core.Entity, targetCombatComp *component.CombatComponent) bool {
	// Quasar immunity: shielded state
	if quasarComp, ok := s.world.Components.Quasar.GetPtr(targetEntity); ok {
		if quasarComp.IsShielded {
			return false
		}
	}

	// Storm circle immunity: concave (invulnerable) state
	if circleComp, ok := s.world.Components.StormCircle.GetPtr(targetEntity); ok {
		if circleComp.IsInvulnerable {
			return false
		}
	}

	// Snake immunity: head immune when shielded, body always immune to stay in sync with body
	if s.world.Components.SnakeHead.HasEntity(targetEntity) {
		// Head: find root and check shield state
		if memberComp, ok := s.world.Components.Member.GetPtr(targetEntity); ok {
			if snakeComp, ok := s.world.Components.Snake.GetPtr(memberComp.HeaderEntity); ok {
				if snakeComp.IsShielded {
					return false
				}
			}
		}
	} else if s.world.Components.SnakeBody.HasEntity(targetEntity) {
		// Body: always immune (spring physics maintains connectivity)
		return false
	}

	// Apply stun
	targetCombatComp.StunnedRemaining = parameter.PulseStunDuration

	// Clear enrage state
	targetCombatComp.IsEnraged = false

	// Zero velocity
	if kineticComp, ok := s.world.Components.Kinetic.GetPtr(targetEntity); ok {
		kineticComp.VelX = 0
		kineticComp.VelY = 0
	}

	return true
}
