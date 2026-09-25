package visual

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/component"
)

// --- Drop Tables ---

// LootVisualDef defines rendering properties for a loot type
type LootVisualDef struct {
	Rune       rune
	InnerColor color.RGB // Sigil color
	GlowColor  color.RGB // Shield glow color
}

// LootVisuals defines the visual attributes of loot
// Can't be cleanly placed in parameters due to cyclic dependency
// Weapon loot is lettered for its attack (Lightning, Missile, Pulse, Bullet, Ray), the name players use
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
		Rune:       'P',
		InnerColor: RgbOrbDisruptor,
		GlowColor:  RgbLootDisruptorGlow,
	},
	component.LootTurret: {
		Rune:       'B',
		InnerColor: RgbOrbTurret,
		GlowColor:  RgbLootTurretGlow,
	},
	component.LootBeam: {
		Rune:       'R',
		InnerColor: RgbOrbBeam,
		GlowColor:  RgbLootBeamGlow,
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
