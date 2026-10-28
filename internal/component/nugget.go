package component

// NuggetComponent represents one participant's personal collectible.
// It spawns from the player stream and never participates in shared state;
// its cooldown timer spaces the beacon's materializes.
type NuggetComponent struct {
	Char rune // Character for visual display
}
