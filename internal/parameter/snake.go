package parameter

import (
	"time"

	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// Snake head dimensions (5×3, appears square in terminal)
const (
	SnakeHeadWidth         = 5
	SnakeHeadHeight        = 3
	SnakeHeadHeaderOffsetX = 2 // Center of 5-wide
	SnakeHeadHeaderOffsetY = 1 // Center of 3-tall
)

// Snake body dimensions
const (
	SnakeBodyWidth = 3 // 3 cells wide perpendicular to direction
)

// Snake trail and segment configuration
const (
	SnakeMaxSegments = 20

	// SnakeSegmentSpacing: distance between segment centers in cells
	// Configurable for testing; lower = tighter body
	SnakeSegmentSpacing = 1.0

	// SnakeTrailSampleInterval: minimum distance head must move before trail sample
	SnakeTrailSampleInterval = 0.5
)

// Snake spawn parameters
const (
	SnakeDefaultSegmentCount = 8
	SnakeSpawnIntervalTicks  = 3 // Ticks between segment spawns during initial spawn
)

// Snake physics (floats for parameter definition)
const (
	SnakeBaseSpeed   = 12.0
	SnakeMaxSpeed    = 25.0
	SnakeHomingAccel = 8.0
	SnakeDrag        = 0.94
	SnakeRestitution = 0.3

	// Spring constants for body member kinetic behavior
	SnakeSpringStiffness = 18.0
	SnakeSpringDamping   = 0.82
	SnakeSpringMaxForce  = 40.0 // Clamp to prevent explosion
)

// Snake combat
const (
	CombatInitialHPSnakeHead      = 50
	CombatInitialHPSnakeMemberMax = 50 // Head-adjacent segment (10x base)
	CombatInitialHPSnakeMemberMin = 5  // Tail segment (base)

	SnakeHeadCollisionRadiusX = 2.5  // Half of 5
	SnakeHeadCollisionRadiusY = 1.25 // Half of 3, aspect adjusted
)

// Snake damage values
const (
	SnakeDamageHeat         = 10
	SnakeShieldDrainPerTick = 500
)

// Snake timers
const (
	SnakeGrowthCooldown = 500 * time.Millisecond // Min time between growth events
)

// Pre-computed values
var (
	SnakeHeadCollisionInvRxSq,
	SnakeHeadCollisionInvRySq = vmath.EllipseInvRadiiSqF(SnakeHeadCollisionRadiusX, SnakeHeadCollisionRadiusY)

	// Squared for distance comparison without sqrt
	SnakeTrailSampleIntervalSq = SnakeTrailSampleInterval * SnakeTrailSampleInterval
)
