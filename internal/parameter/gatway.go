package parameter

import "time"

// Gateway spawn timing defaults
const (
	// GatewayDefaultInterval is the base spawn interval
	GatewayDefaultInterval = 3 * time.Second

	// GatewayDefaultMinInterval is the floor after rate acceleration
	GatewayDefaultMinInterval = 500 * time.Millisecond

	// GatewayDefaultRateMultiplier is the default acceleration factor (no acceleration)
	GatewayDefaultRateMultiplier = 1.0

	// GatewayDefaultRateAccelInterval is the default period between acceleration steps (0 = disabled)
	GatewayDefaultRateAccelInterval = 0
)