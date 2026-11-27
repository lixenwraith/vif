package component

import (
	"time"

	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/parameter"
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
	CombatEntityEye
	CombatEntityTower
	CombatEntityKraken
	CombatEntityDevourer
	CombatEntityDevourerGut
	CombatEntityCount
)

// CombatEntityNames indexes CombatEntityType for telemetry keys and the combat row
// a behavior program's prototype names
var CombatEntityNames = [CombatEntityCount]string{
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
	"kraken",
	"devourer",
	"devourer_gut",
}

// Damage Types
type CombatDamageType int

const (
	CombatDamageNone CombatDamageType = iota
	CombatDamageDirect
	CombatDamageArea
	CombatDamageOverTime // a status the direct resolver leaves, ticking from the profile's figures
)

// Attack Types
type CombatAttackType int

// CombatAttackNone selects no attack profile; effects carrying it are visual only
const CombatAttackNone CombatAttackType = -1

const (
	CombatAttackTracer CombatAttackType = iota
	CombatAttackShield
	CombatAttackLightning
	CombatAttackExplosion
	CombatAttackMissile
	CombatAttackPulse
	CombatAttackSelfDestruct
	CombatAttackBullet
	CombatAttackRay
	CombatAttackBurn      // a charged pulse's follow-up; nothing spends its immunity, so only a ward blocks it
	CombatAttackStrike    // a magic square's strike, all a body's hit points
	CombatAttackDeathWave // a death wave, all a body's hit points as its edge passes
	CombatAttackConfuse   // a magic square's tempest spell: no damage, a confuse
	CombatAttackTypeCount
)

// CombatAttackNames are what a document calls each attack family, and the key its
// damage reads under in telemetry
var CombatAttackNames = [CombatAttackTypeCount]string{
	CombatAttackTracer:       "tracer",
	CombatAttackShield:       "shield",
	CombatAttackLightning:    "lightning",
	CombatAttackExplosion:    "explosion",
	CombatAttackMissile:      "missile",
	CombatAttackPulse:        "pulse",
	CombatAttackSelfDestruct: "self_destruct",
	CombatAttackBullet:       "bullet",
	CombatAttackRay:          "ray",
	CombatAttackBurn:         "burn",
	CombatAttackStrike:       "strike",
	CombatAttackDeathWave:    "death_wave",
	CombatAttackConfuse:      "confuse",
}

// Effect Types
type CombatEffectMask uint64

const CombatEffectNone CombatEffectMask = 0
const (
	CombatEffectEnergyDrain CombatEffectMask = 1 << iota
	CombatEffectKinetic
	CombatEffectStatus // the hit leaves its profile's status
)

// StatusKind indexes CombatComponent.Status and StatusSpecs: one slot per kind, no
// stacking
type StatusKind uint8

const (
	StatusStun StatusKind = iota
	StatusBurn
	StatusWard
	StatusConfuse
	StatusCount
)

// StatusTick is what a status does each period while it runs
type StatusTick uint8

const (
	StatusTickNone   StatusTick = iota
	StatusTickDamage            // its magnitude in hit points, credited to its source
	StatusTickKick              // its body thrown a way drawn anew, at parameter.ConfuseKick
)

// StatusSpec is what a status does to the body it runs on, which every species reads
// alike: whether it holds the body still, refused by one shielded from being moved
// (Moves), lengthened by a hit while it runs or left to run out, landed on the member
// an ablative composite's hit struck, and its tick. A profile naming no duration or
// period leaves the status's own.
type StatusSpec struct {
	Name             string
	Duration, Period time.Duration
	Holds, Moves     bool
	Refresh, Member  bool
	Tick             StatusTick
}

// StatusSpecs holds what each status does; a ward is set by its own species, no hit
var StatusSpecs = [StatusCount]StatusSpec{
	StatusStun:    {Name: "stun", Duration: parameter.PulseStunDuration, Holds: true, Moves: true},
	StatusBurn:    {Name: "burn", Refresh: true, Member: true, Tick: StatusTickDamage},
	StatusWard:    {Name: "ward"},
	StatusConfuse: {Name: "confuse", Duration: parameter.ConfuseDuration, Period: parameter.ConfusePeriod, Moves: true, Refresh: true, Tick: StatusTickKick},
}

// Status is one effect a hit or the entity's own species left on a combat entity,
// aged by combat each tick
type Status struct {
	Remaining time.Duration    // zero: inactive
	Period    time.Duration    // between ticks of a periodic effect; zero: none
	Timer     time.Duration    // until the next tick
	Magnitude int              // what a tick applies
	Source    core.Entity      // crediting cursor; Shared, so capture-safe
	Attack    CombatAttackType // family a tick records under and checks immunity against
}

// CombatComponent tags an entity as combat-relevant for interactions.
type CombatComponent struct {
	// OwnerEntity indicates owner/parent of the entity with combat component (e.g. cursor is the parent of a tracer)
	OwnerEntity core.Entity

	// LastDamagedBy identifies the cursor that most recently dealt HP damage.
	// Zero means the last damaging attack was not owned by a live cursor.
	LastDamagedBy core.Entity

	// LastAttack is the family of that damage, which a kill's effect can turn on
	LastAttack CombatAttackType

	// CombatEntityType
	CombatEntityType CombatEntityType

	// HitPoints is the remaining hit points of the combat entity (>0)
	HitPoints int

	// IsEnraged is the enrage indicator that modifies combat behavior
	IsEnraged bool

	// RemainingDamageImmunity is remaining immunity time for damage
	RemainingDamageImmunity time.Duration

	// Each attack family has a separate allowance per cursor in the open window.
	DamageImmunitySpent [CombatAttackTypeCount]uint32

	// RemainingHitFlash is the remaining duration of hit visual feedback
	RemainingHitFlash time.Duration

	// HitEntity is what the last damaging hit on a unit landed on: the unit, or the
	// member struck, the middle one of an area's
	HitEntity core.Entity

	// RemainingKineticImmunity is remaining immunity time for collision knockback.
	// It is also how every steering system asks "is this target currently
	// displaced", so it stays the whole target's window whoever opened it.
	RemainingKineticImmunity time.Duration

	// KineticStrength is the impulse that opened the window; only a stronger one
	// lands while it runs. Zero, a window a species or soft push opened, refuses all.
	KineticStrength float64

	// Status is indexed by StatusKind
	Status [StatusCount]Status
}

// Held reports a status running that holds the body still
func (c *CombatComponent) Held() bool {
	for kind, spec := range StatusSpecs {
		if spec.Holds && c.Status[kind].Remaining > 0 {
			return true
		}
	}
	return false
}
