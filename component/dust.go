package component


// DustComponent represents orbital dust particles from glyph transformation
// Created when gold is completed during quasar phase
type DustComponent struct {
	KineticState // PreciseX/Y, VelX/Y, AccelX/Y (Q16.16)

	// Grayscale level preserved from original glyph
	Level GlyphLevel

	// Target orbit radius, randomized per entity (Q16.16)
	OrbitRadius int32

	// Chase boost multiplier, decays over time (Q16.16, Scale = 1.0)
	ChaseBoost int32

	// Visual
	Rune rune

	// Grid tracking for spatial index sync
	LastIntX int
	LastIntY int

	// Shield containment tracking for soft redirection
	WasInsideShield bool
}