package component

import (
	"time"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/physics"
)

type CombatEntityType int

const (
	CombatEntityCursor CombatEntityType = iota
	CombatEntityDrain
	CombatEntityQuasar
	CombatEntitySwarm
	CombatEntityStorm
	CombatEntityPylon
	CombatEntitySnakeHead
	CombatEntitySnakeBody
	CombatEntityCount
)

// Damage Types
type CombatDamageType int

const (
	CombatDamageNone CombatDamageType = iota
	CombatDamageDirect
	CombatDamageArea
	CombatDamageOverTime // Future
)

// Attack Types
type CombatAttackType int

const (
	CombatAttackProjectile CombatAttackType = iota
	CombatAttackShield
	CombatAttackLightning
	CombatAttackExplosion
	CombatAttackMissile
	CombatAttackPulse
)

// Effect Types
type CombatEffectMask uint64

const CombatEffectNone CombatEffectMask = 0
const (
	CombatEffectVampireDrain CombatEffectMask = 1 << iota
	CombatEffectKinetic
	CombatEffectStun // Future
)

// CombatComponent tags an entity to be identified as enemy for interactions
type CombatComponent struct {
	// OwnerEntity indicates owner/parent of the entity with combat component (e.g. cursor is the parent of cleaner)
	OwnerEntity core.Entity

	// CombatEntityType
	CombatEntityType CombatEntityType

	// HitPoints is the remaining hit points of the combat entity (>0)
	HitPoints int

	// IsEnraged is the enrage indicator that modifies combat behavior
	IsEnraged bool

	// RemainingDamageImmunity is remaining immunity time for damage
	RemainingDamageImmunity time.Duration

	// RemainingHitFlash is the remaining duration of hit visual feedback
	RemainingHitFlash time.Duration

	// RemainingKineticImmunity is remaining immunity time for collision knockback
	RemainingKineticImmunity time.Duration

	// StunnedRemaining is remaining stun duration (movement suppressed)
	StunnedRemaining time.Duration
}

type CombatAttackProfile struct {
	AttackType         CombatAttackType
	AttackerEntityType CombatEntityType
	DefenderEntityType CombatEntityType
	DamageType         CombatDamageType
	DamageValue        int
	EffectMask         CombatEffectMask
	ChainAttack        *CombatAttackProfile
	CollisionProfile   *physics.CollisionProfile
}

type CombatMatrixKey [2]CombatEntityType

type combatMatrixMap map[CombatAttackType]map[CombatMatrixKey]*CombatAttackProfile

