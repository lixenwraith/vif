package visual

import (
	"math"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// Shield threshold constants
const (
	// ShieldPlayerGlowIntensity
	ShieldPlayerGlowIntensity = 0.7
	// Shield256ThresholdFloat is normalized distSq below which 256-color rim is transparent
	Shield256ThresholdFloat = 0.64
	// ShieldGlowEdgeThresholdFloat is normalized distSq below which glow is suppressed
	ShieldGlowEdgeThresholdFloat = 0.36
)

// Shield visual feather zone (renderer-only, does NOT affect game logic)
// Normalized distSq thresholds derived from the ratio constants
const (
	// ShieldFeatherStart is normalized distSq where fade begins
	ShieldFeatherStart = parameter.ShieldFeatherStartRatio * parameter.ShieldFeatherStartRatio
	// ShieldFeatherEnd is max normalized distSq for visual rendering
	ShieldFeatherEnd = parameter.ShieldFeatherEndRatio * parameter.ShieldFeatherEndRatio
	// ShieldFeatherRange is (End - Start) for fade interpolation
	ShieldFeatherRange = ShieldFeatherEnd - ShieldFeatherStart
)

// ShieldConfig holds pre-calculated geometric and visual parameters
// Field order: geometry (hot), visual params (warm), colors (cold)
type ShieldConfig struct {
	// Geometry - accessed per-cell for containment
	InvRxSq float64
	InvRySq float64
	RadiusX float64
	RadiusY float64

	// Visual params - accessed per-cell for alpha
	MaxOpacity    float64
	GlowIntensity float64

	// Iteration bounds - accessed once per entity
	VisualRadiusXInt int
	VisualRadiusYInt int

	// Timing - accessed once per entity
	GlowPeriod time.Duration

	// Colors - accessed once per entity or per-cell for blend
	Color         color.RGB
	ColorAlt      color.RGB
	GlowColor     color.RGB
	Palette256    uint8
	Palette256Alt uint8
}

// ShieldConfigs indexed by ShieldType
var ShieldConfigs [3]ShieldConfig

func init() {
	// Player
	ShieldConfigs[component.ShieldTypePlayer] = buildShieldConfig(
		parameter.PlayerShieldRadiusX,
		parameter.PlayerShieldRadiusY,
		parameter.ShieldMaxOpacity,
		RgbCleanerBasePositive, RgbCleanerBaseNegative,
		Shield256Positive, Shield256Negative,
		RgbBoostGlow,
		ShieldPlayerGlowIntensity,
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
		color.RGB{},
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
		color.RGB{},
		parameter.LootGlowIntensity,
		parameter.LootGlowRotationPeriod,
	)
}

func buildShieldConfig(rxF, ryF, maxOpacity float64, colorMain, colorAlt color.RGB, palette, paletteAlt uint8, glowColor color.RGB, glowIntensity float64, glowPeriod time.Duration) ShieldConfig {
	invRxSq, invRySq := vmath.EllipseInvRadiiSqF(rxF, ryF)

	visualRxInt := int(math.Ceil(rxF*parameter.ShieldFeatherEndRatio)) + 1
	visualRyInt := int(math.Ceil(ryF*parameter.ShieldFeatherEndRatio)) + 1

	return ShieldConfig{
		RadiusX:          rxF,
		RadiusY:          ryF,
		InvRxSq:          invRxSq,
		InvRySq:          invRySq,
		VisualRadiusXInt: visualRxInt,
		VisualRadiusYInt: visualRyInt,
		MaxOpacity:       maxOpacity,
		GlowIntensity:    glowIntensity,
		GlowPeriod:       glowPeriod,
		Color:            colorMain,
		ColorAlt:         colorAlt,
		Palette256:       palette,
		Palette256Alt:    paletteAlt,
		GlowColor:        glowColor,
	}
}
