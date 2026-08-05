package component

// DustComponent represents orbital dust particles from glyph transformation
type DustComponent struct {
	// Target orbit radius in cells, randomized per entity
	OrbitRadius float64

	// Chase boost multiplier, decays over time (1.0 = no boost)
	ChaseBoost float64

	// Grid tracking for spatial index sync
	LastIntX int
	LastIntY int

	// Stagger group for chase response distribution (0-2)
	ResponseGroup uint8
}
