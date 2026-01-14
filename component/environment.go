package component

import (
	"time"
)

type EnvironmentComponent struct {
	// Grayout visual effect state
	GrayoutActive    bool
	GrayoutIntensity float64   // TODO: logic and Q32.32
	GrayoutStartTime time.Time // UnixNano

	WindActive bool
	// Global wind velocity in Q32.32
	WindVelX int64
	WindVelY int64
}