var CombatMatrix = combatMatrixMap{
	CombatAttackProjectile: {
		{CombatEntityCursor, CombatEntityDrain}:     &CombatAttackCleanerToDrain,
		{CombatEntityCursor, CombatEntityQuasar}:    &CombatAttackCleanerToQuasar,
		{CombatEntityCursor, CombatEntitySwarm}:     &CombatAttackCleanerToSwarm,
		{CombatEntityCursor, CombatEntityStorm}:     &CombatAttackCleanerToStorm,
		{CombatEntityCursor, CombatEntityPylon}:     &CombatAttackCleanerToPylon,
		{CombatEntityCursor, CombatEntitySnakeHead}: &CombatAttackCleanerToSnakeHead,
		{CombatEntityCursor, CombatEntitySnakeBody}: &CombatAttackCleanerToSnakeBody,
	},
	CombatAttackShield: {
		{CombatEntityCursor, CombatEntityDrain}:     &CombatAttackShieldToDrain,
		{CombatEntityCursor, CombatEntityQuasar}:    &CombatAttackShieldToQuasar,
		{CombatEntityCursor, CombatEntitySwarm}:     &CombatAttackShieldToSwarm,
		{CombatEntityCursor, CombatEntityStorm}:     &CombatAttackShieldToStorm,
		{CombatEntityCursor, CombatEntitySnakeHead}: &CombatAttackShieldToSnakeHead,
		{CombatEntityCursor, CombatEntitySnakeBody}: &CombatAttackShieldToSnakeBody,
	},
	CombatAttackLightning: {
		{CombatEntityCursor, CombatEntityDrain}:     &CombatAttackLightningToDrain,
		{CombatEntityCursor, CombatEntityQuasar}:    &CombatAttackLightningToQuasar,
		{CombatEntityCursor, CombatEntitySwarm}:     &CombatAttackLightningToSwarm,
		{CombatEntityCursor, CombatEntityStorm}:     &CombatAttackLightningToStorm,
		{CombatEntityCursor, CombatEntityPylon}:     &CombatAttackLightningToPylon,
		{CombatEntityCursor, CombatEntitySnakeHead}: &CombatAttackLightningToSnakeHead,
		{CombatEntityCursor, CombatEntitySnakeBody}: &CombatAttackLightningToSnakeBody,
	},
	CombatAttackExplosion: {
		{CombatEntityCursor, CombatEntityDrain}:     &CombatAttackExplosionToDrain,
		{CombatEntityCursor, CombatEntityQuasar}:    &CombatAttackExplosionToQuasar,
		{CombatEntityCursor, CombatEntitySwarm}:     &CombatAttackExplosionToSwarm,
		{CombatEntityCursor, CombatEntityStorm}:     &CombatAttackExplosionToStorm,
		{CombatEntityCursor, CombatEntityPylon}:     &CombatAttackExplosionToPylon,
		{CombatEntityCursor, CombatEntitySnakeHead}: &CombatAttackExplosionToSnakeHead,
		{CombatEntityCursor, CombatEntitySnakeBody}: &CombatAttackExplosionToSnakeBody,
	},
	CombatAttackMissile: {
		{CombatEntityCursor, CombatEntityDrain}:     &CombatAttackMissileToDrain,
		{CombatEntityCursor, CombatEntityQuasar}:    &CombatAttackMissileToQuasar,
		{CombatEntityCursor, CombatEntitySwarm}:     &CombatAttackMissileToSwarm,
		{CombatEntityCursor, CombatEntityStorm}:     &CombatAttackMissileToStorm,
		{CombatEntityCursor, CombatEntityPylon}:     &CombatAttackMissileToPylon,
		{CombatEntityCursor, CombatEntitySnakeHead}: &CombatAttackMissileToSnakeHead,
		{CombatEntityCursor, CombatEntitySnakeBody}: &CombatAttackMissileToSnakeBody,
	},
	CombatAttackPulse: {
		{CombatEntityCursor, CombatEntityDrain}:     &CombatAttackPulseToDrain,
		{CombatEntityCursor, CombatEntityQuasar}:    &CombatAttackPulseToQuasar,
		{CombatEntityCursor, CombatEntitySwarm}:     &CombatAttackPulseToSwarm,
		{CombatEntityCursor, CombatEntityStorm}:     &CombatAttackPulseToStorm,
		{CombatEntityCursor, CombatEntityPylon}:     &CombatAttackPulseToPylon,
		{CombatEntityCursor, CombatEntitySnakeHead}: &CombatAttackPulseToSnakeHead,
		{CombatEntityCursor, CombatEntitySnakeBody}: &CombatAttackPulseToSnakeBody,
	},
}

// Combat attack profiles - pre-defined for zero allocation in hot path

// Cleaner attack profiles

var CombatAttackCleanerToDrain = CombatAttackProfile{
	AttackType:         CombatAttackProjectile,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityDrain,
	DamageType:         CombatDamageDirect,
	DamageValue:        parameter.CombatDamageCleaner,
	EffectMask:         CombatEffectKinetic,
	ChainAttack:        &CombatAttackLightningToDrain,
	// TODO: migrate collision to matrix
	CollisionProfile: &physics.CleanerToDrain,
}

var CombatAttackCleanerToQuasar = CombatAttackProfile{
	AttackType:         CombatAttackProjectile,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityQuasar,
	DamageType:         CombatDamageDirect,
	DamageValue:        parameter.CombatDamageCleaner,
	EffectMask:         CombatEffectKinetic,
	ChainAttack:        &CombatAttackLightningToQuasar,
	CollisionProfile:   &physics.CleanerToQuasar,
}

var CombatAttackCleanerToSwarm = CombatAttackProfile{
	AttackType:         CombatAttackProjectile,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntitySwarm,
	DamageType:         CombatDamageDirect,
	DamageValue:        parameter.CombatDamageCleaner,
	EffectMask:         CombatEffectKinetic,
	ChainAttack:        &CombatAttackLightningToSwarm,
	CollisionProfile:   &physics.CleanerToSwarm,
}

var CombatAttackCleanerToStorm = CombatAttackProfile{
	AttackType:         CombatAttackProjectile,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityStorm,
	DamageType:         CombatDamageDirect,
	DamageValue:        parameter.CombatDamageCleaner,
	EffectMask:         CombatEffectKinetic,
	ChainAttack:        &CombatAttackLightningToStorm,
	CollisionProfile:   &physics.CleanerToQuasar, // Reuse quasar profile
}

