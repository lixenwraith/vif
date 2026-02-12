package component

// NavigationComponent provides pathfinding state for kinetic entities
type NavigationComponent struct {
	// True when straight-line path to cursor is walkable
	HasDirectPath bool

	// Flow direction from BFS (Q32.32 normalized), valid when HasDirectPath is false
	FlowX int64
	FlowY int64

	// GA-optimized cornering parameters (Q32.32)

	// TurnThreshold: alignment below which cornering drag activates (0.5–0.95)
	TurnThreshold int64
	// BrakeIntensity: drag multiplier during turns (1.0–6.0)
	BrakeIntensity int64
	// FlowLookahead is flow field projection distance (Q32.32 cells)
	FlowLookahead int64
}