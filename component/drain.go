package component


import "time"

// DrainComponent represents the drain entity that moves toward the cursor
// and drains energy when positioned on top of it
// Drain count is based on Heat (floor(Heat/10)). Drains despawn when:
// - Heat drops (excess drains removed LIFO)
// - Cursor collision without active shield (-10 Heat) and colliding drain despawns
// - Drain-drain collision (all involved despawn)
type DrainComponent struct {
	KineticState            // PreciseX/Y, VelX/Y, AccelX/Y (Q16.16)
	LastDrainTime time.Time // Last time energy was drained (DrainEnergyDrainInterval)
	// TODO: legacy, before cursor store, check to remove
	IsOnCursor   bool      // Cached state for efficient drain checks
	SpawnOrder   int64     // Monotonic counter for LIFO despawn ordering (higher = newer)
	LastIntX     int       // Cell-entry detection
	LastIntY     int       // Cell-entry detection
	DeflectUntil time.Time // Immunity from homing/drag until this time
}