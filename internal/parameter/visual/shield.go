package visual

import (
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/parameter"
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
	// ShieldGeometryCacheMax is how many radii a shield renderer keeps worked out
	ShieldGeometryCacheMax = 8
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

// ShieldLook is a shield kind's light. Its extent is each shield's own radius, and a
// shield's own Color replaces Color and Palette256.
type ShieldLook struct {
	MaxOpacity    float64
	GlowIntensity float64
	GlowPeriod    time.Duration // a cursor's turns while it boosts, at ShieldBoostRotationDuration

	Color         color.RGB
	ColorAlt      color.RGB // a cursor's at negative energy
	GlowColor     color.RGB // a drop's is its kind's glow
	Palette256    uint8
	Palette256Alt uint8
}

// ShieldLooks is indexed by component.ShieldType
var ShieldLooks = [component.ShieldTypeCount]ShieldLook{
	component.ShieldTypePlayer: {
		MaxOpacity: parameter.ShieldMaxOpacity, GlowIntensity: ShieldPlayerGlowIntensity,
		Color: RgbTracerPositive, ColorAlt: RgbTracerNegative, GlowColor: RgbBoostGlow,
		Palette256: Shield256Positive, Palette256Alt: Shield256Negative,
	},
	component.ShieldTypeQuasar: {
		MaxOpacity: parameter.QuasarShieldMaxOpacity,
		Color:      RgbQuasarShield, ColorAlt: RgbQuasarShield,
		Palette256: Quasar256Rim, Palette256Alt: Quasar256Rim,
	},
	component.ShieldTypeLoot: {
		MaxOpacity: parameter.LootShieldMaxOpacity, GlowIntensity: parameter.LootGlowIntensity,
		GlowPeriod: parameter.LootGlowRotationPeriod,
		Color:      RgbLootShieldBorder, ColorAlt: RgbLootShieldBorder,
		Palette256: Loot256Rim, Palette256Alt: Loot256Rim,
	},
}

// WraithField is the field round a Wraith, at the player shield's radius: the boost's
// turning glow, pale and slower
var WraithField = ShieldLook{
	MaxOpacity: parameter.ShieldMaxOpacity, GlowIntensity: ShieldPlayerGlowIntensity,
	GlowPeriod: parameter.WraithGlowPeriod,
	Color:      RgbWraithField, ColorAlt: RgbWraithField, GlowColor: RgbWraithGlow,
	Palette256: Wraith256Rim, Palette256Alt: Wraith256Rim,
}
