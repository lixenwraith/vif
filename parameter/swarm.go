package parameter

import (
	"time"

	"github.com/lixenwraith/vi-fighter/vmath"
)

// Swarm Entity
const (
	// SwarmWidth is horizontal cell count
	SwarmWidth = 4
	// SwarmHeight is vertical cell count
	SwarmHeight = 2

	// SwarmHeaderOffsetX is phantom head X offset from top-left
	SwarmHeaderOffsetX = 1
	// SwarmHeaderOffsetY is phantom head Y offset from top-left
	SwarmHeaderOffsetY = 0

	// SwarmPatternCount is number of visual patterns
	SwarmPatternCount = 2
	// SwarmPatternDuration is time per pattern before cycling
	SwarmPatternDuration = 500 * time.Millisecond

	// SwarmChargeInterval is time between charge sequences
	SwarmChargeInterval = 5 * time.Second
	// SwarmLockDuration is freeze time before charge
	SwarmLockDuration = 2 * time.Second
	// SwarmChargeDuration is charge travel time (fixed, speed calculated from distance)
	SwarmChargeDuration = 800 * time.Millisecond
	// SwarmDecelerationDuration is rapid stop after charge
	SwarmDecelerationDuration = 100 * time.Millisecond

	// Swarm Charge Line (warning pulse before charge)
	// Number of visual pulses during lock phase (3rd = actual charge)
	SwarmChargeLinePulseCount = 2
	// Delay before first pulse = LockDuration - PulseCount * ChargeDuration
	// Negative value means pulses extend before lock (clamp to 0 at runtime)
	SwarmChargeLineShowDelay = SwarmLockDuration - SwarmChargeLinePulseCount*SwarmChargeDuration
	// Trail fade length as fraction of total line distance
	SwarmChargeLineTrailFloat = 0.25
	// Peak bg alpha for first pulse
	SwarmChargeLineAlpha1Float = 0.25
	// Peak bg alpha for second pulse (escalation)
	SwarmChargeLineAlpha2Float = 0.40
	// 256-color visibility threshold
	SwarmChargeLine256Threshold = 0.15

	// SwarmLifetime is maximum swarm lifespan
	SwarmLifetime = 35 * time.Second
	// SwarmMaxCharges is charge count before despawn
	SwarmMaxCharges = 5

	// SwarmChaseSpeedMultiplier relative to drain base speed
	SwarmChaseSpeedMultiplier = 4

	// SwarmFuseAnimationDuration matches spirit convergence timing
	SwarmFuseAnimationDuration = 500 * time.Millisecond

	// SwarmHomingAccelFloat is acceleration toward cursor (cells/sec²)
	SwarmHomingAccelFloat = 6.0
	// SwarmDragFloat is deceleration when overspeed (1/sec)
	SwarmDragFloat = 2.0
	// SwarmDeflectAngleVarFloat is half-angle of random deflection cone (radians)
	SwarmDeflectAngleVarFloat = 0.25
)

// Swarm Teleport
const (
	// SwarmTeleportDuration is visual effect duration before instant move
	SwarmTeleportDuration = 400 * time.Millisecond

	// SwarmTeleportBeamAlphaFloat is peak beam opacity
	SwarmTeleportBeamAlphaFloat = 0.5
	// SwarmTeleportBeamTrailFloat is trail length fraction
	SwarmTeleportBeamTrailFloat = 0.3
	// SwarmTeleport256Threshold for 256-color visibility
	SwarmTeleport256Threshold = 0.15
)

// Swarm physics
var (
	SwarmChaseSpeed      = vmath.Mul(DrainBaseSpeed, vmath.FromInt(SwarmChaseSpeedMultiplier))
	SwarmHomingAccel     = vmath.FromFloat(SwarmHomingAccelFloat)
	SwarmDrag            = vmath.FromFloat(SwarmDragFloat)
	SwarmDeflectAngleVar = vmath.FromFloat(SwarmDeflectAngleVarFloat)
)

// Entity collision radii (ellipse semi-axes for overlap detection)
var (
	SwarmCollisionRadiusX                        = vmath.FromFloat(SwarmCollisionRadiusXFloat)
	SwarmCollisionRadiusY                        = vmath.FromFloat(SwarmCollisionRadiusYFloat)
	SwarmCollisionInvRxSq, SwarmCollisionInvRySq = vmath.EllipseInvRadiiSq(SwarmCollisionRadiusX, SwarmCollisionRadiusY)
)
