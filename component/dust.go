package component

// DustComponent represents orbital dust particles from glyph transformation
type DustComponent struct {
	// Target orbit radius, randomized per entity (Q32.32)
	OrbitRadius int64

	// Chase boost multiplier, decays over time (Q32.32, Scale = 1.0)
	ChaseBoost int64

	// Grid tracking for spatial index sync
	LastIntX int
	LastIntY int

	// Shield containment tracking for soft redirection
	WasInsideShield bool

	// Stagger group for chase response distribution (0-2)
	ResponseGroup uint8
}