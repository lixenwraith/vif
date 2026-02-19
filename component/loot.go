package component

import (
	"github.com/lixenwraith/vi-fighter/parameter"
)

// DropEntry defines a single drop possibility
type DropEntry struct {
	Loot          LootType
	BaseRate      float64
	Count         int // Items dropped on success (0 treated as 1)
	FallbackCount int // Bonus added to next tier when unique entry skipped
}

// DropTier represents a priority level in the drop table
type DropTier struct {
	Unique  bool // If true, skip tier when all entries owned/active
	Entries []DropEntry
}

// SpeciesDropTable defines ordered tiers for a species
type SpeciesDropTable struct {
	Tiers []DropTier
}

// DropTables defines drop behavior per species
var DropTables = map[SpeciesType]SpeciesDropTable{
	SpeciesDrain: {
		Tiers: []DropTier{
			{Unique: false, Entries: []DropEntry{{LootHeat, 0.05, 1, 0}}},
		},
	},
	SpeciesQuasar: {
		Tiers: []DropTier{
			{Unique: true, Entries: []DropEntry{{LootRod, 1.0, 1, 2}}},
			{Unique: false, Entries: []DropEntry{{LootEnergy, 1.0, 1, 0}}},
		},
	},
	SpeciesSwarm: {
		Tiers: []DropTier{
			{Unique: true, Entries: []DropEntry{{LootLauncher, 0.10, 1, 0}}},
			{Unique: false, Entries: []DropEntry{{LootEnergy, 0.20, 1, 0}}},
		},
	},
	SpeciesStorm: {
		Tiers: []DropTier{
			{Unique: true, Entries: []DropEntry{{LootDisruptor, 1.0, 1, 2}}},
			{Unique: false, Entries: []DropEntry{{LootEnergy, 1.0, 3, 0}}},
		},
	},
}

// LootType identifies collectible loot drops
type LootType uint8

const (
	LootRod LootType = iota
	LootLauncher
	LootDisruptor
	LootHeat
	LootEnergy
	// Future loot types here
	LootCount // Sentinel for array sizing
)

// RewardType discriminates reward behavior
type RewardType uint8

const (
	RewardNone RewardType = iota
	RewardWeapon
	RewardHeat
	RewardEnergy
	RewardEvent
)

// RewardProfile defines what happens when loot is collected
// Tagged union: only field matching Type is valid
type RewardProfile struct {
	Type       RewardType
	WeaponType WeaponType // RewardWeapon
	Delta      int        // RewardEnergy, RewardHeat
	// Future: EventType for RewardEvent
}

// LootProfile defines a loot type's behavior
type LootProfile struct {
	Reward *RewardProfile // nil = no reward (visual-only loot)
}

// LootComponent represents a collectible loot drop entity
type LootComponent struct {
	Type LootType

	// Homing state
	Homing   bool
	PreciseX int64 // Q32.32
	PreciseY int64
	VelX     int64
	VelY     int64

	// Grid tracking
	LastIntX int
	LastIntY int
}

// --- Reward Profiles (pre-instantiated) ---

var (
	rewardRod = RewardProfile{
		Type:       RewardWeapon,
		WeaponType: WeaponRod,
	}

	rewardLauncher = RewardProfile{
		Type:       RewardWeapon,
		WeaponType: WeaponLauncher,
	}

	rewardDisruptor = RewardProfile{
		Type:       RewardWeapon,
		WeaponType: WeaponDisruptor,
	}

	rewardHeat = RewardProfile{
		Type:  RewardHeat,
		Delta: parameter.LootHeatRewardValue,
	}

	rewardEnergy = RewardProfile{
		Type:  RewardEnergy,
		Delta: parameter.LootEnergyRewardValue,
	}
)

// LootProfiles indexed by LootType
var LootProfiles = [LootCount]LootProfile{
	LootRod:       {Reward: &rewardRod},
	LootLauncher:  {Reward: &rewardLauncher},
	LootDisruptor: {Reward: &rewardDisruptor},
	LootHeat:      {Reward: &rewardHeat},
	LootEnergy:    {Reward: &rewardEnergy},
}