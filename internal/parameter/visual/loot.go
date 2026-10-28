package visual

import (
	"github.com/lixenwraith/vif/internal/component"
)

// LootVisuals is the code kinds' looks, indexed by LootType. Weapon loot is
// lettered for its attack (Lightning, Missile, Pulse, Bullet, Ray, Tracer), the
// name players use.
var LootVisuals = [component.LootCount]component.LootLook{
	component.LootRod: {
		Rune:  'L',
		Color: RgbOrbRod,
		Glow:  RgbLootRodGlow,
	},
	component.LootLauncher: {
		Rune:  'M',
		Color: RgbOrbLauncher,
		Glow:  RgbLootLauncherGlow,
	},
	component.LootDisruptor: {
		Rune:  'P',
		Color: RgbOrbDisruptor,
		Glow:  RgbLootDisruptorGlow,
	},
	component.LootTurret: {
		Rune:  'B',
		Color: RgbOrbTurret,
		Glow:  RgbLootTurretGlow,
	},
	component.LootEmitter: {
		Rune:  'R',
		Color: RgbOrbEmitter,
		Glow:  RgbLootEmitterGlow,
	},
	component.LootGun: {
		Rune:  'T',
		Color: RgbTracerPositive,
		Glow:  RgbLootGunGlow,
	},
	component.LootHeat: {
		Rune:  'H',
		Color: RgbLootHeatSigil,
		Glow:  RgbLootHeatGlow,
	},
	component.LootEnergy: {
		Rune:  'E',
		Color: RgbLootEnergySigil,
		Glow:  RgbLootEnergyGlow,
	},
}
