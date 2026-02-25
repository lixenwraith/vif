package parameter

import (
	"github.com/lixenwraith/vi-fighter/vmath"
)

// Navigation - Flow Field
const (
	// NavFlowMinTicksBetweenCompute is minimum game ticks between flow field recomputation
	NavFlowMinTicksBetweenCompute = 3

	// NavFlowDirtyDistance triggers immediate recompute if target moves this far (cells)
	NavFlowDirtyDistance = 5
)

var (
	// Alignment below which cornering drag activates
	NavCorneringBrake = vmath.FromFloat(0.8)
	// Drag multiplier during turns
	NavCorneringThreshold = vmath.FromFloat(3.0)
	// LookAhead cells
	NavFlowLookaheadDefault = vmath.FromFloat(12.0)
)

// Route Graph — Computation
const (
	// RouteGraphMaxBranchFanout caps neighbor expansion at a single branch point
	RouteGraphMaxBranchFanout = 8

	// RouteGraphMinWeightFloor ensures every route gets minimum traffic share
	RouteGraphMinWeightFloor = 0.05

	// RouteGraphMaxRoutes caps total routes per graph to bound DFS enumeration
	RouteGraphMaxRoutes = 64
)