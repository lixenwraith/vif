package physics

import "github.com/lixenwraith/vi-fighter/pkg/vmath"

// IntegratePosition advances position by velocity, ignoring acceleration
func IntegratePosition(k *Kinetic, dt float64) (x, y int) {
	k.PreciseX += k.VelX * dt
	k.PreciseY += k.VelY * dt
	p := vmath.PointAtF(k.PreciseX, k.PreciseY)
	return p.X, p.Y
}

// ScaleVelocity multiplies velocity by factor
func ScaleVelocity(k *Kinetic, factor float64) {
	k.VelX *= factor
	k.VelY *= factor
}

// ApplyLinearDrag scales velocity by (1 - rate*dt), clamped to [0, 1]
func ApplyLinearDrag(k *Kinetic, rate, dt float64) {
	ScaleVelocity(k, vmath.ClampF(1.0-rate*dt, 0, 1.0))
}

// ApplyQuadraticDrag scales velocity by (1 - coeff*dt*speed), clamped to [0, 1]
// Drag grows with speed; coeff is per cell
func ApplyQuadraticDrag(k *Kinetic, coeff, dt float64) {
	speed := vmath.MagnitudeF(k.VelX, k.VelY)
	if speed <= 0 {
		return
	}
	ScaleVelocity(k, vmath.ClampF(1.0-coeff*dt*speed, 0, 1.0))
}

// TurnSeverity returns how far the heading falls below the alignment threshold
// relative to the direction to (targetX, targetY); 0 when aligned or at/below minSpeed
func TurnSeverity(k *Kinetic, targetX, targetY, threshold, minSpeed float64) float64 {
	speed := vmath.MagnitudeF(k.VelX, k.VelY)
	if speed <= minSpeed {
		return 0
	}
	nx, ny := k.VelX/speed, k.VelY/speed
	dnx, dny := vmath.Normalize2DF(targetX-k.PreciseX, targetY-k.PreciseY)

	alignment := vmath.DotProductF(nx, ny, dnx, dny)
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
	fx, fy = vmath.ClampMagnitudeF(fx, fy, maxForce)

	k.VelX += fx * dt
	k.VelY += fy * dt
	ScaleVelocity(k, damping)
}
