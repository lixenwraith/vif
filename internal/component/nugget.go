package component

import "time"

// NuggetComponent represents one participant's personal collectible.
// It spawns from the player stream and never participates in shared state.
type NuggetComponent struct {
	Char            rune          // Character for visual display
	SpawnTime       time.Time     // When this nugget was spawned
	BeaconRemaining time.Duration // Time until next directional cleaner emission
}
