package physics

import (
	"math"

	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// OrbitalVelocityF returns tangential velocity for circular orbit: v = sqrt(a * r)
func OrbitalVelocityF(attraction, radius float64) float64 {
	v := attraction * radius
	if v <= 0 {
		return 0
	}
	return math.Sqrt(v)
}

// OrbitalInsertF returns velocity vector for circular orbit insertion
func OrbitalInsertF(dx, dy, attraction float64, clockwise bool) (vx, vy float64) {
	radius := vmath.MagnitudeF(dx, dy)
	if radius == 0 {
		return 0, 0
	}

	speed := OrbitalVelocityF(attraction, radius)

	// Tangent is perpendicular to radius
	tx, ty := vmath.PerpendicularF(dx, dy)
	tx, ty = vmath.Normalize2DF(tx, ty)

	if clockwise {
		tx, ty = -tx, -ty
	}

	return tx * speed, ty * speed
}

// OrbitalAttractionF returns linear acceleration toward center for orbital motion
func OrbitalAttractionF(dx, dy, attraction float64) (ax, ay float64) {
	if dx == 0 && dy == 0 {
		return 0, 0
	}
	dirX, dirY := vmath.Normalize2DF(-dx, -dy) // toward center
	return dirX * attraction, dirY * attraction
}

// OrbitalDampF applies damping to circularize an elliptical orbit
// damping: factor per second (1.0 = full damp); returns velocity trending toward circular
func OrbitalDampF(vx, vy, dx, dy, damping, dt float64) (nvx, nvy float64) {
	distSq := dx*dx + dy*dy
	if distSq == 0 {
		return vx, vy
	}

	invDist := 1.0 / math.Sqrt(distSq)
	rx, ry := dx*invDist, dy*invDist

	// Radial component of velocity
	radialSpeed := vx*rx + vy*ry

	dampFactor := 1.0 - damping*dt
	if dampFactor < 0 {
		dampFactor = 0
	}

	deltaRadial := radialSpeed*dampFactor - radialSpeed
	return vx + deltaRadial*rx, vy + deltaRadial*ry
}

// OrbitalEquilibriumF returns acceleration toward target orbit radius
// Positive when outside target (pull in), negative when inside (push out)
func OrbitalEquilibriumF(dx, dy, targetRadius, stiffness float64) (ax, ay float64) {
	dist := vmath.MagnitudeF(dx, dy)
	if dist == 0 {
		// At center, push outward in an arbitrary direction
		return stiffness, 0
	}

	forceMag := (dist - targetRadius) * stiffness

	dirX := -dx / dist
	dirY := -dy / dist

	return dirX * forceMag, dirY * forceMag
}