var CombatAttackCleanerToPylon = CombatAttackProfile{
	AttackType:         CombatAttackProjectile,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityPylon,
	DamageType:         CombatDamageDirect,
	DamageValue:        parameter.CombatDamageCleaner,
	EffectMask:         CombatEffectNone,
	ChainAttack:        &CombatAttackLightningToPylon,
}

var CombatAttackCleanerToSnakeHead = CombatAttackProfile{
	AttackType:         CombatAttackProjectile,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntitySnakeHead,
	DamageType:         CombatDamageDirect,
	DamageValue:        parameter.CombatDamageCleaner,
	EffectMask:         CombatEffectKinetic,
	ChainAttack:        &CombatAttackLightningToSnakeHead,
	CollisionProfile:   &physics.CleanerToQuasar, // Reuse quasar profile
}

var CombatAttackCleanerToSnakeBody = CombatAttackProfile{
	AttackType:         CombatAttackProjectile,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntitySnakeBody,
	DamageType:         CombatDamageDirect,
	DamageValue:        parameter.CombatDamageCleaner,
	EffectMask:         CombatEffectKinetic,
	ChainAttack:        &CombatAttackLightningToSnakeBody,
	CollisionProfile:   &physics.CleanerToSwarm, // Reuse swarm profile for body
}

// Lightning attack profiles

var CombatAttackLightningToDrain = CombatAttackProfile{
	AttackType:         CombatAttackLightning,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityDrain,
	DamageType:         CombatDamageDirect,
	DamageValue:        parameter.CombatDamageRod,
	EffectMask:         CombatEffectVampireDrain,
	ChainAttack:        nil,
	CollisionProfile:   nil,
}

var CombatAttackLightningToQuasar = CombatAttackProfile{
	AttackType:         CombatAttackLightning,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityQuasar,
	DamageType:         CombatDamageDirect,
	DamageValue:        parameter.CombatDamageRod,
	EffectMask:         CombatEffectVampireDrain,
	ChainAttack:        nil,
	CollisionProfile:   nil,
}

var CombatAttackLightningToSwarm = CombatAttackProfile{
	AttackType:         CombatAttackLightning,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntitySwarm,
	DamageType:         CombatDamageDirect,
	DamageValue:        parameter.CombatDamageRod,
	EffectMask:         CombatEffectVampireDrain,
	ChainAttack:        nil,
	CollisionProfile:   nil,
}

var CombatAttackLightningToStorm = CombatAttackProfile{
	AttackType:         CombatAttackLightning,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityStorm,
	DamageType:         CombatDamageDirect,
	DamageValue:        parameter.CombatDamageRod,
	EffectMask:         CombatEffectVampireDrain,
	ChainAttack:        nil,
	CollisionProfile:   nil,
}

var CombatAttackLightningToPylon = CombatAttackProfile{
	AttackType:         CombatAttackLightning,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityPylon,
	DamageType:         CombatDamageDirect,
	DamageValue:        parameter.CombatDamageRod,
	EffectMask:         CombatEffectVampireDrain,
	ChainAttack:        nil,
	CollisionProfile:   nil,
}

var CombatAttackLightningToSnakeHead = CombatAttackProfile{
	AttackType:         CombatAttackLightning,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntitySnakeHead,
	DamageType:         CombatDamageDirect,
	DamageValue:        parameter.CombatDamageRod,
	EffectMask:         CombatEffectVampireDrain,
	ChainAttack:        nil,
	CollisionProfile:   nil,
}

var CombatAttackLightningToSnakeBody = CombatAttackProfile{
	AttackType:         CombatAttackLightning,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntitySnakeBody,
	DamageType:         CombatDamageDirect,
	DamageValue:        parameter.CombatDamageRod,
	EffectMask:         CombatEffectVampireDrain,
	ChainAttack:        nil,
	CollisionProfile:   nil,
}

// Shield attack profiles

var CombatAttackShieldToDrain = CombatAttackProfile{
	AttackType:         CombatAttackShield,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityDrain,
	DamageType:         CombatDamageArea,
	DamageValue:        0,
	EffectMask:         CombatEffectKinetic,
	ChainAttack:        nil,
	CollisionProfile:   &physics.ShieldToDrain,
}

var CombatAttackShieldToQuasar = CombatAttackProfile{
	AttackType:         CombatAttackShield,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityQuasar,
	DamageType:         CombatDamageArea,
	DamageValue:        0,
	EffectMask:         CombatEffectKinetic,
	ChainAttack:        nil,
	CollisionProfile:   &physics.ShieldToQuasar,
}

