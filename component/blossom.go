package component

// BlossomComponent represents a blossom character entity
type BlossomComponent struct {
	// Visual
	Char rune

	// Logic sentinels for cell-entry detection
	LastIntX int
	LastIntY int
}