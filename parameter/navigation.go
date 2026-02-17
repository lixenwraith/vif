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

	// NavFlowROIMargin is expansion margin around computed AABB (cells)
	NavFlowROIMargin = 10
)

// Navigation defaults (Q32.32)
var (
	NavTurnThresholdDefault  = vmath.FromFloat(GADrainTurnThresholdDefault)
	NavBrakeIntensityDefault = vmath.FromFloat(GADrainBrakeIntensityDefault)
	NavFlowLookaheadDefault  = vmath.FromFloat(GADrainFlowLookaheadDefault)
)