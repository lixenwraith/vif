package visual

import (
	"time"

	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// ShieldConfig holds pre-calculated geometric and visual parameters for an entity type
type ShieldConfig struct {
	RadiusX int64
	RadiusY int64
	InvRxSq int64
	InvRySq int64

	Color      terminal.RGB
	Palette256 uint8
	MaxOpacity float64

	GlowColor     terminal.RGB
	GlowIntensity float64
	GlowPeriod    time.Duration
}

var (
	// Pre-calculated configs
	PlayerShieldConfig ShieldConfig
	QuasarShieldConfig ShieldConfig
	LootShieldConfig   ShieldConfig
)

func init() {
	initPlayerShield()
	initQuasarShield()
	initLootShield()
}

func initPlayerShield() {
	rx := vmath.FromFloat(parameter.ShieldRadiusX)
	ry := vmath.FromFloat(parameter.ShieldRadiusY)
	invRxSq, invRySq := vmath.EllipseInvRadiiSq(rx, ry)

	PlayerShieldConfig = ShieldConfig{
		RadiusX:    rx,
		RadiusY:    ry,
		InvRxSq:    invRxSq,
		InvRySq:    invRySq,
		Color:      RgbCleanerBasePositive, // Default, logic updates this based on Energy polarity
		Palette256: Shield256Positive,
		MaxOpacity: parameter.ShieldMaxOpacity,

		// Player glow specific configuration
		GlowColor:     RgbBoostGlow,
		GlowIntensity: 0.7,
		GlowPeriod:    0, // Default 0, toggled by Boost logic in system
	}
}

func initQuasarShield() {
	// Calculate dimensions including padding (Logic ported from QuasarRenderer)
	padX := parameter.QuasarShieldPadX
	padY := parameter.QuasarShieldPadY

	rx := vmath.FromFloat(float64(parameter.QuasarWidth)/2.0 + float64(padX))
	ry := vmath.FromFloat(float64(parameter.QuasarHeight)/2.0 + float64(padY))
	invRxSq, invRySq := vmath.EllipseInvRadiiSq(rx, ry)

	QuasarShieldConfig = ShieldConfig{
		RadiusX:    rx,
		RadiusY:    ry,
		InvRxSq:    invRxSq,
		InvRySq:    invRySq,
		Color:      RgbQuasarShield,
		Palette256: parameter.QuasarShield256Palette,
		MaxOpacity: parameter.QuasarShieldMaxOpacity,
		GlowPeriod: 0,
	}
}

func initLootShield() {
	rx := vmath.FromFloat(parameter.LootShieldRadiusX)
	ry := vmath.FromFloat(parameter.LootShieldRadiusY)
	invRxSq, invRySq := vmath.EllipseInvRadiiSq(rx, ry)

	LootShieldConfig = ShieldConfig{
		RadiusX:    rx,
		RadiusY:    ry,
		InvRxSq:    invRxSq,
		InvRySq:    invRySq,
		Color:      RgbLootShieldBorder,
		Palette256: Loot256Rim,
		MaxOpacity: parameter.LootShieldMaxOpacity,
		// Loot has specific glow definition
		GlowIntensity: parameter.LootGlowIntensity,
		GlowPeriod:    parameter.LootGlowRotationPeriod,
		// GlowColor is dynamic per loot type, set at spawn time by logic
	}
}