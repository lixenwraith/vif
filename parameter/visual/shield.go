package visual

import (
	"math"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// Shield visual feather zone (renderer-only, does NOT affect game logic)
// Derived from ratio constants to maintain single source of truth
var (
	// ShieldFeatherStart is normalized distSq where fade begins
	ShieldFeatherStart int64

	// ShieldFeatherEnd is max normalized distSq for visual rendering
	ShieldFeatherEnd int64
)

// ShieldConfig holds pre-calculated geometric and visual parameters for an entity type
type ShieldConfig struct {
	// Geometry (copied to component for game mechanics)
	RadiusX, RadiusY int64
	InvRxSq, InvRySq int64

	// Visual iteration bounds (includes feather zone)
	VisualRadiusXInt int
	VisualRadiusYInt int

	// Visual parameters
	MaxOpacity    float64
	GlowIntensity float64
	GlowPeriod    time.Duration

	// Colors (Player uses Color/ColorAlt based on energy polarity)
	Color         terminal.RGB
	ColorAlt      terminal.RGB
	Palette256    uint8
	Palette256Alt uint8
	GlowColor     terminal.RGB
}

// ShieldConfigs indexed by ShieldType
var ShieldConfigs [3]ShieldConfig

func init() {
	startRatio := vmath.FromFloat(parameter.ShieldFeatherStartRatio)
	endRatio := vmath.FromFloat(parameter.ShieldFeatherEndRatio)
	ShieldFeatherStart = vmath.Mul(startRatio, startRatio)
	ShieldFeatherEnd = vmath.Mul(endRatio, endRatio)

	// Player
	ShieldConfigs[component.ShieldTypePlayer] = buildShieldConfig(
		parameter.PlayerFieldRadiusX,
		parameter.PlayerFieldRadiusY,
		parameter.ShieldMaxOpacity,
		RgbCleanerBasePositive, RgbCleanerBaseNegative,
		Shield256Positive, Shield256Negative,
		RgbBoostGlow,
		0.7,
		0,
	)

	// Quasar
	qrx := float64(parameter.QuasarWidth)/2.0 + float64(parameter.QuasarShieldPadX)
	qry := float64(parameter.QuasarHeight)/2.0 + float64(parameter.QuasarShieldPadY)
	ShieldConfigs[component.ShieldTypeQuasar] = buildShieldConfig(
		qrx, qry,
		parameter.QuasarShieldMaxOpacity,
		RgbQuasarShield, RgbQuasarShield,
		parameter.QuasarShield256Palette, parameter.QuasarShield256Palette,
		terminal.RGB{},
		0,
		0,
	)

	// Loot
	ShieldConfigs[component.ShieldTypeLoot] = buildShieldConfig(
		parameter.LootShieldRadiusX,
		parameter.LootShieldRadiusY,
		parameter.LootShieldMaxOpacity,
		RgbLootShieldBorder, RgbLootShieldBorder,
		Loot256Rim, Loot256Rim,
		terminal.RGB{},
		parameter.LootGlowIntensity,
		parameter.LootGlowRotationPeriod,
	)
}

func buildShieldConfig(rxF, ryF, maxOpacity float64, color, colorAlt terminal.RGB, palette, paletteAlt uint8, glowColor terminal.RGB, glowIntensity float64, glowPeriod time.Duration) ShieldConfig {
	rx := vmath.FromFloat(rxF)
	ry := vmath.FromFloat(ryF)
	invRxSq, invRySq := vmath.EllipseInvRadiiSq(rx, ry)

	visualRxInt := int(math.Ceil(rxF*parameter.ShieldFeatherEndRatio)) + 1
	visualRyInt := int(math.Ceil(ryF*parameter.ShieldFeatherEndRatio)) + 1

	return ShieldConfig{
		RadiusX:          rx,
		RadiusY:          ry,
		InvRxSq:          invRxSq,
		InvRySq:          invRySq,
		VisualRadiusXInt: visualRxInt,
		VisualRadiusYInt: visualRyInt,
		MaxOpacity:       maxOpacity,
		GlowIntensity:    glowIntensity,
		GlowPeriod:       glowPeriod,
		Color:            color,
		ColorAlt:         colorAlt,
		Palette256:       palette,
		Palette256Alt:    paletteAlt,
		GlowColor:        glowColor,
	}
}