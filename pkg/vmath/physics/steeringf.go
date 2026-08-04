package physics

import "github.com/lixenwraith/vi-fighter/pkg/vmath"

// IntegratePositionF advances position by velocity, ignoring acceleration
func IntegratePositionF(k *KineticF, dt float64) (x, y int) {
	k.PreciseX += k.VelX * dt
	k.PreciseY += k.VelY * dt
	p := vmath.PointAtF(k.PreciseX, k.PreciseY)
	return p.X, p.Y
}

// ScaleVelocityF multiplies velocity by factor
func ScaleVelocityF(k *KineticF, factor float64) {
	k.VelX *= factor
	k.VelY *= factor
}

// ApplyLinearDragF scales velocity by (1 - rate*dt), clamped to [0, 1]
func ApplyLinearDragF(k *KineticF, rate, dt float64) {
	ScaleVelocityF(k, vmath.ClampF(1.0-rate*dt, 0, 1.0))
}

// ApplyQuadraticDragF scales velocity by (1 - coeff*dt*speed), clamped to [0, 1]
func ApplyQuadraticDragF(k *KineticF, coeff, dt float64) {
	speed := vmath.MagnitudeF(k.VelX, k.VelY)
	if speed <= 0 {
		return
	}
	ScaleVelocityF(k, vmath.ClampF(1.0-coeff*dt*speed, 0, 1.0))
}

// TurnSeverityF returns how far the heading falls below the alignment threshold
// relative to the direction to (targetX, targetY); 0 when aligned or at/below minSpeed
func TurnSeverityF(k *KineticF, targetX, targetY, threshold, minSpeed float64) float64 {
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

// ReflectVelocityXF flips and scales the X velocity component
func ReflectVelocityXF(k *KineticF, restitution float64) { k.VelX = -k.VelX * restitution }

// ReflectVelocityYF flips and scales the Y velocity component
func ReflectVelocityYF(k *KineticF, restitution float64) { k.VelY = -k.VelY * restitution }

// ReflectBoundsDampedXF clamps to the cell range and scales the reflected velocity
func ReflectBoundsDampedXF(k *KineticF, minX, maxX int, restitution float64) bool {
	if !ReflectBoundsXF(k, minX, maxX) {
		return false
	}
	k.VelX *= restitution
	return true
}

// ReflectBoundsDampedYF clamps to the cell range and scales the reflected velocity
func ReflectBoundsDampedYF(k *KineticF, minY, maxY int, restitution float64) bool {
	if !ReflectBoundsYF(k, minY, maxY) {
		return false
	}
	k.VelY *= restitution
	return true
}

// SpringToRestF accelerates toward (restX, restY) and damps velocity;
// force magnitude is clamped to maxForce
func SpringToRestF(k *KineticF, restX, restY, stiffness, damping, maxForce, dt float64) {
	fx := (restX - k.PreciseX) * stiffness
	fy := (restY - k.PreciseY) * stiffness
	fx, fy = vmath.ClampMagnitudeF(fx, fy, maxForce)

	k.VelX += fx * dt
	k.VelY += fy * dt
	ScaleVelocityF(k, damping)
}
