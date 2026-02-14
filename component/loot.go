package component

import (
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/terminal"
)

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
	SpeciesQuasar: {
		Tiers: []DropTier{
			{Unique: true, Entries: []DropEntry{{LootRod, 1.0}}},
			{Unique: false, Entries: []DropEntry{{LootEnergy, 1.0}}},
		},
	},
	SpeciesSwarm: {
		Tiers: []DropTier{
			{Unique: true, Entries: []DropEntry{{LootLauncher, 0.10}}},
			{Unique: false, Entries: []DropEntry{{LootEnergy, 0.20}}},
		},
	},
}

// LootType identifies collectible loot drops
type LootType uint8

const (
	LootRod LootType = iota
	LootLauncher
	LootSpray
	LootEnergy
	// Future loot types here
	LootCount // Sentinel for array sizing
)

// RewardType discriminates reward behavior
type RewardType uint8

const (
	RewardNone RewardType = iota
	RewardWeapon
	RewardEnergy
	RewardHeat
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

	rewardEnergy = RewardProfile{
		Type:  RewardEnergy,
		Delta: parameter.LootEnergyRewardValue,
	}
)

// LootProfiles indexed by LootType
var LootProfiles = [LootCount]LootProfile{
	LootRod:      {Reward: &rewardRod},
	LootLauncher: {Reward: &rewardLauncher},
	LootSpray:    {Reward: &rewardLauncher}, // TODO: placeholder
	LootEnergy:   {Reward: &rewardEnergy},
}

// --- Drop Tables ---

// DropEntry defines a single drop possibility
type DropEntry struct {
	Loot     LootType
	BaseRate float64
}

// LootVisualDef defines rendering properties for a loot type
type LootVisualDef struct {
	Rune       rune
	InnerColor terminal.RGB // Sigil color
	GlowColor  terminal.RGB // Shield glow color
}

// LootVisuals defines the visual attributes of loot
// Can't be cleanly placed in parameters due to cyclic dependency
var LootVisuals = map[LootType]LootVisualDef{
	LootRod: {
		Rune:       'L',
		InnerColor: visual.RgbOrbRod,
		GlowColor:  visual.RgbLootRodGlow,
	},
	LootLauncher: {
		Rune:       'M',
		InnerColor: visual.RgbOrbLauncher,
		GlowColor:  visual.RgbLootLauncherGlow,
	},
	LootEnergy: {
		Rune:       'E',
		InnerColor: visual.RgbOrbEnergy,
		GlowColor:  visual.RgbLootEnergyGlow,
	},
}