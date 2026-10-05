package profile

import "github.com/lixenwraith/vif/internal/component"

// DropEntry is one loot kind a tier can roll
type DropEntry struct {
	Loot          component.LootType
	BaseRate      float64
	Count         int // items dropped on success (0 treated as 1)
	FallbackCount int // added to every later non-unique tier when this unique entry is skipped
}

// DropTier is one roll among its entries. A unique tier skips the weapons a cursor
// holds at full charge or already has in flight.
type DropTier struct {
	Entries []DropEntry
	Unique  bool
}

// Drops holds each species' tiers in roll order; a species without a row drops nothing
var Drops = [component.SpeciesCount][]DropTier{
	component.SpeciesDrain: {
		{Entries: []DropEntry{{component.LootHeat, 0.05, 1, 0}}},
	},
	component.SpeciesQuasar: {
		{Unique: true, Entries: []DropEntry{{component.LootRod, 1.0, 1, 2}}},
		{Entries: []DropEntry{{component.LootEnergy, 1.0, 1, 0}}},
	},
	component.SpeciesSwarm: {
		{Unique: true, Entries: []DropEntry{{component.LootLauncher, 0.10, 1, 0}}},
		{Entries: []DropEntry{{component.LootEnergy, 0.20, 1, 0}}},
	},
	component.SpeciesStorm: {
		{Unique: true, Entries: []DropEntry{{component.LootDisruptor, 1.0, 1, 2}, {component.LootTurret, 1.0, 1, 0}}},
		{Entries: []DropEntry{{component.LootEnergy, 1.0, 3, 0}}},
	},
	component.SpeciesPylon: {
		{Unique: true, Entries: []DropEntry{{component.LootTurret, 1.0, 1, 2}, {component.LootEmitter, 1.0, 1, 0}}},
		{Entries: []DropEntry{{component.LootEnergy, 1.0, 3, 0}}},
	},
	component.SpeciesSnake: {
		{Unique: true, Entries: []DropEntry{{component.LootDisruptor, 1.0, 1, 2}}},
		{Entries: []DropEntry{{component.LootEnergy, 1.0, 3, 0}}},
	},
	component.SpeciesEye: {
		{Unique: true, Entries: []DropEntry{{component.LootRod, 0.05, 1, 0}}},
		{Unique: true, Entries: []DropEntry{{component.LootLauncher, 0.05, 1, 0}}},
		{Entries: []DropEntry{{component.LootEnergy, 0.10, 1, 0}}},
	},
	component.SpeciesKraken: {
		{Unique: true, Entries: []DropEntry{{component.LootEmitter, 1.0, 1, 1}}},
		{Entries: []DropEntry{{component.LootEnergy, 1.0, 2, 0}}},
		{Entries: []DropEntry{{component.LootHeat, 1.0, 2, 0}}},
	},
}
