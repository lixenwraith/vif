package components

import "time"

// DrainComponent represents the drain entity that moves toward the cursor
// and drains energy when positioned on top of it.
// The drain entity spawns when energy > 0 and despawns when energy <= 0.
type DrainComponent struct {
	// TODO: Redundant with PositionComponent
	// X, Y          int       // Current position in game coordinates
	LastMoveTime  time.Time // Last time the drain moved (DrainMoveInterval)
	LastDrainTime time.Time // Last time energy was drained (DrainEnergyDrainInterval)
	IsOnCursor    bool      // Cached state for efficient drain checks
}