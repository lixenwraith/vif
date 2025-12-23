package component

// DecayComponent represents a decay character entity
type DecayComponent struct {
	KineticState // PreciseX/Y, VelX/Y, AccelX/Y (int32 Q16.16)

	// Visual
	Char rune

	// Logic sentinels for cell-entry detection
	LastIntX int
	LastIntY int
}