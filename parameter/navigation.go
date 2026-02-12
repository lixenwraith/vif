package parameter

// Navigation - Flow Field
const (
	// NavFlowMinTicksBetweenCompute is minimum ticks between flow field recomputation
	// At 20 ticks/sec game logic, 3 ticks = 150ms max staleness
	NavFlowMinTicksBetweenCompute = 3

	// NavFlowDirtyDistance triggers immediate recompute if target moves this far (cells)
	NavFlowDirtyDistance = 5
)