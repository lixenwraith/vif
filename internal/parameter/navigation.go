package parameter

// Navigation - Flow Field
const (
	// NavFlowMinTicksBetweenCompute is the cooldown, in game ticks, between flow field recomputes
	NavFlowMinTicksBetweenCompute = 5

	// NavFlowDirtyDistance is the target drift, in cells, that recomputes once the cooldown passes
	NavFlowDirtyDistance = 5

	// NavFlowSettleTicks is how long a smaller drift waits: a field a few cells off
	// still points the same way everywhere but beside the target, where sight takes over
	NavFlowSettleTicks = 20

	// NavRouteRebuildInterval is minimum game ticks between gateway route graph recomputes
	NavRouteRebuildInterval = 20

	// NavCorneringBrake is the drag multiplier per unit of turn severity
	NavCorneringBrake = 0.8
	// NavCorneringBase is what TurnSeverity reads a heading against: severity is
	// this minus the cosine of the turn, so a steering actor always drags, at
	// twice NavCorneringBrake heading straight at its target and four times
	// heading away. Below 1 it would gate the brake instead.
	NavCorneringBase = 3.0
	// NavFlowLookaheadDefault is flow-field target lookahead (cells)
	NavFlowLookaheadDefault = 12.0
)
