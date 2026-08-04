package parameter

import (
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// Navigation - Flow Field
const (
	// NavFlowMinTicksBetweenCompute is minimum game ticks between flow field recomputation
	NavFlowMinTicksBetweenCompute = 3

	// NavFlowDirtyDistance triggers immediate recompute if target moves this far (cells)
	NavFlowDirtyDistance = 5

	// NavRouteRebuildInterval is minimum game ticks between gateway route graph recomputes
	NavRouteRebuildInterval = 20

	// NavCorneringBrakeFloat is the drag multiplier per unit of turn severity
	NavCorneringBrakeFloat = 0.8
	// NavCorneringThresholdFloat is the alignment below which cornering drag activates
	NavCorneringThresholdFloat = 3.0
	// NavFlowLookaheadDefaultFloat is flow-field target lookahead (cells)
	NavFlowLookaheadDefaultFloat = 12.0
)

var (
	NavCorneringBrake       = vmath.FromFloat(NavCorneringBrakeFloat)
	NavCorneringThreshold   = vmath.FromFloat(NavCorneringThresholdFloat)
	NavFlowLookaheadDefault = vmath.FromFloat(NavFlowLookaheadDefaultFloat)
)

// Route Graph — Computation
const (
	// RouteGraphMinWeightFloor ensures every route gets minimum traffic share
	RouteGraphMinWeightFloor = 0.05

	// RouteGraphMaxRoutes caps accepted routes per graph
	RouteGraphMaxRoutes = 8

	// RouteGraphExtraAttempts is the rejected-candidate budget above the route cap
	RouteGraphExtraAttempts = 8

	// RouteTolerancePct caps route length as a percentage above the optimum
	RouteTolerancePct = 50

	// RouteGraphMaxOverlapPct: reject route candidate sharing more than this
	// percentage of its cells with already-accepted routes (dilated)
	RouteGraphMaxOverlapPct = 70

	// RouteCorridorRadius: BFS dilation (cells) around a route path; sets
	// penalty spread and per-route flow-field corridor width. Raise if
	// knockback deaths outside corridors produce excessive zero-fitness noise
	RouteCorridorRadius = 2

	// RouteGraphWaypointStride: path decimation interval for Route.Waypoints
	RouteGraphWaypointStride = 8
)
