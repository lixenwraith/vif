package profile

import (
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
)

// AttackProfile defines the outcome of one attacker -> defender interaction
type AttackProfile struct {
	AttackType component.CombatAttackType
	Attacker   component.CombatEntityType
	Defender   component.CombatEntityType

	DamageType  component.CombatDamageType
	DamageValue int
	EffectMask  component.CombatEffectMask

	// Chain fires as a follow-up direct attack after this one resolves
	Chain *AttackProfile

	// Collision and KineticImmunity apply when EffectMask has CombatEffectKinetic
	Collision       *physics.CollisionProfile
	KineticImmunity time.Duration
}

// attackMatrix is a dense [attack][attacker][defender] lookup; nil = undefined
var attackMatrix [component.CombatAttackTypeCount][component.CombatEntityCount][component.CombatEntityCount]*AttackProfile

// Attack returns the profile for an interaction, nil if undefined
func Attack(atk component.CombatAttackType, attacker, defender component.CombatEntityType) *AttackProfile {
	if uint(atk) >= uint(component.CombatAttackTypeCount) ||
		uint(attacker) >= uint(component.CombatEntityCount) ||
		uint(defender) >= uint(component.CombatEntityCount) {
		return nil
	}
	return attackMatrix[atk][attacker][defender]
}

func register(p *AttackProfile) {
	attackMatrix[p.AttackType][p.Attacker][p.Defender] = p
}

// cursorDefenders lists every entity the player can attack
var cursorDefenders = [...]component.CombatEntityType{
	component.CombatEntityDrain,
	component.CombatEntityQuasar,
	component.CombatEntitySwarm,
	component.CombatEntityStorm,
	component.CombatEntityPylon,
	component.CombatEntitySnakeHead,
	component.CombatEntitySnakeBody,
	component.CombatEntityEye,
}

// eyeTargets lists everything an eye can self-destruct against
var eyeTargets = [...]component.CombatEntityType{
	component.CombatEntityDrain,
	component.CombatEntityQuasar,
	component.CombatEntitySwarm,
	component.CombatEntityStorm,
	component.CombatEntityPylon,
	component.CombatEntitySnakeHead,
	component.CombatEntitySnakeBody,
	component.CombatEntityCursor,
	component.CombatEntityTower,
}

// collisionSet maps a defender to its knockback profile for one attack family;
// a nil entry means that defender takes no knockback (pylon is stationary)
type collisionSet [component.CombatEntityCount]*physics.CollisionProfile

var cleanerCollision = collisionSet{
	component.CombatEntityDrain:     &CleanerToDrain,
	component.CombatEntityQuasar:    &CleanerToQuasar,
	component.CombatEntitySwarm:     &CleanerToSwarm,
	component.CombatEntityStorm:     &CleanerToStorm,
	component.CombatEntitySnakeHead: &CleanerToSnakeHead,
	component.CombatEntitySnakeBody: &CleanerToSnakeBody,
	component.CombatEntityEye:       &CleanerToEye,
}

var shieldCollision = collisionSet{
	component.CombatEntityDrain:     &ShieldToDrain,
	component.CombatEntityQuasar:    &ShieldToQuasar,
	component.CombatEntitySwarm:     &ShieldToSwarm,
	component.CombatEntityStorm:     &ShieldToStorm,
	component.CombatEntitySnakeHead: &ShieldToSnakeHead,
	component.CombatEntitySnakeBody: &ShieldToSnakeBody,
	component.CombatEntityEye:       &ShieldToEye,
}

var explosionCollision = collisionSet{
	component.CombatEntityDrain:     &ExplosionToDrain,
	component.CombatEntityQuasar:    &ExplosionToQuasar,
	component.CombatEntitySwarm:     &ExplosionToSwarm,
	component.CombatEntityStorm:     &ExplosionToStorm,
	component.CombatEntitySnakeHead: &ExplosionToSnakeHead,
	component.CombatEntitySnakeBody: &ExplosionToSnakeBody,
	component.CombatEntityEye:       &ExplosionToEye,
}

