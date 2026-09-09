package parameter

// Navigation - Flow Field
const (
	// NavFlowMinTicksBetweenCompute is minimum game ticks between flow field recomputation
	NavFlowMinTicksBetweenCompute = 3

	// NavFlowDirtyDistance triggers immediate recompute if target moves this far (cells)
	NavFlowDirtyDistance = 5

	// NavRouteRebuildInterval is minimum game ticks between gateway route graph recomputes
	NavRouteRebuildInterval = 20

	// NavCorneringBrake is the drag multiplier per unit of turn severity
	NavCorneringBrake = 0.8
	// NavCorneringThreshold is the alignment below which cornering drag activates
	NavCorneringThreshold = 3.0
	// NavFlowLookaheadDefault is flow-field target lookahead (cells)
	NavFlowLookaheadDefault = 12.0
)
