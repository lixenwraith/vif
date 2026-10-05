package visual

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/component"
)

// LootVisualDef defines rendering properties for a loot type
type LootVisualDef struct {
	Rune       rune
	InnerColor color.RGB // Rune color
	GlowColor  color.RGB // Shield glow color
}

// LootVisuals is indexed by LootType. Weapon loot is lettered for its attack
// (Lightning, Missile, Pulse, Bullet, Ray), the name players use.
var LootVisuals = [component.LootCount]LootVisualDef{
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
	component.LootEmitter: {
		Rune:       'R',
		InnerColor: RgbOrbEmitter,
		GlowColor:  RgbLootEmitterGlow,
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
