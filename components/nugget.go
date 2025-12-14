package components

import "time"

// NuggetComponent represents a collectible nugget entity
// Nuggets spawn randomly on the game field and respawn after being collected
type NuggetComponent struct {
	ID        int       // Unique identifier for tracking
	SpawnTime time.Time // When this nugget was spawned
}