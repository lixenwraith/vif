package component

// DecayComponent represents a decay character entity
type DecayComponent struct {
	// Visual
	Char rune

	// Logic sentinels for cell-entry detection
	LastIntX int
	LastIntY int
}