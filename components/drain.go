package components

import "time"

// DrainComponent represents the drain entity that moves toward the cursor
// and drains score when positioned on top of it.
// The drain entity spawns when score > 0 and despawns when score <= 0.
type DrainComponent struct {
	X, Y          int       // Current position in game coordinates
	LastMoveTime  time.Time // Last time the drain moved (250ms interval)
	LastDrainTime time.Time // Last time score was drained (250ms interval)
	IsOnCursor    bool      // Cached state for efficient drain checks
}
