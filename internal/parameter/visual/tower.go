package visual

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/component"
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

	// 256-color palette per health zone; damaged is drawn as ▒ in its color over the healthy one
	Palette256Healthy  uint8
	Palette256Damaged  uint8
	Palette256Critical uint8
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
		Palette256Healthy:  ConCyan,
		Palette256Damaged:  ConYellow,
		Palette256Critical: ConRed,
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
		Palette256Healthy:  ConYellow,
		Palette256Damaged:  ConRed,
		Palette256Critical: ConRed,
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
		Palette256Healthy:  ConMagenta,
		Palette256Damaged:  ConWhite,
		Palette256Critical: ConRed,
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
		Palette256Healthy:  ConGreen,
		Palette256Damaged:  ConYellow,
		Palette256Critical: ConRed,
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
	TowerGlowExtend         = 1.5
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
