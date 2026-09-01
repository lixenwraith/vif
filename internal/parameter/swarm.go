package parameter

import (
	"time"

	"github.com/lixenwraith/vi-fighter/pkg/vmath"
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

	// SwarmTransitionRetryInterval re-arms a state timer whose transition could not
	// resolve this tick. It exists so a failed entry is a short delay rather than a
	// wedge: a timer left expired takes its transition branch on every subsequent
	// tick, and the branch returns before the state integrates.
	SwarmTransitionRetryInterval = 200 * time.Millisecond

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
	SwarmChargeLineTrail = 0.25
	// Peak bg alpha for first pulse
	SwarmChargeLineAlpha1 = 0.25
	// Peak bg alpha for second pulse (escalation)
	SwarmChargeLineAlpha2 = 0.40
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

	// SwarmHomingAccel is acceleration toward cursor (cells/sec²)
	SwarmHomingAccel = 6.0
	// SwarmDrag is deceleration when overspeed (1/sec)
	SwarmDrag = 2.0
	// SwarmDeflectAngleVar is half-angle of random deflection cone (radians)
	SwarmDeflectAngleVar = 0.25

	// SwarmRestitution is velocity retained on wall/boundary bounce
	SwarmRestitution = 0.5
	// SwarmDecelDrag is the per-tick velocity scale during the decelerate phase
	SwarmDecelDrag = 0.1
)

// Swarm Teleport
const (
	// SwarmTeleportDuration is visual effect duration before instant move
	SwarmTeleportDuration = 400 * time.Millisecond

	// SwarmTeleportBeamAlpha is peak beam opacity
	SwarmTeleportBeamAlpha = 0.5
	// SwarmTeleportBeamTrail is trail length fraction
	SwarmTeleportBeamTrail = 0.3
	// SwarmTeleport256Threshold for 256-color visibility
	SwarmTeleport256Threshold = 0.15
)

// Entity collision radii (ellipse semi-axes for overlap detection)
var SwarmCollisionInvRxSq, SwarmCollisionInvRySq = vmath.EllipseInvRadiiSqF(
	SwarmCollisionRadiusX, SwarmCollisionRadiusY)
