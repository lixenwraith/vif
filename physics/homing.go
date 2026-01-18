package physics

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// HomingProfile defines homing behavior parameters
type HomingProfile struct {
	BaseSpeed   int64 // Target cruising speed (Q32.32 cells/sec)
	HomingAccel int64 // Acceleration toward target (Q32.32 cells/sec²)
	Drag        int64 // Deceleration when overspeed (Q32.32 1/sec)

	// Arrival steering (0 = disabled)
	ArrivalRadius    int64 // Distance at which arrival steering begins (Q32.32)
	ArrivalDragBoost int64 // Max drag multiplier at target (Q32.32, Scale = 1x)

	// Dead zone snap (0 = use default settling)
	DeadZone int64 // Snap-to-target threshold (Q32.32)
}

// ApplyHoming updates velocity to home toward target position
// Returns true if entity is within settling distance (near-stationary at target)
// targetX, targetY: target position in Q32.32
// dt: delta time in Q32.32 seconds
func ApplyHoming(
	kinetic *component.Kinetic,
	targetX, targetY int64,
	profile *HomingProfile,
	dt int64,
) bool {
	return applyHomingInternal(kinetic, targetX, targetY, profile, vmath.Scale, dt, true)
}

// ApplyHomingScaled applies homing with speed multiplier (for progressive difficulty)
// speedMultiplier: Q32.32 scale factor (Scale = 1.0x)
// applyDrag: if false, skip drag application (for immunity-gated drag)
func ApplyHomingScaled(
	kinetic *component.Kinetic,
	targetX, targetY int64,
	profile *HomingProfile,
	speedMultiplier int64,
	dt int64,
	applyDrag bool,
) bool {
	return applyHomingInternal(kinetic, targetX, targetY, profile, speedMultiplier, dt, applyDrag)
}

// applyHomingInternal is the shared implementation
func applyHomingInternal(
	kinetic *component.Kinetic,
	targetX, targetY int64,
	profile *HomingProfile,
	speedMultiplier int64,
	dt int64,
	applyDrag bool,
) bool {
	dx := targetX - kinetic.PreciseX
	dy := targetY - kinetic.PreciseY
	dist := vmath.Magnitude(dx, dy)

	// Dead zone snap: if configured, snap to target when very close
	deadZone := profile.DeadZone
	if deadZone == 0 {
		deadZone = vmath.Scale / 4 // Default: 0.25 cells
	}

	speed := vmath.Magnitude(kinetic.VelX, kinetic.VelY)
	settleSpeedThreshold := int64(vmath.Scale) / 2 // 0.5 cells/sec

	if dist < deadZone && speed < settleSpeedThreshold {
		// Snap to exact target
		kinetic.PreciseX = targetX
		kinetic.PreciseY = targetY
		kinetic.VelX = 0
		kinetic.VelY = 0
		return true
	}

	// Calculate effective acceleration and drag
	effectiveAccel := vmath.Mul(profile.HomingAccel, speedMultiplier)
	effectiveDrag := profile.Drag

	// Arrival steering: ramp down accel, ramp up drag when near target
	if profile.ArrivalRadius > 0 && dist < profile.ArrivalRadius {
		// Factor: 0 at target, Scale at edge of arrival radius
		factor := vmath.Div(dist, profile.ArrivalRadius)

		// Ramp down acceleration
		effectiveAccel = vmath.Mul(effectiveAccel, factor)

		// Ramp up drag: base + boost * (1 - factor)
		if profile.ArrivalDragBoost > 0 {
			boostFactor := vmath.Scale - factor
			dragBoost := vmath.Mul(profile.ArrivalDragBoost, boostFactor)
			effectiveDrag = vmath.Mul(effectiveDrag, vmath.Scale+dragBoost)
		}
	}

	// Apply homing acceleration
	dirX, dirY := vmath.Normalize2D(dx, dy)
	kinetic.VelX += vmath.Mul(vmath.Mul(dirX, effectiveAccel), dt)
	kinetic.VelY += vmath.Mul(vmath.Mul(dirY, effectiveAccel), dt)

	// Apply drag if enabled and overspeed
	if applyDrag {
		effectiveBaseSpeed := vmath.Mul(profile.BaseSpeed, speedMultiplier)
		currentSpeed := vmath.Magnitude(kinetic.VelX, kinetic.VelY)

		if currentSpeed > effectiveBaseSpeed && currentSpeed > 0 {
			excess := currentSpeed - effectiveBaseSpeed
			dragScale := vmath.Div(excess, currentSpeed)
			dragAmount := vmath.Mul(vmath.Mul(effectiveDrag, dt), dragScale)

			// Clamp drag to prevent overshoot
			if dragAmount > vmath.Scale {
				dragAmount = vmath.Scale
			}

			kinetic.VelX -= vmath.Mul(kinetic.VelX, dragAmount)
			kinetic.VelY -= vmath.Mul(kinetic.VelY, dragAmount)
		}
	}

	return false
}