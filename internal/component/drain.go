package component

// DrainComponent represents the drain entity that moves toward the cursor, drains energy when inside a shield and destroys itself upon collision and removing heat from cursor;
// its cooldown timer spaces the energy drains
type DrainComponent struct {
	// Monotonic counter for LIFO despawn ordering (higher = newer)
	SpawnOrder int

	// Cell-entry detection for interaction dedup
	LastIntX int
	LastIntY int
}
