package component

// DecayComponent represents a decay character entity
type DecayComponent struct {
	Kinetic // PreciseX/Y, VelX/Y, AccelX/Y (int64 Q32.32)

	// Visual
	Char rune

	// Logic sentinels for cell-entry detection
	LastIntX int
	LastIntY int
}