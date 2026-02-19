package visual

import (
	"math"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
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
// Derived from ratio constants to maintain single source of truth
var (
	// ShieldFeatherStart is normalized distSq where fade begins
	ShieldFeatherStart int64
	// ShieldFeatherEnd is max normalized distSq for visual rendering
	ShieldFeatherEnd int64
	// ShieldFeatherRange is (End - Start) for fade interpolation
	ShieldFeatherRange int64

	// Shield256Threshold is Q32.32 threshold for 256-color rim visibility
	Shield256Threshold int64
	// ShieldGlowEdgeThreshold is Q32.32 threshold below which glow is suppressed
	ShieldGlowEdgeThreshold int64
)

// ShieldConfig holds pre-calculated geometric and visual parameters
// Field order: geometry (hot), visual params (warm), colors (cold)
type ShieldConfig struct {
	// Geometry - accessed per-cell for containment
	InvRxSq int64 // 8 bytes
	InvRySq int64 // 8 bytes
	RadiusX int64 // 8 bytes
	RadiusY int64 // 8 bytes

	// Visual params - accessed per-cell for alpha
	MaxOpacityQ32    int64 // 8 bytes
	GlowIntensityQ32 int64 // 8 bytes

	// Iteration bounds - accessed once per entity
	VisualRadiusXInt int // 8 bytes
	VisualRadiusYInt int // 8 bytes

	// Timing - accessed once per entity
	GlowPeriod time.Duration // 8 bytes

	// Colors - accessed once per entity or per-cell for blend
	Color         terminal.RGB // 3 bytes
	ColorAlt      terminal.RGB // 3 bytes
	GlowColor     terminal.RGB // 3 bytes
	_             [5]byte      // padding to 8-byte boundary
	Palette256    uint8        // 1 byte
	Palette256Alt uint8        // 1 byte
}

// Total: 64 + 8 + 9 + 5 + 2 = 88 bytes (fits in ~1.5 cache lines)

// ShieldConfigs indexed by ShieldType
var ShieldConfigs [3]ShieldConfig

func init() {
	startRatio := vmath.FromFloat(parameter.ShieldFeatherStartRatio)
	endRatio := vmath.FromFloat(parameter.ShieldFeatherEndRatio)
	ShieldFeatherStart = vmath.Mul(startRatio, startRatio)
	ShieldFeatherEnd = vmath.Mul(endRatio, endRatio)
	ShieldFeatherRange = ShieldFeatherEnd - ShieldFeatherStart

	Shield256Threshold = vmath.FromFloat(Shield256ThresholdFloat)
	ShieldGlowEdgeThreshold = vmath.FromFloat(ShieldGlowEdgeThresholdFloat)

	// Player
	ShieldConfigs[component.ShieldTypePlayer] = buildShieldConfig(
		parameter.PlayerFieldRadiusX,
		parameter.PlayerFieldRadiusY,
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
		MaxOpacityQ32:    vmath.FromFloat(maxOpacity),
		GlowIntensityQ32: vmath.FromFloat(glowIntensity),
		GlowPeriod:       glowPeriod,
		Color:            color,
		ColorAlt:         colorAlt,
		Palette256:       palette,
		Palette256Alt:    paletteAlt,
		GlowColor:        glowColor,
	}
}