var CombatAttackShieldToSwarm = CombatAttackProfile{
	AttackType:         CombatAttackShield,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntitySwarm,
	DamageType:         CombatDamageArea,
	DamageValue:        0,
	EffectMask:         CombatEffectKinetic,
	ChainAttack:        nil,
	CollisionProfile:   &physics.ShieldToSwarm,
}

var CombatAttackShieldToStorm = CombatAttackProfile{
	AttackType:         CombatAttackShield,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityStorm,
	DamageType:         CombatDamageArea,
	DamageValue:        0,
	EffectMask:         CombatEffectKinetic,
	CollisionProfile:   &physics.ShieldToQuasar, // Reuse quasar profile
}

var CombatAttackShieldToSnakeHead = CombatAttackProfile{
	AttackType:         CombatAttackShield,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntitySnakeHead,
	DamageType:         CombatDamageArea,
	DamageValue:        0,
	EffectMask:         CombatEffectKinetic,
	ChainAttack:        nil,
	CollisionProfile:   &physics.ShieldToQuasar, // Reuse quasar profile
}

var CombatAttackShieldToSnakeBody = CombatAttackProfile{
	AttackType:         CombatAttackShield,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntitySnakeBody,
	DamageType:         CombatDamageArea,
	DamageValue:        0,
	EffectMask:         CombatEffectKinetic,
	ChainAttack:        nil,
	CollisionProfile:   &physics.ShieldToSwarm, // Reuse swarm profile
}

// Explosion attack profiles

var CombatAttackExplosionToDrain = CombatAttackProfile{
	AttackType:         CombatAttackExplosion,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityDrain,
	DamageType:         CombatDamageArea,
	DamageValue:        parameter.CombatDamageExplosion,
	EffectMask:         CombatEffectKinetic,
	ChainAttack:        nil,
	CollisionProfile:   &physics.ExplosionToDrain,
}

var CombatAttackExplosionToQuasar = CombatAttackProfile{
	AttackType:         CombatAttackExplosion,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityQuasar,
	DamageType:         CombatDamageArea,
	DamageValue:        parameter.CombatDamageExplosion,
	EffectMask:         CombatEffectKinetic,
	ChainAttack:        nil,
	CollisionProfile:   &physics.ExplosionToQuasar,
}

var CombatAttackExplosionToSwarm = CombatAttackProfile{
	AttackType:         CombatAttackExplosion,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntitySwarm,
	DamageType:         CombatDamageArea,
	DamageValue:        parameter.CombatDamageExplosion,
	EffectMask:         CombatEffectKinetic,
	ChainAttack:        nil,
	CollisionProfile:   &physics.ExplosionToSwarm,
}

var CombatAttackExplosionToStorm = CombatAttackProfile{
	AttackType:         CombatAttackExplosion,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityStorm,
	DamageType:         CombatDamageArea,
	DamageValue:        parameter.CombatDamageExplosion,
	EffectMask:         CombatEffectKinetic,
	ChainAttack:        nil,
	CollisionProfile:   &physics.ExplosionToQuasar, // Reuse quasar profile
}

var CombatAttackExplosionToPylon = CombatAttackProfile{
	AttackType:         CombatAttackExplosion,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityPylon,
	DamageType:         CombatDamageArea,
	DamageValue:        parameter.CombatDamageExplosion,
	EffectMask:         CombatEffectNone,
	ChainAttack:        nil,
	CollisionProfile:   nil,
}

var CombatAttackExplosionToSnakeHead = CombatAttackProfile{
	AttackType:         CombatAttackExplosion,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntitySnakeHead,
	DamageType:         CombatDamageArea,
	DamageValue:        parameter.CombatDamageExplosion,
	EffectMask:         CombatEffectKinetic,
	ChainAttack:        nil,
	CollisionProfile:   &physics.ExplosionToQuasar, // Reuse quasar profile
}

var CombatAttackExplosionToSnakeBody = CombatAttackProfile{
	AttackType:         CombatAttackExplosion,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntitySnakeBody,
	DamageType:         CombatDamageArea,
	DamageValue:        parameter.CombatDamageExplosion,
	EffectMask:         CombatEffectKinetic,
	ChainAttack:        nil,
	CollisionProfile:   &physics.ExplosionToSwarm,
}

// Missile attack profiles

var CombatAttackMissileToDrain = CombatAttackProfile{
	AttackType:         CombatAttackMissile,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityDrain,
	DamageType:         CombatDamageArea,
	DamageValue:        parameter.CombatDamageMissile,
	EffectMask:         CombatEffectNone,
	ChainAttack:        nil,
	CollisionProfile:   &physics.ExplosionToDrain,
}

