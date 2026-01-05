package component

// BlossomComponent represents a blossom character entity
type BlossomComponent struct {
	KineticState // PreciseX/Y, VelX/Y, AccelX/Y (int64 Q32.32)

	// Visual
	Char rune

	// Logic sentinels for cell-entry detection
	LastIntX int
	LastIntY int
}