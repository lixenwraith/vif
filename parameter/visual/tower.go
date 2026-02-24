package visual

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// TowerTypeColors holds per-type health gradient, glow, and palette colors
type TowerTypeColors struct {
	// TrueColor health zones (bright=center, dark=edge)
	HealthyBright  terminal.RGB
	HealthyDark    terminal.RGB
	DamagedBright  terminal.RGB
	DamagedDark    terminal.RGB
	CriticalBright terminal.RGB
	CriticalDark   terminal.RGB

	// TrueColor glow
	GlowColor terminal.RGB

	// 256-color palette per health zone
	Palette256Healthy  uint8
	Palette256Damaged  uint8
	Palette256Critical uint8

	// Basic (8/16) color per health zone
	BasicHealthy  uint8
	BasicDamaged  uint8
	BasicCritical uint8
}

// TowerTypes defines visual properties indexed by TowerComponent.VisualType
var TowerTypes = [component.TowerTypeCount]TowerTypeColors{
	// Type 0: Cyan — Cyan → PaleGold → Coral
	{
		HealthyBright:      terminal.BrightCyan,
		HealthyDark:        terminal.Teal,
		DamagedBright:      terminal.PaleGold,
		DamagedDark:        terminal.Amber,
		CriticalBright:     terminal.Coral,
		CriticalDark:       terminal.Brick,
		GlowColor:          terminal.SkyTeal,
		Palette256Healthy:  terminal.P256Cyan,
		Palette256Damaged:  terminal.P256Gold,
		Palette256Critical: terminal.P256Crimson,
		BasicHealthy:       6, // Cyan
		BasicDamaged:       3, // Yellow
		BasicCritical:      1, // Red
	},
	// Type 1: Gold — Gold → WarmOrange → BrightRed
	{
		HealthyBright:      terminal.Gold,
		HealthyDark:        terminal.DarkGold,
		DamagedBright:      terminal.WarmOrange,
		DamagedDark:        terminal.BurntOrange,
		CriticalBright:     terminal.BrightRed,
		CriticalDark:       terminal.Brick,
		GlowColor:          terminal.Apricot,
		Palette256Healthy:  terminal.P256Gold,
		Palette256Damaged:  terminal.P256Amber,
		Palette256Critical: terminal.P256Red,
		BasicHealthy:       3, // Yellow
		BasicDamaged:       3, // Yellow
		BasicCritical:      1, // Red
	},
	// Type 2: Violet — ElectricViolet → SoftLavender → Vermilion
	{
		HealthyBright:      terminal.ElectricViolet,
		HealthyDark:        terminal.DarkViolet,
		DamagedBright:      terminal.SoftLavender,
		DamagedDark:        terminal.MutedPurple,
		CriticalBright:     terminal.Vermilion,
		CriticalDark:       terminal.DarkCrimson,
		GlowColor:          terminal.SoftLavender,
		Palette256Healthy:  terminal.P256MediumPurple,
		Palette256Damaged:  terminal.P256Violet,
		Palette256Critical: terminal.P256Crimson,
		BasicHealthy:       5, // Magenta
		BasicDamaged:       5, // Magenta
		BasicCritical:      1, // Red
	},
	// Type 3: Emerald — EmeraldGreen → YellowGreen → BurntSienna
	{
		HealthyBright:      terminal.EmeraldGreen,
		HealthyDark:        terminal.SeaGreen,
		DamagedBright:      terminal.YellowGreen,
		DamagedDark:        terminal.FernGreen,
		CriticalBright:     terminal.BurntSienna,
		CriticalDark:       terminal.Brick,
		GlowColor:          terminal.MintGreen,
		Palette256Healthy:  terminal.P256Green,
		Palette256Damaged:  terminal.P256YellowGreen,
		Palette256Critical: terminal.P256Crimson,
		BasicHealthy:       2, // Green
		BasicDamaged:       2, // Green
		BasicCritical:      1, // Red
	},
}

// Active target glow color (shared across all types)
var RgbTowerActiveGlow = terminal.Gold

// Health ratio thresholds for zone boundaries
const (
	TowerHealthThresholdDamaged  = 0.6
	TowerHealthThresholdCritical = 0.3
)

// Position brightness falloff (center=1.0, edge=TowerEdgeBrightnessMin)
const (
	TowerEdgeDimFactor     = 0.4
	TowerEdgeBrightnessMin = 0.6
)

// Glow parameters (normal state)
const (
	TowerGlowExtendFloat    = 1.5
	TowerGlowIntensityMin   = 0.3
	TowerGlowIntensityMax   = 0.6
	TowerGlowFalloffMult    = 2.0
	TowerGlowOuterDistSqMax = 2.25 // (1.5)²
	TowerGlowPeriodMs       = 2000
)

// Active target glow parameters (brighter, faster pulse)
const (
	TowerActiveGlowIntensityMin = 0.5
	TowerActiveGlowIntensityMax = 0.8
	TowerActiveGlowPeriodMs     = 1000
)