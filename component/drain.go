package component

import "time"

// DrainComponent represents the drain entity that moves toward the cursor, drains energy when inside a shield and destroys itself upon collision and removing heat from cursor
// Drain count is based on Heat (floor(Heat/10)). Drains despawn when:
// - Heat drops (excess drains removed LIFO)
// - Cursor collision without active shield (-10 Heat) and colliding drain despawns
// - Drain-drain collision (all involved despawn)
type DrainComponent struct {
	KineticState            // PreciseX/Y, VelX/Y, AccelX/Y, DeflectUntil (Q32.32)
	LastDrainTime time.Time // Last time energy was drained (DrainEnergyDrainInterval)
	SpawnOrder    int64     // Monotonic counter for LIFO despawn ordering (higher = newer)
	LastIntX      int       // Cell-entry detection
	LastIntY      int       // Cell-entry detection
}