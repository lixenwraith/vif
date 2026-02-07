package parameter

import (
	"time"
)

const (
	// Drop rates (base probability per kill)
	LootDropRateLauncher = 0.10 // 10%
	LootDropRateRod      = 1.00 // 100% guaranteed from quasar

	// Pity: rate += baseRate per consecutive miss
	// Formula: currentRate = baseRate * (1 + missCount)

	// Homing
	LootHomingAccelFloat    = 120.0 // cells/sec²
	LootHomingMaxSpeedFloat = 60.0  // cells/sec

	// Collection: Chebyshev distance <= 1 from cursor
	LootCollectRadius = 1

	// Shield ellipse radii (float, converted to Q32.32 in renderer init)
	LootShieldRadiusX = 2.5
	LootShieldRadiusY = 1.5

	// Shield opacity (lower than player to avoid saturation and allow glow visibility)
	LootShieldMaxOpacity = 0.5

	// Rotating glow indicator
	LootGlowRotationPeriod = 500 * time.Millisecond // 2 rotations/sec
	LootGlowEdgeThreshold  = 0.25                   // Broad rim for small ellipse
	LootGlowIntensity      = 0.7
)