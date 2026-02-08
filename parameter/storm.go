package parameter

import (
	"github.com/lixenwraith/vi-fighter/vmath"
)

// Storm spawn trigger
const SwarmKillsForStorm = 10

// Storm circle dimensions (ellipse, terminal 2:1 aspect)
const (
	StormCircleRadiusXFloat = 10.0
	StormCircleRadiusYFloat = 5.0
)

// Storm combat (same as quasar baseline)
const (
	StormCircleHealth               = 1000.0
	StormCircleMassFloat            = 150.0 // Same as quasar
	StormCircleRadiusCollisionFloat = 7.5   // Geometric mean for collision
)

// Storm 3D physics
const (
	StormGravityFloat      = 500.0
	StormDampingFloat      = 0.998 // Per-tick velocity decay
	StormMaxVelocityFloat  = 50.0
	StormRestitutionFloat  = 0.8
	StormZMinFloat         = 3.0  // Near plane
	StormZMaxFloat         = 30.0 // Far plane
	StormZSpawnOffsetFloat = 8.0  // Initial Z spread
)

// Pre-computed Q32.32 values
var (
	StormCircleRadiusX = vmath.FromFloat(StormCircleRadiusXFloat)
	StormCircleRadiusY = vmath.FromFloat(StormCircleRadiusYFloat)
	StormCircleMass    = vmath.FromFloat(StormCircleMassFloat)
	StormGravity       = vmath.FromFloat(StormGravityFloat)
	StormDamping       = vmath.FromFloat(StormDampingFloat)
	StormMaxVelocity   = vmath.FromFloat(StormMaxVelocityFloat)
	StormRestitution   = vmath.FromFloat(StormRestitutionFloat)
	StormZMin          = vmath.FromFloat(StormZMinFloat)
	StormZMax          = vmath.FromFloat(StormZMaxFloat)
	StormZSpawnOffset  = vmath.FromFloat(StormZSpawnOffsetFloat)

	StormCollisionRadius = vmath.FromFloat(StormCircleRadiusCollisionFloat)
	StormCollisionInvRxSq,
	StormCollisionInvRySq = vmath.EllipseInvRadiiSq(StormCircleRadiusX, StormCircleRadiusY)
)