package visual

import (
	"math"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/vmath"
)

const (
	ShieldPlayerGlowIntensity = 0.7
	// PeerFieldBlend keeps remote shield and ember fields subordinate to the local cursor.
	PeerFieldBlend = 0.3
	// Shield256RimStart is the normalized distSq a 256-color rim starts at when the shield is large enough
	Shield256RimStart = 0.64
	// Shield256GlowDot is the least alignment with the glow direction that lights a 256-color rim cell
	Shield256GlowDot = 0.8
	// ShieldGlowEdgeThreshold is normalized distSq below which glow is suppressed
	ShieldGlowEdgeThreshold = 0.36
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

	// 256-color rim band in normalized distSq
	Rim256Min float64
	Rim256Max float64

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
		Quasar256Rim, Quasar256Rim,
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

func buildShieldConfig(rx, ry, maxOpacity float64, colorMain, colorAlt color.RGB, palette, paletteAlt uint8, glowColor color.RGB, glowIntensity float64, glowPeriod time.Duration) ShieldConfig {
	invRxSq, invRySq := vmath.EllipseInvRadiiSqF(rx, ry)

	visualRxInt := int(math.Ceil(rx*parameter.ShieldFeatherEndRatio)) + 1
	visualRyInt := int(math.Ceil(ry*parameter.ShieldFeatherEndRatio)) + 1
	rimMin, rimMax := rim256Band(invRxSq, invRySq, visualRxInt, visualRyInt)

	return ShieldConfig{
		RadiusX:          rx,
		RadiusY:          ry,
		InvRxSq:          invRxSq,
		InvRySq:          invRySq,
		VisualRadiusXInt: visualRxInt,
		VisualRadiusYInt: visualRyInt,
		MaxOpacity:       maxOpacity,
		GlowIntensity:    glowIntensity,
		Rim256Min:        rimMin,
		Rim256Max:        rimMax,
		GlowPeriod:       glowPeriod,
		Color:            colorMain,
		ColorAlt:         colorAlt,
		Palette256:       palette,
		Palette256Alt:    paletteAlt,
		GlowColor:        glowColor,
	}
}

// rim256Band fits the 256-color rim to the cell grid: it spans from the outermost drawn cell on
// each axis inward, starting before Shield256RimStart when a shield is too small for that band
// to hold a whole row or column, so every shield closes into a ring
func rim256Band(invRxSq, invRySq float64, reachX, reachY int) (float64, float64) {
	var edgeX, edgeY float64
	for n := reachX; n > 0 && edgeX == 0; n-- {
		if d := vmath.EllipseDistSqF(float64(n), 0, invRxSq, invRySq); d <= ShieldFeatherEnd {
			edgeX = d
		}
	}
	for n := reachY; n > 0 && edgeY == 0; n-- {
		if d := vmath.EllipseDistSqF(0, float64(n), invRxSq, invRySq); d <= ShieldFeatherEnd {
			edgeY = d
		}
	}
	return min(Shield256RimStart, edgeX, edgeY), max(edgeX, edgeY)
}
