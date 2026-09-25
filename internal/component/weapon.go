package component

import (
	"time"

	"github.com/lixenwraith/vif/internal/parameter"
)

// WeaponType names one weapon kind; WeaponSpecs holds what it does
type WeaponType int

const (
	WeaponRod WeaponType = iota
	WeaponLauncher
	WeaponDisruptor
	WeaponTurret
	WeaponBeam
	WeaponCount
)

// WeaponDelivery is the mechanism a weapon discharges through
type WeaponDelivery uint8

const (
	DeliveryLightning WeaponDelivery = iota // instant direct hit per target
	DeliveryMissile                         // homing projectile, area damage on impact
	DeliveryPulse                           // area burst at the emitter, fired only on a target inside it
	DeliveryBullet                          // linear projectile per shot, direct damage on contact
	DeliveryBeam                            // straight band to the first wall, area damage along it
)

// Aimed reports whether the delivery needs targets assigned before it fires
func (d WeaponDelivery) Aimed() bool { return d != DeliveryPulse }

// WeaponSpec is one weapon kind's static profile, whatever carries it. A cursor's
// discharge resolves against species through Attack; a hosted one strikes cursors.
type WeaponSpec struct {
	Name       string // status key and command name
	Delivery   WeaponDelivery
	Attack     CombatAttackType
	Cooldown   time.Duration // a cursor's; a mount without an interval of its own takes it too
	MaxCharges int

	HostedRange  float64      // cells a mounted weapon reaches, horizontally; vertical is half
	HostedDamage CursorDamage // what a mounted weapon's hit costs a cursor
}

// CursorDamage is a hit on a cursor: energy drained through an active shield,
// heat changed without one (negative reduces)
type CursorDamage struct {
	EnergyDrain int
	HeatDelta   int
}

// WeaponPalette selects the colours a discharge draws with
type WeaponPalette uint8

const (
	PalettePositive WeaponPalette = iota // a cursor's weapon at non-negative energy
	PaletteNegative                      // a cursor's weapon at negative energy
	PaletteHostile                       // a mounted weapon
	PaletteCount
)

// WeaponSpecs is indexed by WeaponType
var WeaponSpecs = [WeaponCount]WeaponSpec{
	WeaponRod: {Name: "rod", Delivery: DeliveryLightning, Attack: CombatAttackLightning,
		Cooldown: parameter.WeaponCooldownRod, MaxCharges: parameter.WeaponMaxChargeRod,
		HostedRange:  parameter.HostedRodRange,
		HostedDamage: CursorDamage{parameter.HostedRodEnergy, -parameter.HostedRodHeat}},
	WeaponLauncher: {Name: "launcher", Delivery: DeliveryMissile, Attack: CombatAttackMissile,
		Cooldown: parameter.WeaponCooldownLauncher, MaxCharges: parameter.WeaponMaxChargeLauncher,
		HostedRange:  parameter.HostedLauncherRange,
		HostedDamage: CursorDamage{parameter.HostedLauncherEnergy, -parameter.HostedLauncherHeat}},
	WeaponDisruptor: {Name: "disruptor", Delivery: DeliveryPulse, Attack: CombatAttackPulse,
		Cooldown: parameter.WeaponCooldownDisruptor, MaxCharges: parameter.WeaponMaxChargeDisruptor,
		HostedRange:  parameter.PulseRadiusX,
		HostedDamage: CursorDamage{parameter.HostedDisruptorEnergy, -parameter.HostedDisruptorHeat}},
	WeaponTurret: {Name: "turret", Delivery: DeliveryBullet, Attack: CombatAttackBullet,
		Cooldown: parameter.WeaponCooldownTurret, MaxCharges: parameter.WeaponMaxChargeTurret,
		HostedRange:  parameter.HostedTurretRange,
		HostedDamage: CursorDamage{parameter.HostedTurretEnergy, -parameter.HostedTurretHeat}},
	WeaponBeam: {Name: "beam", Delivery: DeliveryBeam, Attack: CombatAttackBeam,
		Cooldown: parameter.WeaponCooldownBeam, MaxCharges: parameter.WeaponMaxChargeBeam,
		HostedRange:  parameter.HostedBeamRange,
		HostedDamage: CursorDamage{parameter.HostedBeamEnergy, -parameter.HostedBeamHeat}},
}

// WeaponComponent is a cursor's loadout: charges and cooldown per kind, and main fire's cooldown.
// Charges[wt] == 0 means weapon not owned; availability derives from Charges, no separate flag
type WeaponComponent struct {
	Charges          [WeaponCount]int
	Cooldown         [WeaponCount]time.Duration
	MainFireCooldown time.Duration
}
