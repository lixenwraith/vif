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
	LootCount // Sentinel for array sizing
)

// RewardType is what collecting a loot kind changes
type RewardType uint8

const (
	RewardNone RewardType = iota
	RewardWeapon
	RewardHeat
	RewardEnergy
)

// LootSpec is one loot kind's reward: a charge of Weapon, or Delta heat or energy
type LootSpec struct {
	Reward RewardType
	Weapon WeaponType
	Delta  int
}

// LootSpecs is indexed by LootType
var LootSpecs = [LootCount]LootSpec{
	LootRod:       {Reward: RewardWeapon, Weapon: WeaponRod},
	LootLauncher:  {Reward: RewardWeapon, Weapon: WeaponLauncher},
	LootDisruptor: {Reward: RewardWeapon, Weapon: WeaponDisruptor},
	LootHeat:      {Reward: RewardHeat, Delta: parameter.LootHeatRewardValue},
	LootEnergy:    {Reward: RewardEnergy, Delta: parameter.LootEnergyRewardValue},
	LootTurret:    {Reward: RewardWeapon, Weapon: WeaponTurret},
	LootEmitter:   {Reward: RewardWeapon, Weapon: WeaponEmitter},
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
