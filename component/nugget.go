package component

import "time"

// NuggetComponent represents a collectible nugget entity
// Nuggets spawn randomly on the game field and respawn after being collected
type NuggetComponent struct {
	Char      rune      // Character for visual display
	SpawnTime time.Time // When this nugget was spawned
}