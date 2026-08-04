package physics

import (
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// HomingProfile defines homing behavior parameters
type HomingProfile struct {
	BaseSpeed   int64 // Target cruising speed (Q32.32 cells/sec)
	HomingAccel int64 // Acceleration toward target (Q32.32 cells/sec²)
	Drag        int64 // Deceleration when overspeed (Q32.32 1/sec)

	// Arrival steering (0 = disabled)
	ArrivalRadius    int64 // Distance at which arrival steering begins (Q32.32)
	ArrivalDragBoost int64 // Max drag multiplier at target (Q32.32, Scale = 1x)
	ArrivalAccelMin  int64 // Minimum accel factor at target (0 = scale to zero, Scale = no scaling)

	// Dead zone snap (0 = use default settling)
	DeadZone int64 // Snap-to-target threshold (Q32.32)
}

// ApplyHoming updates velocity to home toward target position
// Returns true if entity is within settling distance (near-stationary at target)
// targetX, targetY: target position in Q32.32
// dt: delta time in Q32.32 seconds
func ApplyHoming(
	k *Kinetic,
	targetX, targetY int64,
	profile *HomingProfile,
	dt int64,
) bool {
	return applyHomingInternal(k, targetX, targetY, profile, vmath.Scale, dt, true)
}

// ApplyHomingScaled applies homing with speed multiplier (for progressive difficulty)
// speedMultiplier: Q32.32 scale factor (Scale = 1.0x)
// applyDrag: if false, skip drag application (for immunity-gated drag)
func ApplyHomingScaled(
	k *Kinetic,
	targetX, targetY int64,
	profile *HomingProfile,
	speedMultiplier int64,
	dt int64,
	applyDrag bool,
) bool {
	return applyHomingInternal(k, targetX, targetY, profile, speedMultiplier, dt, applyDrag)
}

// applyHomingInternal is the shared implementation
func applyHomingInternal(
	k *Kinetic,
	targetX, targetY int64,
	profile *HomingProfile,
	speedMultiplier int64,
	dt int64,
	applyDrag bool,
) bool {
	dx := targetX - k.PreciseX
	dy := targetY - k.PreciseY
	dist := vmath.Magnitude(dx, dy)

	// Dead zone snap: if configured, snap to target when very close
	deadZone := profile.DeadZone
	if deadZone == 0 {
		deadZone = vmath.Scale / 2 // Default: 0.5 cells
	}

	// Squared comparison: avoids a sqrt on the settle check
	const settleSpeedSq = (vmath.Scale / 2) * (vmath.Scale / 2) >> vmath.Shift
	if dist < deadZone && vmath.MagnitudeSq(k.VelX, k.VelY) < settleSpeedSq {
		// Snap to exact target
		k.PreciseX = targetX
		k.PreciseY = targetY
		k.VelX = 0
		k.VelY = 0
		return true
	}

	// Calculate effective acceleration and drag
	effectiveAccel := vmath.Mul(profile.HomingAccel, speedMultiplier)
	effectiveDrag := profile.Drag
	effectiveBaseSpeed := vmath.Mul(profile.BaseSpeed, speedMultiplier)

	// Arrival steering: ramp down accel and cruise speed, ramp up drag
	if profile.ArrivalRadius > 0 && dist < profile.ArrivalRadius {
		// Factor: 0 at target, Scale at edge of arrival radius
		factor := vmath.Div(dist, profile.ArrivalRadius)

		// Accel scaling: lerp from ArrivalAccelMin (at target) to Scale (at edge)
		accelFactor := vmath.Lerp(profile.ArrivalAccelMin, vmath.Scale, factor)

		// Ramp down acceleration
		effectiveAccel = vmath.Mul(effectiveAccel, accelFactor)

		// Cruise speed ramps to zero at the target so drag engages below BaseSpeed;
		// Without this the actor orbits at cruise speed forever
		effectiveBaseSpeed = vmath.Mul(effectiveBaseSpeed, factor)

		// Ramp up drag: base + boost * (1 - factor)
		if profile.ArrivalDragBoost > 0 {
			boostFactor := vmath.Scale - factor
			dragBoost := vmath.Mul(profile.ArrivalDragBoost, boostFactor)
			effectiveDrag = vmath.Mul(effectiveDrag, vmath.Scale+dragBoost)
		}
	}

	// Apply homing acceleration; dist is already the magnitude, so reuse it
	// instead of paying a second sqrt inside Normalize2D
	dirX, dirY := vmath.Div(dx, dist), vmath.Div(dy, dist)
	k.VelX += vmath.Mul(vmath.Mul(dirX, effectiveAccel), dt)
	k.VelY += vmath.Mul(vmath.Mul(dirY, effectiveAccel), dt)

	// Apply drag if enabled and overspeed
	if applyDrag {
		currentSpeed := vmath.Magnitude(k.VelX, k.VelY)

		if currentSpeed > effectiveBaseSpeed && currentSpeed > 0 {
			excess := currentSpeed - effectiveBaseSpeed
			dragScale := vmath.Div(excess, currentSpeed)
			// Clamp drag to prevent overshoot
			dragAmount := min(vmath.Mul(vmath.Mul(effectiveDrag, dt), dragScale), vmath.Scale)

			k.VelX -= vmath.Mul(k.VelX, dragAmount)
			k.VelY -= vmath.Mul(k.VelY, dragAmount)
		}
	}

	return false
}
