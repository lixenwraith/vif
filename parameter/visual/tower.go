package visual

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vi-fighter/component"
)

// TowerTypeColors holds per-type health gradient, glow, and palette colors
type TowerTypeColors struct {
	// TrueColor health zones (bright=center, dark=edge)
	HealthyBright  color.RGB
	HealthyDark    color.RGB
	DamagedBright  color.RGB
	DamagedDark    color.RGB
	CriticalBright color.RGB
	CriticalDark   color.RGB

	// TrueColor glow
	GlowColor color.RGB

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
		HealthyBright:      color.BrightCyan,
		HealthyDark:        color.Teal,
		DamagedBright:      color.PaleGold,
		DamagedDark:        color.Amber,
		CriticalBright:     color.Coral,
		CriticalDark:       color.Brick,
		GlowColor:          color.SkyTeal,
		Palette256Healthy:  color.P256Cyan,
		Palette256Damaged:  color.P256Gold,
		Palette256Critical: color.P256Crimson,
		BasicHealthy:       6, // Cyan
		BasicDamaged:       3, // Yellow
		BasicCritical:      1, // Red
	},
	// Type 1: Gold — Gold → WarmOrange → BrightRed
	{
		HealthyBright:      color.Gold,
		HealthyDark:        color.DarkGold,
		DamagedBright:      color.WarmOrange,
		DamagedDark:        color.BurntOrange,
		CriticalBright:     color.BrightRed,
		CriticalDark:       color.Brick,
		GlowColor:          color.Apricot,
		Palette256Healthy:  color.P256Gold,
		Palette256Damaged:  color.P256Amber,
		Palette256Critical: color.P256Red,
		BasicHealthy:       3, // Yellow
		BasicDamaged:       3, // Yellow
		BasicCritical:      1, // Red
	},
	// Type 2: Violet — ElectricViolet → SoftLavender → Vermilion
	{
		HealthyBright:      color.ElectricViolet,
		HealthyDark:        color.DarkViolet,
		DamagedBright:      color.SoftLavender,
		DamagedDark:        color.MutedPurple,
		CriticalBright:     color.Vermilion,
		CriticalDark:       color.DarkCrimson,
		GlowColor:          color.SoftLavender,
		Palette256Healthy:  color.P256MediumPurple,
		Palette256Damaged:  color.P256Violet,
		Palette256Critical: color.P256Crimson,
		BasicHealthy:       5, // Magenta
		BasicDamaged:       5, // Magenta
		BasicCritical:      1, // Red
	},
	// Type 3: Emerald — EmeraldGreen → YellowGreen → BurntSienna
	{
		HealthyBright:      color.EmeraldGreen,
		HealthyDark:        color.SeaGreen,
		DamagedBright:      color.YellowGreen,
		DamagedDark:        color.FernGreen,
		CriticalBright:     color.BurntSienna,
		CriticalDark:       color.Brick,
		GlowColor:          color.MintGreen,
		Palette256Healthy:  color.P256Green,
		Palette256Damaged:  color.P256YellowGreen,
		Palette256Critical: color.P256Crimson,
		BasicHealthy:       2, // Green
		BasicDamaged:       2, // Green
		BasicCritical:      1, // Red
	},
}

// Active target glow color (shared across all types)
var RgbTowerActiveGlow = color.Gold

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
