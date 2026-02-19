package visual

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// --- Drop Tables ---

// LootVisualDef defines rendering properties for a loot type
type LootVisualDef struct {
	Rune       rune
	InnerColor terminal.RGB // Sigil color
	GlowColor  terminal.RGB // Shield glow color
}

// LootVisuals defines the visual attributes of loot
// Can't be cleanly placed in parameters due to cyclic dependency
var LootVisuals = map[component.LootType]LootVisualDef{
	component.LootRod: {
		Rune:       'L',
		InnerColor: RgbOrbRod,
		GlowColor:  RgbLootRodGlow,
	},
	component.LootLauncher: {
		Rune:       'M',
		InnerColor: RgbOrbLauncher,
		GlowColor:  RgbLootLauncherGlow,
	},
	component.LootDisruptor: {
		Rune:       'D',
		InnerColor: RgbOrbDisruptor,
		GlowColor:  RgbLootDisruptorGlow,
	},
	component.LootHeat: {
		Rune:       'H',
		InnerColor: RgbLootHeatSigil,
		GlowColor:  RgbLootHeatGlow,
	},
	component.LootEnergy: {
		Rune:       'E',
		InnerColor: RgbLootEnergySigil,
		GlowColor:  RgbLootEnergyGlow,
	},
}