package physics

import "github.com/lixenwraith/vi-fighter/pkg/vmath"

// HomingProfileF defines homing behavior parameters
type HomingProfileF struct {
	BaseSpeed   float64 // Target cruising speed (cells/sec)
	HomingAccel float64 // Acceleration toward target (cells/sec²)
	Drag        float64 // Deceleration when overspeed (1/sec)

	// Arrival steering (0 = disabled)
	ArrivalRadius    float64 // Distance at which arrival steering begins (cells)
	ArrivalDragBoost float64 // Max drag multiplier at target (0 = 1x)
	ArrivalAccelMin  float64 // Minimum accel factor at target (0 = scale to zero, 1 = no scaling)

	// Dead zone snap (0 = use default settling)
	DeadZone float64 // Snap-to-target threshold (cells)
}

// homingSettleSpeedSq is the squared speed below which a dead-zone entity settles
const homingSettleSpeedSq = 0.25 // (0.5 cells/sec)²

// homingDefaultDeadZone is the snap radius when the profile leaves DeadZone unset
const homingDefaultDeadZone = 0.5

// ApplyHomingF updates velocity to home toward target position
// Returns true if entity is within settling distance (near-stationary at target)
func ApplyHomingF(k *KineticF, targetX, targetY float64, profile *HomingProfileF, dt float64) bool {
	return applyHomingInternalF(k, targetX, targetY, profile, 1.0, dt, true)
}

// ApplyHomingScaledF applies homing with speed multiplier (for progressive difficulty)
// applyDrag: if false, skip drag application (for immunity-gated drag)
func ApplyHomingScaledF(
	k *KineticF,
	targetX, targetY float64,
	profile *HomingProfileF,
	speedMultiplier float64,
	dt float64,
	applyDrag bool,
) bool {
	return applyHomingInternalF(k, targetX, targetY, profile, speedMultiplier, dt, applyDrag)
}

// applyHomingInternalF is the shared implementation
func applyHomingInternalF(
	k *KineticF,
	targetX, targetY float64,
	profile *HomingProfileF,
	speedMultiplier float64,
	dt float64,
	applyDrag bool,
) bool {
	dx := targetX - k.PreciseX
	dy := targetY - k.PreciseY
	dist := vmath.MagnitudeF(dx, dy)

	deadZone := profile.DeadZone
	if deadZone == 0 {
		deadZone = homingDefaultDeadZone
	}

	// Squared comparison: avoids a sqrt on the settle check
	if dist < deadZone && vmath.MagnitudeSqF(k.VelX, k.VelY) < homingSettleSpeedSq {
		k.PreciseX = targetX
		k.PreciseY = targetY
		k.VelX = 0
		k.VelY = 0
		return true
	}

	effectiveAccel := profile.HomingAccel * speedMultiplier
	effectiveDrag := profile.Drag
	effectiveBaseSpeed := profile.BaseSpeed * speedMultiplier

	// Arrival steering: ramp down accel and cruise speed, ramp up drag
	if profile.ArrivalRadius > 0 && dist < profile.ArrivalRadius {
		factor := dist / profile.ArrivalRadius

		effectiveAccel *= vmath.LerpF(profile.ArrivalAccelMin, 1.0, factor)

		// Cruise speed ramps to zero at the target so drag engages below BaseSpeed;
		// Without this the actor orbits at cruise speed forever
		effectiveBaseSpeed *= factor

		if profile.ArrivalDragBoost > 0 {
			effectiveDrag *= 1.0 + profile.ArrivalDragBoost*(1.0-factor)
		}
	}

	// Apply homing acceleration; dist is already the magnitude, so reuse it
	// instead of paying a second sqrt inside Normalize2DF.
	// Guard dist == 0: the fixed path relied on Div's zero-denominator return.
	if dist > 0 {
		dirX, dirY := dx/dist, dy/dist
		k.VelX += dirX * effectiveAccel * dt
		k.VelY += dirY * effectiveAccel * dt
	}

	if applyDrag {
		currentSpeed := vmath.MagnitudeF(k.VelX, k.VelY)

		if currentSpeed > effectiveBaseSpeed && currentSpeed > 0 {
			excess := currentSpeed - effectiveBaseSpeed
			dragScale := excess / currentSpeed
			// Clamp drag to prevent overshoot
			dragAmount := min(effectiveDrag*dt*dragScale, 1.0)

			k.VelX -= k.VelX * dragAmount
			k.VelY -= k.VelY * dragAmount
		}
	}

	return false
}
