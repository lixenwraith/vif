package components

import "time"

// DrainComponent represents the drain entity that moves toward the cursor
// and drains energy when positioned on top of it
// Drain count is based on Heat (floor(Heat/10)). Drains despawn when:
// - Heat drops (excess drains removed LIFO)
// - Cursor collision without active shield (-10 Heat) and colliding drain despawns
// - Drain-drain collision (all involved despawn)
type DrainComponent struct {
	LastMoveTime  time.Time // Last time the drain moved (DrainMoveInterval)
	LastDrainTime time.Time // Last time energy was drained (DrainEnergyDrainInterval)
	IsOnCursor    bool      // Cached state for efficient drain checks
	SpawnOrder    int64     // Monotonic counter for LIFO despawn ordering (higher = newer)
}