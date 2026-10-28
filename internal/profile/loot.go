package profile

import (
	"slices"

	"github.com/lixenwraith/vif/internal/component"
)

// Drops holds each code species' tiers in roll order; a species without a row drops
// nothing, and a scenario's table named for a species replaces its row
var Drops = [component.SpeciesCount][]component.DropTier{
	component.SpeciesDrain: {
		{Entries: []component.DropEntry{{Loot: component.LootHeat, BaseRate: 0.05, Count: 1}}},
	},
	component.SpeciesQuasar: {
		{Unique: true, Entries: []component.DropEntry{{Loot: component.LootRod, BaseRate: 1.0, Count: 1, FallbackCount: 2}}},
		{Entries: []component.DropEntry{{Loot: component.LootEnergy, BaseRate: 1.0, Count: 1}}},
	},
	component.SpeciesSwarm: {
		{Unique: true, Entries: []component.DropEntry{{Loot: component.LootLauncher, BaseRate: 0.10, Count: 1}}},
		{Entries: []component.DropEntry{{Loot: component.LootEnergy, BaseRate: 0.20, Count: 1}}},
	},
	component.SpeciesStorm: {
		{Unique: true, Entries: []component.DropEntry{{Loot: component.LootDisruptor, BaseRate: 1.0, Count: 1, FallbackCount: 2}, {Loot: component.LootTurret, BaseRate: 1.0, Count: 1}}},
		{Entries: []component.DropEntry{{Loot: component.LootEnergy, BaseRate: 1.0, Count: 3}}},
	},
	component.SpeciesPylon: {
		{Unique: true, Entries: []component.DropEntry{{Loot: component.LootTurret, BaseRate: 1.0, Count: 1, FallbackCount: 2}, {Loot: component.LootEmitter, BaseRate: 1.0, Count: 1}}},
		{Entries: []component.DropEntry{{Loot: component.LootEnergy, BaseRate: 1.0, Count: 3}}},
	},
	component.SpeciesSnake: {
		{Unique: true, Entries: []component.DropEntry{{Loot: component.LootDisruptor, BaseRate: 1.0, Count: 1, FallbackCount: 2}}},
		{Entries: []component.DropEntry{{Loot: component.LootEnergy, BaseRate: 1.0, Count: 3}}},
	},
	component.SpeciesEye: {
		{Unique: true, Entries: []component.DropEntry{{Loot: component.LootRod, BaseRate: 0.05, Count: 1}}},
		{Unique: true, Entries: []component.DropEntry{{Loot: component.LootLauncher, BaseRate: 0.05, Count: 1}}},
		{Entries: []component.DropEntry{{Loot: component.LootEnergy, BaseRate: 0.10, Count: 1}}},
	},
	component.SpeciesKraken: {
		{Unique: true, Entries: []component.DropEntry{{Loot: component.LootEmitter, BaseRate: 1.0, Count: 1, FallbackCount: 1}}},
		{Entries: []component.DropEntry{{Loot: component.LootEnergy, BaseRate: 1.0, Count: 2}}},
		{Entries: []component.DropEntry{{Loot: component.LootHeat, BaseRate: 1.0, Count: 2}}},
	},
}

// DropTableNamed is the table name names where set is declared: the scenario's
// own, else a code species' row, which any kill rolls
func DropTableNamed(set *component.LootSet, name string) (component.DropTable, bool) {
	if t, ok := set.Tables[name]; ok {
		return t, true
	}
	if i := slices.Index(component.SpeciesNames[:], name); i > int(component.SpeciesNone) {
		return component.DropTable{Tiers: Drops[i]}, true
	}
	return component.DropTable{}, false
}
