package physics

import "github.com/lixenwraith/vi-fighter/pkg/vmath"

// IntegratePosition advances position by velocity, ignoring acceleration
func IntegratePosition(k *Kinetic, dt int64) (x, y int) {
	k.PreciseX += vmath.Mul(k.VelX, dt)
	k.PreciseY += vmath.Mul(k.VelY, dt)
	return vmath.ToInt(k.PreciseX), vmath.ToInt(k.PreciseY)
}

// ScaleVelocity multiplies velocity by factor
func ScaleVelocity(k *Kinetic, factor int64) {
	k.VelX = vmath.Mul(k.VelX, factor)
	k.VelY = vmath.Mul(k.VelY, factor)
}

// ApplyLinearDrag scales velocity by (1 - rate*dt), clamped to [0, 1]
func ApplyLinearDrag(k *Kinetic, rate, dt int64) {
	f := vmath.Scale - vmath.Mul(rate, dt)
	if f < 0 {
		f = 0
	}
	if f > vmath.Scale {
		f = vmath.Scale
	}
	ScaleVelocity(k, f)
}

// ApplyQuadraticDrag scales velocity by (1 - coeff*dt*speed), clamped to [0, 1]
// Drag grows with speed; coeff is per cell
func ApplyQuadraticDrag(k *Kinetic, coeff, dt int64) {
	speed := vmath.Magnitude(k.VelX, k.VelY)
	if speed <= 0 {
		return
	}
	amount := vmath.Mul(vmath.Mul(coeff, dt), speed)
	if amount > vmath.Scale {
		amount = vmath.Scale
	}
	ScaleVelocity(k, vmath.Scale-amount)
}

// TurnSeverity returns how far the heading falls below the alignment threshold
// relative to the direction to (targetX, targetY); 0 when aligned or at/below minSpeed
func TurnSeverity(k *Kinetic, targetX, targetY, threshold, minSpeed int64) int64 {
	speed := vmath.Magnitude(k.VelX, k.VelY)
	if speed <= minSpeed {
		return 0
	}
	nx := vmath.Div(k.VelX, speed)
	ny := vmath.Div(k.VelY, speed)
	dnx, dny := vmath.Normalize2D(targetX-k.PreciseX, targetY-k.PreciseY)

	alignment := vmath.DotProduct(nx, ny, dnx, dny)
	if alignment >= threshold {
		return 0
	}
	return threshold - alignment
}

// ReflectVelocityX flips and scales the X velocity component
func ReflectVelocityX(k *Kinetic, restitution int64) {
	k.VelX = -vmath.Mul(k.VelX, restitution)
}

// ReflectVelocityY flips and scales the Y velocity component
func ReflectVelocityY(k *Kinetic, restitution int64) {
	k.VelY = -vmath.Mul(k.VelY, restitution)
}

// ReflectBoundsDampedX clamps to the cell range and scales the reflected velocity
func ReflectBoundsDampedX(k *Kinetic, minX, maxX int, restitution int64) bool {
	if !ReflectBoundsX(k, minX, maxX) {
		return false
	}
	k.VelX = vmath.Mul(k.VelX, restitution)
	return true
}

// ReflectBoundsDampedY clamps to the cell range and scales the reflected velocity
func ReflectBoundsDampedY(k *Kinetic, minY, maxY int, restitution int64) bool {
	if !ReflectBoundsY(k, minY, maxY) {
		return false
	}
	k.VelY = vmath.Mul(k.VelY, restitution)
	return true
}

// SpringToRest accelerates toward (restX, restY) and damps velocity;
// force magnitude is clamped to maxForce
func SpringToRest(k *Kinetic, restX, restY, stiffness, damping, maxForce, dt int64) {
	fx := vmath.Mul(restX-k.PreciseX, stiffness)
	fy := vmath.Mul(restY-k.PreciseY, stiffness)
	fx, fy = vmath.ClampMagnitude(fx, fy, maxForce)

	k.VelX += vmath.Mul(fx, dt)
	k.VelY += vmath.Mul(fy, dt)
	ScaleVelocity(k, damping)
}
