package component

// DecayComponent represents a decay character entity
type DecayComponent struct {
	// Visual
	Rune rune

	// Logic sentinels for cell-entry detection
	LastIntX int
	LastIntY int
}