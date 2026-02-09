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

// Storm combat
const (
	StormCircleRadiusCollisionFloat = 7.5
)

// Storm spawn geometry
const (
	StormInitialRadiusFloat = 20.0
	StormInitialSpeedFloat  = 8.0
)

// Storm 3D physics (tuned for three-body chaos)
const (
	StormGravityFloat           = 80.0
	StormRepulsionRadiusFloat   = 18.0
	StormRepulsionStrengthFloat = 250.0
	StormDampingFloat           = 0.92
	StormMaxVelocityFloat       = 45.0
	StormRestitutionFloat       = 1.0
	StormZMinFloat              = 3.0
	StormZMaxFloat              = 30.0
	StormZSpawnOffsetFloat      = 10.0
)

// Storm boundary insets (account for visual radius)
const (
	StormBoundaryInsetXFloat = 11.0
	StormBoundaryInsetYFloat = 6.0
)

// Pre-computed Q32.32 values
var (
	StormCircleRadiusX = vmath.FromFloat(StormCircleRadiusXFloat)
	StormCircleRadiusY = vmath.FromFloat(StormCircleRadiusYFloat)

	StormGravity           = vmath.FromFloat(StormGravityFloat)
	StormRepulsionRadius   = vmath.FromFloat(StormRepulsionRadiusFloat)
	StormRepulsionStrength = vmath.FromFloat(StormRepulsionStrengthFloat)
	StormDamping           = vmath.FromFloat(StormDampingFloat)
	StormMaxVelocity       = vmath.FromFloat(StormMaxVelocityFloat)
	StormRestitution       = vmath.FromFloat(StormRestitutionFloat)
	StormZMin              = vmath.FromFloat(StormZMinFloat)
	StormZMax              = vmath.FromFloat(StormZMaxFloat)
	StormZSpawnOffset      = vmath.FromFloat(StormZSpawnOffsetFloat)

	StormBoundaryInsetX = int(StormBoundaryInsetXFloat)
	StormBoundaryInsetY = int(StormBoundaryInsetYFloat)

	StormCollisionRadius = vmath.FromFloat(StormCircleRadiusCollisionFloat)
	StormCollisionInvRxSq,
	StormCollisionInvRySq = vmath.EllipseInvRadiiSq(StormCircleRadiusX, StormCircleRadiusY)
)