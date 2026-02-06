package parameter

const (
	// Loot entity visual dimensions (renderer-only, not composite)
	LootShieldWidth  = 5
	LootShieldHeight = 3

	// Drop rates (base probability per kill)
	LootDropRateLauncher = 0.10 // 10%
	LootDropRateRod      = 1.00 // 100% guaranteed from quasar

	// Pity: rate += baseRate per consecutive miss
	// Formula: currentRate = baseRate * (1 + missCount)

	// Homing
	LootHomingAccel    = 120.0 // cells/sec²
	LootHomingMaxSpeed = 60.0  // cells/sec

	// Collection: Chebyshev distance <= 1 from cursor
	LootCollectRadius = 1

	// Shield ellipse radii (float, converted to Q32.32 in renderer init)
	LootShieldRadiusX = 2.0
	LootShieldRadiusY = 1.0
)