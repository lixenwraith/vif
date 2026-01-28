package component

import (
	"time"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/physics"
)

// TODO: change name, this is not entity anymore
type CombatEntityType int

const (
	CombatEntityCursor CombatEntityType = iota
	CombatEntityCleaner
	CombatEntityDrain
	CombatEntityQuasar
	CombatEntitySwarm
	CombatEntityStorm
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
		{CombatEntityCleaner, CombatEntityDrain}:  &CombatAttackCleanerToDrain,
		{CombatEntityCleaner, CombatEntityQuasar}: &CombatAttackCleanerToQuasar,
		{CombatEntityCleaner, CombatEntitySwarm}:  &CombatAttackCleanerToSwarm,
	},
	CombatAttackShield: {
		{CombatEntityCursor, CombatEntityDrain}:  &CombatAttackShieldToDrain,
		{CombatEntityCursor, CombatEntityQuasar}: &CombatAttackShieldToQuasar,
		{CombatEntityCursor, CombatEntitySwarm}:  &CombatAttackShieldToSwarm,
	},
	CombatAttackLightning: {
		{CombatEntityCursor, CombatEntityDrain}:  &CombatAttackLightningToDrain,
		{CombatEntityCursor, CombatEntityQuasar}: &CombatAttackLightningToQuasar,
		{CombatEntityCursor, CombatEntitySwarm}:  &CombatAttackLightningToSwarm,
	},
	CombatAttackExplosion: {
		{CombatEntityCursor, CombatEntityDrain}:  &CombatAttackExplosionToDrain,
		{CombatEntityCursor, CombatEntityQuasar}: &CombatAttackExplosionToQuasar,
		{CombatEntityCursor, CombatEntitySwarm}:  &CombatAttackExplosionToSwarm,
	},
}

// Combat attack profiles - pre-defined for zero allocation in hot path

var CombatAttackCleanerToDrain = CombatAttackProfile{
	AttackType:         CombatAttackProjectile,
	AttackerEntityType: CombatEntityCleaner,
	DefenderEntityType: CombatEntityDrain,
	DamageType:         CombatDamageDirect,
	DamageValue:        1,
	EffectMask:         CombatEffectKinetic,
	ChainAttack:        &CombatAttackLightningToDrain,
	// TODO: migrate collision to matrix
	CollisionProfile: &physics.CleanerToDrain,
}

var CombatAttackCleanerToQuasar = CombatAttackProfile{
	AttackType:         CombatAttackProjectile,
	AttackerEntityType: CombatEntityCleaner,
	DefenderEntityType: CombatEntityQuasar,
	DamageType:         CombatDamageDirect,
	DamageValue:        1,
	EffectMask:         CombatEffectKinetic,
	ChainAttack:        &CombatAttackLightningToQuasar,
	CollisionProfile:   &physics.CleanerToQuasar,
}

var CombatAttackLightningToDrain = CombatAttackProfile{
	AttackType:         CombatAttackLightning,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityDrain,
	DamageType:         CombatDamageDirect,
	DamageValue:        1,
	EffectMask:         CombatEffectVampireDrain,
	ChainAttack:        nil,
	CollisionProfile:   nil,
}

var CombatAttackLightningToQuasar = CombatAttackProfile{
	AttackType:         CombatAttackLightning,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityDrain,
	DamageType:         CombatDamageDirect,
	DamageValue:        1,
	EffectMask:         CombatEffectVampireDrain,
	ChainAttack:        nil,
	CollisionProfile:   nil,
}

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

var CombatAttackExplosionToDrain = CombatAttackProfile{
	AttackType:         CombatAttackExplosion,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityDrain,
	DamageType:         CombatDamageArea,
	DamageValue:        1,
	EffectMask:         CombatEffectKinetic,
	ChainAttack:        nil,
	CollisionProfile:   &physics.ExplosionToDrain,
}

var CombatAttackExplosionToQuasar = CombatAttackProfile{
	AttackType:         CombatAttackExplosion,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntityQuasar,
	DamageType:         CombatDamageArea,
	DamageValue:        1,
	EffectMask:         CombatEffectKinetic,
	ChainAttack:        nil,
	CollisionProfile:   &physics.ExplosionToQuasar,
}

var CombatAttackCleanerToSwarm = CombatAttackProfile{
	AttackType:         CombatAttackProjectile,
	AttackerEntityType: CombatEntityCleaner,
	DefenderEntityType: CombatEntitySwarm,
	DamageType:         CombatDamageDirect,
	DamageValue:        1,
	EffectMask:         CombatEffectKinetic,
	ChainAttack:        &CombatAttackLightningToSwarm,
	CollisionProfile:   &physics.CleanerToSwarm,
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

var CombatAttackLightningToSwarm = CombatAttackProfile{
	AttackType:         CombatAttackLightning,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntitySwarm,
	DamageType:         CombatDamageDirect,
	DamageValue:        1,
	EffectMask:         CombatEffectVampireDrain,
	ChainAttack:        nil,
	CollisionProfile:   nil,
}

var CombatAttackExplosionToSwarm = CombatAttackProfile{
	AttackType:         CombatAttackExplosion,
	AttackerEntityType: CombatEntityCursor,
	DefenderEntityType: CombatEntitySwarm,
	DamageType:         CombatDamageArea,
	DamageValue:        1,
	EffectMask:         CombatEffectKinetic,
	ChainAttack:        nil,
	CollisionProfile:   &physics.ExplosionToSwarm,
}