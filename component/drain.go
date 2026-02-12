package component

import "time"

// DrainComponent represents the drain entity that moves toward the cursor, drains energy when inside a shield and destroys itself upon collision and removing heat from cursor
type DrainComponent struct {
	// Last time energy was drained
	LastDrainTime time.Time

	// Monotonic counter for LIFO despawn ordering (higher = newer)
	SpawnOrder int64

	// Cell-entry detection for interaction dedup
	LastIntX int
	LastIntY int
}