var CombatAttackMissileToQuasar = CombatAttackProfile{
	AttackType:         CombatAttackMissile,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityQuasar,
	DamageType:         CombatDamageArea,
	DamageValue:        parameter.CombatDamageMissile,
	EffectMask:         CombatEffectNone,
	ChainAttack:        nil,
	CollisionProfile:   &physics.ExplosionToQuasar,
}

var CombatAttackMissileToSwarm = CombatAttackProfile{
	AttackType:         CombatAttackMissile,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntitySwarm,
	DamageType:         CombatDamageArea,
	DamageValue:        parameter.CombatDamageMissile,
	EffectMask:         CombatEffectNone,
	ChainAttack:        nil,
	CollisionProfile:   &physics.ExplosionToSwarm,
}

var CombatAttackMissileToStorm = CombatAttackProfile{
	AttackType:         CombatAttackMissile,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityStorm,
	DamageType:         CombatDamageArea,
	DamageValue:        parameter.CombatDamageMissile,
	EffectMask:         CombatEffectNone,
	ChainAttack:        nil,
	CollisionProfile:   &physics.ExplosionToQuasar,
}

var CombatAttackMissileToPylon = CombatAttackProfile{
	AttackType:         CombatAttackMissile,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityPylon,
	DamageType:         CombatDamageArea,
	DamageValue:        parameter.CombatDamageMissile,
	EffectMask:         CombatEffectNone,
}

var CombatAttackMissileToSnakeHead = CombatAttackProfile{
	AttackType:         CombatAttackMissile,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntitySnakeHead,
	DamageType:         CombatDamageArea,
	DamageValue:        parameter.CombatDamageMissile,
	EffectMask:         CombatEffectNone,
	ChainAttack:        nil,
	CollisionProfile:   &physics.ExplosionToQuasar,
}

var CombatAttackMissileToSnakeBody = CombatAttackProfile{
	AttackType:         CombatAttackMissile,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntitySnakeBody,
	DamageType:         CombatDamageArea,
	DamageValue:        parameter.CombatDamageMissile,
	EffectMask:         CombatEffectNone,
	ChainAttack:        nil,
	CollisionProfile:   &physics.ExplosionToSwarm,
}

// Pulse
var CombatAttackPulseToDrain = CombatAttackProfile{
	AttackType:         CombatAttackPulse,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityDrain,
	DamageType:         CombatDamageArea,
	DamageValue:        parameter.CombatDamagePulse,
	EffectMask:         CombatEffectStun,
}

var CombatAttackPulseToQuasar = CombatAttackProfile{
	AttackType:         CombatAttackPulse,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityQuasar,
	DamageType:         CombatDamageArea,
	DamageValue:        parameter.CombatDamagePulse,
	EffectMask:         CombatEffectStun,
}

var CombatAttackPulseToSwarm = CombatAttackProfile{
	AttackType:         CombatAttackPulse,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntitySwarm,
	DamageType:         CombatDamageArea,
	DamageValue:        parameter.CombatDamagePulse,
	EffectMask:         CombatEffectStun,
}

var CombatAttackPulseToStorm = CombatAttackProfile{
	AttackType:         CombatAttackPulse,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityStorm,
	DamageType:         CombatDamageArea,
	DamageValue:        parameter.CombatDamagePulse,
	EffectMask:         CombatEffectStun,
}

var CombatAttackPulseToPylon = CombatAttackProfile{
	AttackType:         CombatAttackPulse,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityPylon,
	DamageType:         CombatDamageArea,
	DamageValue:        parameter.CombatDamagePulse,
	EffectMask:         CombatEffectStun,
}

var CombatAttackPulseToSnakeHead = CombatAttackProfile{
	AttackType:         CombatAttackPulse,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntitySnakeHead,
	DamageType:         CombatDamageArea,
	DamageValue:        parameter.CombatDamagePulse,
	EffectMask:         CombatEffectStun,
	ChainAttack:        nil,
	CollisionProfile:   nil,
}

var CombatAttackPulseToSnakeBody = CombatAttackProfile{
	AttackType:         CombatAttackPulse,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntitySnakeBody,
	DamageType:         CombatDamageArea,
	DamageValue:        parameter.CombatDamagePulse,
	EffectMask:         CombatEffectStun,
	ChainAttack:        nil,
	CollisionProfile:   nil,
}