// Backing storage: fixed arrays keep profile addresses stable for Chain
// pointers and for the matrix
var (
	cleanerProfiles      [component.CombatEntityCount]AttackProfile
	lightningProfiles    [component.CombatEntityCount]AttackProfile
	shieldProfiles       [component.CombatEntityCount]AttackProfile
	explosionProfiles    [component.CombatEntityCount]AttackProfile
	missileProfiles      [component.CombatEntityCount]AttackProfile
	pulseProfiles        [component.CombatEntityCount]AttackProfile
	selfDestructProfiles [component.CombatEntityCount]AttackProfile
)

// withKinetic attaches knockback when the family defines one for this defender
func withKinetic(p *AttackProfile, set *collisionSet) {
	c := set[p.Defender]
	if c == nil {
		return
	}
	p.EffectMask |= component.CombatEffectKinetic
	p.Collision = c
	p.KineticImmunity = parameter.CombatKineticImmunityDuration
}

func init() {
	// Lightning first: cleaner chains into it
	for _, d := range cursorDefenders {
		lightningProfiles[d] = AttackProfile{
			AttackType:  component.CombatAttackLightning,
			Attacker:    component.CombatEntityCursor,
			Defender:    d,
			DamageType:  component.CombatDamageDirect,
			DamageValue: parameter.CombatDamageRod,
			EffectMask:  component.CombatEffectVampireDrain,
		}
		register(&lightningProfiles[d])
	}

	// Cleaner: direct damage, knockback, chains into lightning
	for _, d := range cursorDefenders {
		p := &cleanerProfiles[d]
		*p = AttackProfile{
			AttackType:  component.CombatAttackProjectile,
			Attacker:    component.CombatEntityCursor,
			Defender:    d,
			DamageType:  component.CombatDamageDirect,
			DamageValue: parameter.CombatDamageCleaner,
			Chain:       &lightningProfiles[d],
		}
		withKinetic(p, &cleanerCollision)
		register(p)
	}

	// Shield: no damage, knockback only
	for _, d := range cursorDefenders {
		p := &shieldProfiles[d]
		*p = AttackProfile{
			AttackType: component.CombatAttackShield,
			Attacker:   component.CombatEntityCursor,
			Defender:   d,
			DamageType: component.CombatDamageArea,
		}
		withKinetic(p, &shieldCollision)
		register(p)
	}

	// Explosion
	for _, d := range cursorDefenders {
		p := &explosionProfiles[d]
		*p = AttackProfile{
			AttackType:  component.CombatAttackExplosion,
			Attacker:    component.CombatEntityCursor,
			Defender:    d,
			DamageType:  component.CombatDamageArea,
			DamageValue: parameter.CombatDamageExplosion,
		}
		withKinetic(p, &explosionCollision)
		register(p)
	}

	// Missile: damage only, no knockback. The explosion spawned on impact is
	// typed ExplosionTypeMissile and routes back to this same family, so
	// nothing downstream adds an impulse either.
	for _, d := range cursorDefenders {
		missileProfiles[d] = AttackProfile{
			AttackType:  component.CombatAttackMissile,
			Attacker:    component.CombatEntityCursor,
			Defender:    d,
			DamageType:  component.CombatDamageArea,
			DamageValue: parameter.CombatDamageMissile,
		}
		register(&missileProfiles[d])
	}

	// Pulse: stun, no knockback
	for _, d := range cursorDefenders {
		pulseProfiles[d] = AttackProfile{
			AttackType:  component.CombatAttackPulse,
			Attacker:    component.CombatEntityCursor,
			Defender:    d,
			DamageType:  component.CombatDamageArea,
			DamageValue: parameter.CombatDamagePulse,
			EffectMask:  component.CombatEffectStun,
		}
		register(&pulseProfiles[d])
	}

	// Eye self-destruct: uniform across every target
	for _, d := range eyeTargets {
		selfDestructProfiles[d] = AttackProfile{
			AttackType:  component.CombatAttackSelfDestruct,
			Attacker:    component.CombatEntityEye,
			Defender:    d,
			DamageType:  component.CombatDamageArea,
			DamageValue: parameter.CombatDamageEyeSelfDestruct,
		}
		register(&selfDestructProfiles[d])
	}
}
