package component

import (
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/parameter"
)

// LootType names one loot kind; LootSpecs holds what collecting it grants
type LootType uint8

const (
	LootRod LootType = iota
	LootLauncher
	LootDisruptor
	LootHeat
	LootEnergy
	LootTurret
	LootEmitter
	LootGun
	LootUpgrade // the gun's next level, a scenario's event drop rather than a table's
	LootCount   // Sentinel for array sizing
)

// RewardType is what collecting a loot kind changes
type RewardType uint8

const (
	RewardNone RewardType = iota
	RewardWeapon
	RewardHeat
	RewardEnergy
)

// LootSpec is one loot kind: the name a document calls it by, and its reward, a
// charge of Weapon, or Delta heat or energy
type LootSpec struct {
	Name   string
	Reward RewardType
	Weapon WeaponType
	Delta  int
}

// LootSpecs is indexed by LootType
var LootSpecs = [LootCount]LootSpec{
	LootRod:       {Name: "rod", Reward: RewardWeapon, Weapon: WeaponRod},
	LootLauncher:  {Name: "launcher", Reward: RewardWeapon, Weapon: WeaponLauncher},
	LootDisruptor: {Name: "disruptor", Reward: RewardWeapon, Weapon: WeaponDisruptor},
	LootHeat:      {Name: "heat", Reward: RewardHeat, Delta: parameter.LootHeatRewardValue},
	LootEnergy:    {Name: "energy", Reward: RewardEnergy, Delta: parameter.LootEnergyRewardValue},
	LootTurret:    {Name: "turret", Reward: RewardWeapon, Weapon: WeaponTurret},
	LootEmitter:   {Name: "emitter", Reward: RewardWeapon, Weapon: WeaponEmitter},
	LootGun:       {Name: "gun", Reward: RewardWeapon, Weapon: WeaponGun},
	LootUpgrade:   {Name: "upgrade", Reward: RewardWeapon, Weapon: WeaponGun},
}

// LootComponent represents a collectible loot drop entity
type LootComponent struct {
	Type LootType

	// Owner is the cursor this drop belongs to; loot is personal
	Owner core.Entity

	// Grid tracking
	LastIntX int
	LastIntY int
}
