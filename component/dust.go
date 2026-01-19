package component

// DustComponent represents orbital dust particles from glyph transformation
// Created when gold is completed during quasar phase
type DustComponent struct {
	// Grayscale level preserved from original glyph
	Level GlyphLevel

	// Target orbit radius, randomized per entity (Q32.32)
	OrbitRadius int64

	// Chase boost multiplier, decays over time (Q32.32, Scale = 1.0)
	ChaseBoost int64

	// Visual
	Rune rune

	// Grid tracking for spatial index sync
	LastIntX int
	LastIntY int

	// Shield containment tracking for soft redirection
	WasInsideShield bool
}