package component

import (
	"time"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/physics"
)

type CombatEntityType int

const (
	CombatTypeCursor CombatEntityType = iota
	CombatTypeCleaner
	CombatTypeDrain
	CombatTypeQuasar
	CombatTypeSwarm
	CombatTypeStorm
)

// Damage Types
type CombatDamageType int

const (
	CombatDamageNone CombatDamageType = iota
	CombatDamageDirect
	CombatDamageArea
	CombatDamageDOT // Future
)

// Hit Types
type CombatHitType int

const (
	CombatHitMass CombatHitType = iota
	CombatHitEnergy
)

// CombatComponent tags an entity to be identified as enemy for interactions
type CombatComponent struct {
	// OwnerEntity indicates owner/parent of the entity with combat component (e.g. cursor is the parent of cleaner)
	OwnerEntity core.Entity

	// CombatType
	CombatType CombatEntityType

	// HitPoints is the remaining hit points of the combat entity (>0)
	HitPoints int

	// IsEnraged is the enrage indicator that modifies combat behavior
	IsEnraged bool

	// DamageImmunityRemaining is remaining immunity time for damage
	DamageImmunityRemaining time.Duration

	// HitFlashRemaining is the remaining duration of hit visual feedback
	HitFlashRemaining time.Duration

	// KineticImmunityRemaining is remaining immunity time for collision knockback
	KineticImmunityRemaining time.Duration
}

type CombatProfile struct {
	DamageType       CombatDamageType
	DamageValue      int
	HitType          CombatHitType
	CollisionProfile *physics.CollisionProfile
}

type CombatMatrixKey [2]CombatEntityType

type CombatMatrixMap map[CombatMatrixKey]CombatProfile

var (
	CombatMatrix = CombatMatrixMap{
		{CombatTypeCleaner, CombatTypeDrain}: CombatProfile{
			DamageType:       CombatDamageDirect,
			DamageValue:      1,
			HitType:          CombatHitMass,
			CollisionProfile: &physics.CleanerToDrain,
		},
		{CombatTypeCleaner, CombatTypeQuasar}: CombatProfile{
			DamageType:       CombatDamageDirect,
			DamageValue:      1,
			HitType:          CombatHitMass,
			CollisionProfile: &physics.CleanerToQuasar,
		},
	}
)