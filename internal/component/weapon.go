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
	WeaponEmitter
	WeaponGun // the innate main weapon
	WeaponCount
)

// WeaponDelivery is the mechanism a weapon discharges through
type WeaponDelivery uint8

const (
	DeliveryLightning WeaponDelivery = iota // instant direct hit per target
	DeliveryMissile                         // homing projectile, area damage on impact
	DeliveryPulse                           // area burst at the emitter, fired only on a target inside it
	DeliveryBullet                          // linear projectile per shot, direct damage on contact
	DeliveryRay                             // straight band to the first wall, area damage along it
	DeliveryTracer                          // cardinal streaks from the cursor: direct damage on contact, glyph effects by polarity
)

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

	// An innate kind is held from spawn, kept through an energy zero crossing and
	// never dropped; FromCursor fires from the cursor's cell and has no orb
	Innate     bool
	FromCursor bool
	Directions TracerDirection // the cardinals a tracer volley covers

	// What the kind gains at MaxCharges; a zero field changes nothing
	ArcRange      float64       // cells a hit arcs to the nearest other target within
	MaxedCooldown time.Duration // replaces Cooldown
	Stream        int           // bullets per stream shot while a wave's cooldown runs
	Sustained     bool          // the ray lasts while main fire is held
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

// WeaponPaletteNames are what a document calls each WeaponPalette
var WeaponPaletteNames = [PaletteCount]string{PalettePositive: "positive", PaletteNegative: "negative", PaletteHostile: "hostile"}

// WeaponSpecs is indexed by WeaponType
var WeaponSpecs = [WeaponCount]WeaponSpec{
	WeaponRod: {Name: "rod", Delivery: DeliveryLightning, Attack: CombatAttackLightning,
		Cooldown: parameter.WeaponCooldownRod, MaxCharges: parameter.WeaponMaxChargeRod,
		HostedRange:  parameter.HostedRodRange,
		HostedDamage: CursorDamage{parameter.HostedRodEnergy, -parameter.HostedRodHeat},
		ArcRange:     parameter.LightningArcRange},
	WeaponLauncher: {Name: "launcher", Delivery: DeliveryMissile, Attack: CombatAttackMissile,
		Cooldown: parameter.WeaponCooldownLauncher, MaxCharges: parameter.WeaponMaxChargeLauncher,
		HostedRange:   parameter.HostedLauncherRange,
		HostedDamage:  CursorDamage{parameter.HostedLauncherEnergy, -parameter.HostedLauncherHeat},
		MaxedCooldown: parameter.WeaponCooldownLauncherMaxed},
	WeaponDisruptor: {Name: "disruptor", Delivery: DeliveryPulse, Attack: CombatAttackPulse,
		Cooldown: parameter.WeaponCooldownDisruptor, MaxCharges: parameter.WeaponMaxChargeDisruptor,
		HostedRange:  parameter.PulseRadiusX,
		HostedDamage: CursorDamage{parameter.HostedDisruptorEnergy, -parameter.HostedDisruptorHeat}},
	WeaponTurret: {Name: "turret", Delivery: DeliveryBullet, Attack: CombatAttackBullet,
		Cooldown: parameter.WeaponCooldownTurret, MaxCharges: parameter.WeaponMaxChargeTurret,
		HostedRange:  parameter.HostedTurretRange,
		HostedDamage: CursorDamage{parameter.HostedTurretEnergy, -parameter.HostedTurretHeat},
		Stream:       parameter.TurretStream},
	WeaponEmitter: {Name: "emitter", Delivery: DeliveryRay, Attack: CombatAttackRay,
		Cooldown: parameter.WeaponCooldownEmitter, MaxCharges: parameter.WeaponMaxChargeEmitter,
		HostedRange:  parameter.HostedEmitterRange,
		HostedDamage: CursorDamage{parameter.HostedEmitterEnergy, -parameter.HostedEmitterHeat},
		Sustained:    true},
	WeaponGun: {Name: "gun", Delivery: DeliveryTracer, Attack: CombatAttackTracer,
		MaxCharges: parameter.WeaponMaxChargeGun, Innate: true, FromCursor: true, Directions: TracerAllDirections},
}

// GunLevel is one step of a gun's upgrade path: the lanes, up to the three a volley
// holds, it fires along each cardinal
type GunLevel struct {
	Right uint8 `toml:"right"`
	Left  uint8 `toml:"left"`
	Down  uint8 `toml:"down"`
	Up    uint8 `toml:"up"`
}

// WeaponComponent is a cursor's loadout: charges and cooldown per kind, and main fire's cooldown.
// Charges[wt] == 0 means weapon not owned; availability derives from Charges, no separate flag
type WeaponComponent struct {
	Charges          [WeaponCount]int
	Cooldown         [WeaponCount]time.Duration
	Stream           [WeaponCount]time.Duration // until a capped kind's next stream shot; runs only while its Cooldown does, and stays with its author
	MainFireCooldown time.Duration
	Aim              TracerDirection // the cardinals the gun fires along; zero fires its own

	// A scenario's upgrade path for the gun, GunLevels long: a gun of n charges fires
	// level n and holds a charge per level; zero levels leave it on Aim
	GunPath   [parameter.GunPathMax]GunLevel
	GunLevels int
}
