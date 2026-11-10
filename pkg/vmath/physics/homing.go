package physics

import "github.com/lixenwraith/vif/pkg/vmath"

// HomingProfile defines homing behavior parameters
type HomingProfile struct {
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

// arrival returns the acceleration, overspeed drag and cruise speed a body homes
// with at dist from its target: inside ArrivalRadius the first and last ramp
// down and the drag up
func (p *HomingProfile) arrival(dist, speedMultiplier float64) (accel, drag, cruise float64) {
	accel, drag, cruise = p.HomingAccel*speedMultiplier, p.Drag, p.BaseSpeed*speedMultiplier
	if p.ArrivalRadius > 0 && dist < p.ArrivalRadius {
		factor := dist / p.ArrivalRadius
		accel *= vmath.Lerp(p.ArrivalAccelMin, 1.0, factor)
		// Cruise speed ramps to zero at the target so drag engages below BaseSpeed;
		// without this the actor orbits at cruise speed forever
		cruise *= factor
		if p.ArrivalDragBoost > 0 {
			drag *= 1.0 + p.ArrivalDragBoost*(1.0-factor)
		}
	}
	return accel, drag, cruise
}

// ApplyHoming updates velocity to home toward target position
// Returns true if entity is within settling distance (near-stationary at target)
func ApplyHoming(k *Kinetic, targetX, targetY float64, profile *HomingProfile, dt float64) bool {
	return ApplyHomingScaled(k, targetX, targetY, profile, 1.0, dt, true)
}

// ApplyHomingScaled applies homing with speed multiplier (for progressive difficulty)
// applyDrag: if false, skip drag application (for immunity-gated drag)
func ApplyHomingScaled(
	k *Kinetic,
	targetX, targetY float64,
	profile *HomingProfile,
	speedMultiplier float64,
	dt float64,
	applyDrag bool,
) bool {
	dx := targetX - k.PreciseX
	dy := targetY - k.PreciseY
	dist := vmath.Magnitude(dx, dy)

	deadZone := profile.DeadZone
	if deadZone == 0 {
		deadZone = homingDefaultDeadZone
	}

	// The snap leaves a body under an external acceleration alone: its integrator
	// applies that later in the tick, and settling here would pin the body against
	// any steady field, such as wind, by resetting it every tick
	if dist < deadZone && vmath.MagnitudeSq(k.VelX, k.VelY) < homingSettleSpeedSq &&
		k.AccelX == 0 && k.AccelY == 0 {
		k.PreciseX = targetX
		k.PreciseY = targetY
		k.VelX = 0
		k.VelY = 0
		return true
	}

	accel, drag, cruise := profile.arrival(dist, speedMultiplier)

	// dist is already the magnitude, so reuse it instead of a second sqrt
	if dist > 0 {
		dirX, dirY := dx/dist, dy/dist
		k.VelX += dirX * accel * dt
		k.VelY += dirY * accel * dt
	}

	if applyDrag {
		if speed := vmath.Magnitude(k.VelX, k.VelY); speed > cruise && speed > 0 {
			// Clamp drag to prevent overshoot
			amount := min(drag*dt*((speed-cruise)/speed), 1.0)
			k.VelX -= k.VelX * amount
			k.VelY -= k.VelY * amount
		}
	}

	return false
}

// ApplyHoming3D is ApplyHoming in a volume
func ApplyHoming3D(k *Kinetic3D, target vmath.Vec3, profile *HomingProfile, dt float64) bool {
	return ApplyHomingScaled3D(k, target, profile, 1.0, dt, true)
}

// ApplyHomingScaled3D is ApplyHomingScaled in a volume
func ApplyHomingScaled3D(
	k *Kinetic3D,
	target vmath.Vec3,
	profile *HomingProfile,
	speedMultiplier float64,
	dt float64,
	applyDrag bool,
) bool {
	d := vmath.V3Sub(target, k.Pos)
	dist := vmath.V3Mag(d)

	deadZone := profile.DeadZone
	if deadZone == 0 {
		deadZone = homingDefaultDeadZone
	}
	if dist < deadZone && vmath.V3MagSq(k.Vel) < homingSettleSpeedSq && k.Accel == (vmath.Vec3{}) {
		k.Pos, k.Vel = target, vmath.Vec3{}
		return true
	}

	accel, drag, cruise := profile.arrival(dist, speedMultiplier)
	if dist > 0 {
		k.Vel = vmath.V3Add(k.Vel, vmath.V3Scale(d, accel*dt/dist))
	}
	if applyDrag {
		if speed := vmath.V3Mag(k.Vel); speed > cruise && speed > 0 {
			k.Vel = vmath.V3Scale(k.Vel, 1.0-min(drag*dt*((speed-cruise)/speed), 1.0))
		}
	}
	return false
}
