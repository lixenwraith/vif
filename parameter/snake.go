package parameter

import (
	"time"

	"github.com/lixenwraith/vi-fighter/vmath"
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
	SnakeMaxSegments    = 20
	SnakeTrailLookahead = 8

	// SnakeSegmentSpacingFloat: distance between segment centers in cells
	// Configurable for testing; lower = tighter body
	SnakeSegmentSpacingFloat = 1.5

	// SnakeTrailSampleInterval: minimum distance head must move before trail sample
	SnakeTrailSampleIntervalFloat = 0.5
)

// Snake spawn parameters
const (
	SnakeDefaultSegmentCount = 8
	SnakeSpawnIntervalTicks  = 3 // Ticks between segment spawns during initial spawn
)

// Snake physics (floats for parameter definition)
const (
	SnakeBaseSpeedFloat   = 12.0
	SnakeMaxSpeedFloat    = 25.0
	SnakeHomingAccelFloat = 8.0
	SnakeDragFloat        = 0.94
	SnakeRestitutionFloat = 0.3

	// Spring constants for body member kinetic behavior
	SnakeSpringStiffnessFloat = 18.0
	SnakeSpringDampingFloat   = 0.82
	SnakeSpringMaxForceFloat  = 40.0 // Clamp to prevent explosion
)

// Snake combat
const (
	CombatInitialHPSnakeHead   = 50
	CombatInitialHPSnakeMember = 5 // Per body cell

	SnakeHeadCollisionRadiusXFloat = 2.5  // Half of 5
	SnakeHeadCollisionRadiusYFloat = 1.25 // Half of 3, aspect adjusted
)

// Snake damage values
const (
	SnakeDamageHeat         = 15 // Heat removed on head collision without shield
	SnakeShieldDrainPerTick = 50 // Energy drained when inside player shield
)

// Snake timers
const (
	SnakeGrowthCooldown = 500 * time.Millisecond // Min time between growth events
)

// Pre-computed Q32.32 values
var (
	SnakeBaseSpeed   = vmath.FromFloat(SnakeBaseSpeedFloat)
	SnakeMaxSpeed    = vmath.FromFloat(SnakeMaxSpeedFloat)
	SnakeHomingAccel = vmath.FromFloat(SnakeHomingAccelFloat)
	SnakeDrag        = vmath.FromFloat(SnakeDragFloat)
	SnakeRestitution = vmath.FromFloat(SnakeRestitutionFloat)

	SnakeSpringStiffness = vmath.FromFloat(SnakeSpringStiffnessFloat)
	SnakeSpringDamping   = vmath.FromFloat(SnakeSpringDampingFloat)
	SnakeSpringMaxForce  = vmath.FromFloat(SnakeSpringMaxForceFloat)

	SnakeSegmentSpacing      = vmath.FromFloat(SnakeSegmentSpacingFloat)
	SnakeTrailSampleInterval = vmath.FromFloat(SnakeTrailSampleIntervalFloat)

	SnakeHeadCollisionRadiusX = vmath.FromFloat(SnakeHeadCollisionRadiusXFloat)
	SnakeHeadCollisionRadiusY = vmath.FromFloat(SnakeHeadCollisionRadiusYFloat)
	SnakeHeadCollisionInvRxSq,
	SnakeHeadCollisionInvRySq = vmath.EllipseInvRadiiSq(SnakeHeadCollisionRadiusX, SnakeHeadCollisionRadiusY)
)