package physics

import (
	"math"

	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// grav3DMinDistSq clamps the inverse-square singularity at one cell
const grav3DMinDistSq = 1.0

// GravitationalAccel3D returns acceleration vector on body at posA toward posB
// Returns acceleration (force/massA), not force
func GravitationalAccel3D(posA, posB vmath.Vec3F, massB, G float64) vmath.Vec3F {
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

// GravitationalAccelWithRepulsion3D combines gravity and soft repulsion
// Beyond repulsionRadius: inverse-square attraction; within: linear repulsion
func GravitationalAccelWithRepulsion3D(
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

// ElasticCollision3D computes post-collision velocities in place, returns true if resolved
func ElasticCollision3D(
	posA, posB *vmath.Vec3F,
	velA, velB *vmath.Vec3F,
	massA, massB, restitution float64,
) bool {
	dx := posB.X - posA.X
	dy := posB.Y - posA.Y
	dz := posB.Z - posA.Z

	distSq := dx*dx + dy*dy + dz*dz
	if distSq == 0 {
		return false
	}

	dist := math.Sqrt(distSq)
	invDist := 1.0 / dist
	nx, ny, nz := dx*invDist, dy*invDist, dz*invDist

	relVx := velA.X - velB.X
	relVy := velA.Y - velB.Y
	relVz := velA.Z - velB.Z

	vn := relVx*nx + relVy*ny + relVz*nz
	if vn <= 0 { // Separating
		return false
	}

	invA := 1.0 / massA
	invB := 1.0 / massB
	j := (1.0 + restitution) * vn / (invA + invB)

	jInvA := j * invA
	jInvB := j * invB

	velA.X -= jInvA * nx
	velA.Y -= jInvA * ny
	velA.Z -= jInvA * nz
	velB.X += jInvB * nx
	velB.Y += jInvB * ny
	velB.Z += jInvB * nz

	return true
}

// separateMargin is the extra gap applied when pushing overlapping spheres apart
const separateMargin = 0.0625

// SeparateOverlap3D pushes overlapping spheres apart in place, returns true if moved
func SeparateOverlap3D(posA, posB *vmath.Vec3F, radiusA, radiusB, massA, massB float64) bool {
	dx := posB.X - posA.X
	dy := posB.Y - posA.Y
	dz := posB.Z - posA.Z

	distSq := dx*dx + dy*dy + dz*dz
	minDist := radiusA + radiusB

	if distSq >= minDist*minDist || distSq == 0 {
		return false
	}

	dist := math.Sqrt(distSq)
	overlap := minDist - dist
	invDist := 1.0 / dist

	nx, ny, nz := dx*invDist, dy*invDist, dz*invDist

	totalMass := massA + massB
	sepA := (overlap + separateMargin) * (massB / totalMass)
	sepB := (overlap + separateMargin) * (massA / totalMass)

	posA.X -= nx * sepA
	posA.Y -= ny * sepA
	posA.Z -= nz * sepA
	posB.X += nx * sepB
	posB.Y += ny * sepB
	posB.Z += nz * sepB

	return true
}

// ReflectAxis3D clamps position component and reflects velocity on boundary
func ReflectAxis3D(pos, vel *float64, lo, hi, restitution float64) bool {
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
