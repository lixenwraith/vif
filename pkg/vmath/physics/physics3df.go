package physics

import (
	"math"

	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// grav3DMinDistSq clamps the inverse-square singularity at one cell
const grav3DMinDistSq = 1.0

// GravitationalAccel3DF returns acceleration vector on body at posA toward posB
// Returns acceleration (force/massA), not force
func GravitationalAccel3DF(posA, posB vmath.Vec3F, massB, G float64) vmath.Vec3F {
	delta := vmath.V3FSub(posB, posA)
	distSq := vmath.V3FMagSq(delta)

	if distSq < grav3DMinDistSq {
		distSq = grav3DMinDistSq
	}

	dist := math.Sqrt(distSq)
	accelMag := G * massB / distSq
	inv := accelMag / dist

	return vmath.Vec3F{X: delta.X * inv, Y: delta.Y * inv, Z: delta.Z * inv}
}

// GravitationalAccelWithRepulsion3DF combines gravity and soft repulsion
// Beyond repulsionRadius: inverse-square attraction; within: linear repulsion
func GravitationalAccelWithRepulsion3DF(
	posA, posB vmath.Vec3F,
	massB, G, repulsionRadius, repulsionStrength float64,
) vmath.Vec3F {
	delta := vmath.V3FSub(posB, posA)
	dist := vmath.V3FMag(delta)

	if dist == 0 {
		return vmath.Vec3F{}
	}

	invDist := 1.0 / dist
	dirX, dirY, dirZ := delta.X*invDist, delta.Y*invDist, delta.Z*invDist

	var accelMag float64
	if dist < repulsionRadius {
		// Repulsion zone: linear falloff from center
		accelMag = -repulsionStrength * (1.0 - dist/repulsionRadius)
	} else {
		distSq := dist * dist
		if distSq < grav3DMinDistSq {
			distSq = grav3DMinDistSq
		}
		accelMag = G * massB / distSq
	}

	return vmath.Vec3F{X: dirX * accelMag, Y: dirY * accelMag, Z: dirZ * accelMag}
}

// ReflectAxis3DF clamps position component and reflects velocity on boundary
func ReflectAxis3DF(pos, vel *float64, lo, hi, restitution float64) bool {
	if *pos < lo {
		*pos = lo
		if *vel < 0 {
			*vel = -*vel * restitution
		}
		return true
	}
	if *pos > hi {
		*pos = hi
		if *vel > 0 {
			*vel = -*vel * restitution
		}
		return true
	}
	return false
}

// SeparateOverlap3DValueF is the value-returning form of SeparateOverlap3DF,
// mirroring the fixed SeparateOverlap3D signature for call sites that need it
func SeparateOverlap3DValueF(posA, posB vmath.Vec3F, radiusA, radiusB, massA, massB float64) (vmath.Vec3F, vmath.Vec3F, bool) {
	a, b := posA, posB
	ok := SeparateOverlap3DF(&a, &b, radiusA, radiusB, massA, massB)
	return a, b, ok
}
