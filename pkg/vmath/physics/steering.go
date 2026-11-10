package physics

import "github.com/lixenwraith/vif/pkg/vmath"

// IntegratePosition advances position by velocity, ignoring acceleration
func IntegratePosition(k *Kinetic, dt float64) (x, y int) {
	k.PreciseX += k.VelX * dt
	k.PreciseY += k.VelY * dt
	p := vmath.PointAt(k.PreciseX, k.PreciseY)
	return p.X, p.Y
}

// ScaleVelocity multiplies velocity by factor
func ScaleVelocity(k *Kinetic, factor float64) {
	k.VelX *= factor
	k.VelY *= factor
}

// ApplyLinearDrag scales velocity by (1 - rate*dt), clamped to [0, 1]
func ApplyLinearDrag(k *Kinetic, rate, dt float64) {
	ScaleVelocity(k, vmath.Clamp(1.0-rate*dt, 0, 1.0))
}

// ApplyQuadraticDrag scales velocity by (1 - coeff*dt*speed), clamped to [0, 1]
// Drag grows with speed; coeff is per cell
func ApplyQuadraticDrag(k *Kinetic, coeff, dt float64) {
	speed := vmath.Magnitude(k.VelX, k.VelY)
	if speed <= 0 {
		return
	}
	ScaleVelocity(k, vmath.Clamp(1.0-coeff*dt*speed, 0, 1.0))
}

// TurnSeverity returns how far the heading's cosine to (targetX, targetY) falls
// below threshold; 0 at or below minSpeed. A threshold above 1 never gates: the
// result is then a constant plus the turn term.
func TurnSeverity(k *Kinetic, targetX, targetY, threshold, minSpeed float64) float64 {
	speed := vmath.Magnitude(k.VelX, k.VelY)
	if speed <= minSpeed {
		return 0
	}
	nx, ny := k.VelX/speed, k.VelY/speed
	dnx, dny := vmath.Normalize2D(targetX-k.PreciseX, targetY-k.PreciseY)

	alignment := vmath.DotProduct(nx, ny, dnx, dny)
	if alignment >= threshold {
		return 0
	}
	return threshold - alignment
}

// ReflectVelocityX flips and scales the X velocity component
func ReflectVelocityX(k *Kinetic, restitution float64) { k.VelX = -k.VelX * restitution }

// ReflectVelocityY flips and scales the Y velocity component
func ReflectVelocityY(k *Kinetic, restitution float64) { k.VelY = -k.VelY * restitution }

// ReflectBoundsDampedX clamps to the cell range and scales the reflected velocity
func ReflectBoundsDampedX(k *Kinetic, minX, maxX int, restitution float64) bool {
	if !ReflectBoundsX(k, minX, maxX) {
		return false
	}
	k.VelX *= restitution
	return true
}

// ReflectBoundsDampedY clamps to the cell range and scales the reflected velocity
func ReflectBoundsDampedY(k *Kinetic, minY, maxY int, restitution float64) bool {
	if !ReflectBoundsY(k, minY, maxY) {
		return false
	}
	k.VelY *= restitution
	return true
}

// SpringToRest accelerates toward (restX, restY) and damps velocity;
// force magnitude is clamped to maxForce. Does not integrate position.
func SpringToRest(k *Kinetic, restX, restY, stiffness, damping, maxForce, dt float64) {
	fx := (restX - k.PreciseX) * stiffness
	fy := (restY - k.PreciseY) * stiffness
	fx, fy = vmath.ClampMagnitude(fx, fy, maxForce)

	k.VelX += fx * dt
	k.VelY += fy * dt
	ScaleVelocity(k, damping)
}

// IntegratePosition3D advances position by velocity, ignoring acceleration
func IntegratePosition3D(k *Kinetic3D, dt float64) vmath.Point3 {
	k.Pos = vmath.V3Add(k.Pos, vmath.V3Scale(k.Vel, dt))
	return vmath.PointAt3(k.Pos)
}

// ApplyLinearDrag3D scales velocity by (1 - rate*dt), clamped to [0, 1]
func ApplyLinearDrag3D(k *Kinetic3D, rate, dt float64) {
	k.Vel = vmath.V3Scale(k.Vel, vmath.Clamp(1.0-rate*dt, 0, 1.0))
}

// ApplyQuadraticDrag3D scales velocity by (1 - coeff*dt*speed), clamped to [0, 1]
func ApplyQuadraticDrag3D(k *Kinetic3D, coeff, dt float64) {
	if speed := vmath.V3Mag(k.Vel); speed > 0 {
		k.Vel = vmath.V3Scale(k.Vel, vmath.Clamp(1.0-coeff*dt*speed, 0, 1.0))
	}
}

// TurnSeverity3D is TurnSeverity in a volume: how far the heading's cosine to
// target falls below threshold, 0 at or below minSpeed
func TurnSeverity3D(k *Kinetic3D, target vmath.Vec3, threshold, minSpeed float64) float64 {
	speed := vmath.V3Mag(k.Vel)
	if speed <= minSpeed {
		return 0
	}
	alignment := vmath.V3Dot(vmath.V3Scale(k.Vel, 1/speed), vmath.V3Normalize(vmath.V3Sub(target, k.Pos)))
	if alignment >= threshold {
		return 0
	}
	return threshold - alignment
}

// SpringToRest3D accelerates toward rest and damps velocity; force magnitude is
// clamped to maxForce. Does not integrate position.
func SpringToRest3D(k *Kinetic3D, rest vmath.Vec3, stiffness, damping, maxForce, dt float64) {
	force := vmath.V3ClampMagnitude(vmath.V3Scale(vmath.V3Sub(rest, k.Pos), stiffness), maxForce)
	k.Vel = vmath.V3Scale(vmath.V3Add(k.Vel, vmath.V3Scale(force, dt)), damping)
}
