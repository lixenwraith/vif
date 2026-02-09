package physics

import (
	"math"

	"github.com/lixenwraith/vi-fighter/vmath"
)

// OrbitalVelocity returns tangential velocity for circular orbit
// attraction: centripetal acceleration at unit distance (G*M equivalent)
// radius: orbital radius (Q32.32)
// Returns velocity magnitude for stable circular orbit
func OrbitalVelocity(attraction, radius int64) int64 {
	// v = sqrt(a * r)
	return vmath.Sqrt(vmath.Mul(attraction, radius))
}

// OrbitalInsert returns velocity vector for circular orbit insertion
// dx, dy: position relative to center (Q32.32)
// attraction: centripetal acceleration factor
// clockwise: orbit direction
func OrbitalInsert(dx, dy, attraction int64, clockwise bool) (vx, vy int64) {
	radius := vmath.Magnitude(dx, dy)
	if radius == 0 {
		return 0, 0
	}

	speed := OrbitalVelocity(attraction, radius)

	// Tangent is perpendicular to radius
	tx, ty := vmath.Perpendicular(dx, dy)
	tx, ty = vmath.Normalize2D(tx, ty)

	if clockwise {
		tx, ty = -tx, -ty
	}

	return vmath.Mul(tx, speed), vmath.Mul(ty, speed)
}

// OrbitalAttraction returns acceleration toward center for orbital motion
// dx, dy: position relative to center (Q32.32)
// attraction: base attraction strength
// Returns acceleration vector pointing toward center
func OrbitalAttraction(dx, dy, attraction int64) (ax, ay int64) {
	distSq := vmath.Mul(dx, dx) + vmath.Mul(dy, dy)
	if distSq == 0 {
		return 0, 0
	}

	// a = attraction / r² * direction (inverse square)
	// For linear: a = attraction * direction
	dirX, dirY := vmath.Normalize2D(-dx, -dy) // toward center

	// Linear attraction (simpler, more controllable)
	return vmath.Mul(dirX, attraction), vmath.Mul(dirY, attraction)
}

// Precomputed constant for damp factor scaling
const invScaleSq = 1.0 / (vmath.ScaleF * vmath.ScaleF)

// OrbitalDamp applies damping to circularize an elliptical orbit
// vx, vy: current velocity
// dx, dy: position relative to center
// damping: factor per second (Q32.32, Scale = full damp)
// dt: delta time
// Returns damped velocity that trends toward circular
// Optimization: Batched float operation. Performs entire physics calculation in float domain then converts back once. Reciprocal multiplication for damping factors.
func OrbitalDamp(vx, vy, dx, dy, damping, dt int64) (nvx, nvy int64) {
	// Lift to float domain: converting position to float directly
	// Note: If dx/dy are huge (world coordinates), precision loss occurs here, but relative to the center of the orbit, this is usually acceptable
	fdx, fdy := float64(dx), float64(dy)

	distSq := fdx*fdx + fdy*fdy
	if distSq == 0 {
		return vx, vy
	}

	dist := math.Sqrt(distSq)
	invDist := 1.0 / dist

	// Normalized radial vector
	rx := fdx * invDist
	ry := fdy * invDist

	fvx, fvy := float64(vx), float64(vy)

	// Radial component of velocity (dot product)
	radialSpeed := fvx*rx + fvy*ry

	// Damp radial component
	// factor = (damping/Scale) * (dt/Scale)

	factor := float64(damping) * float64(dt) * invScaleSq
	dampFactor := 1.0 - factor
	if dampFactor < 0 {
		dampFactor = 0
	}

	newRadialSpeed := radialSpeed * dampFactor
	deltaRadial := newRadialSpeed - radialSpeed

	// Apply delta to velocity
	resVx := fvx + deltaRadial*rx
	resVy := fvy + deltaRadial*ry

	return int64(resVx), int64(resVy)
}

// OrbitalEquilibrium returns acceleration toward target orbit radius
// Positive when outside target (pull in), negative when inside (push out)
// dx, dy: position relative to center (Q32.32)
// targetRadius: desired orbit distance (Q32.32)
// stiffness: force multiplier (Q32.32)
func OrbitalEquilibrium(dx, dy, targetRadius, stiffness int64) (ax, ay int64) {
	dist := vmath.Magnitude(dx, dy)
	if dist == 0 {
		// At center, push outward in random direction
		return stiffness, 0
	}

	// Force proportional to (distance - target)
	// Positive = too far = pull in
	// Negative = too close = push out
	delta := dist - targetRadius
	forceMag := vmath.Mul(delta, stiffness)

	// Direction toward center (will be negated if delta < 0)
	dirX := vmath.Div(-dx, dist)
	dirY := vmath.Div(-dy, dist)

	return vmath.Mul(dirX, forceMag), vmath.Mul(dirY, forceMag)
}