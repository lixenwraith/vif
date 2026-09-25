package component

import (
	"time"

	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// WeaponType names one weapon kind; WeaponSpecs holds what it does
type WeaponType int

const (
	WeaponRod WeaponType = iota
	WeaponLauncher
	WeaponDisruptor
	WeaponCount
)

// WeaponDelivery is the mechanism a weapon discharges through
type WeaponDelivery uint8

const (
	DeliveryLightning WeaponDelivery = iota // instant direct hit per target
	DeliveryMissile                         // homing projectile, area damage on impact
	DeliveryPulse                           // area burst at the emitter, fired only on a target inside it
)

// Aimed reports whether the delivery needs targets assigned before it fires
func (d WeaponDelivery) Aimed() bool { return d != DeliveryPulse }

// WeaponSpec is one weapon kind's static profile, whatever carries it
type WeaponSpec struct {
	Name       string // status key and command name
	Delivery   WeaponDelivery
	Attack     CombatAttackType
	Cooldown   time.Duration
	MaxCharges int
}

// WeaponSpecs is indexed by WeaponType
var WeaponSpecs = [WeaponCount]WeaponSpec{
	WeaponRod: {Name: "rod", Delivery: DeliveryLightning, Attack: CombatAttackLightning,
		Cooldown: parameter.WeaponCooldownRod, MaxCharges: parameter.WeaponMaxChargeRod},
	WeaponLauncher: {Name: "launcher", Delivery: DeliveryMissile, Attack: CombatAttackMissile,
		Cooldown: parameter.WeaponCooldownLauncher, MaxCharges: parameter.WeaponMaxChargeLauncher},
	WeaponDisruptor: {Name: "disruptor", Delivery: DeliveryPulse, Attack: CombatAttackPulse,
		Cooldown: parameter.WeaponCooldownDisruptor, MaxCharges: parameter.WeaponMaxChargeDisruptor},
}

// WeaponComponent is a cursor's loadout: charges and cooldown per kind, and main fire's cooldown.
// Charges[wt] == 0 means weapon not owned; availability derives from Charges, no separate flag
type WeaponComponent struct {
	Charges          [WeaponCount]int
	Cooldown         [WeaponCount]time.Duration
	MainFireCooldown time.Duration
}
