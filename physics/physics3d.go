package physics

import (
	"math"

	"github.com/lixenwraith/vi-fighter/vmath"
)

// GravitationalForce3D returns acceleration vector on body at posA toward posB
// G: gravitational constant (Q32.32)
// massB: attracting body mass (Q32.32)
// Returns acceleration (force/massA), not force
func GravitationalAccel3D(posA, posB vmath.Vec3, massB, G int64) vmath.Vec3 {
	delta := vmath.V3Sub(posB, posA)
	distSq := vmath.V3MagSq(delta)

	// Prevent singularity, clamp minimum distance
	minDistSq := vmath.Scale // 1.0 in Q32.32
	if distSq < minDistSq {
		distSq = minDistSq
	}

	// accel = G * massB / distSq, direction normalized
	dist := vmath.Sqrt(distSq)
	accelMag := vmath.Div(vmath.Mul(G, massB), distSq)

	return vmath.Vec3{
		vmath.Mul(vmath.Div(delta.X, dist), accelMag),
		vmath.Mul(vmath.Div(delta.Y, dist), accelMag),
		vmath.Mul(vmath.Div(delta.Z, dist), accelMag),
	}
}

// ElasticCollision3D computes post-collision velocities
// Returns (newVelA, newVelB, collided)
// Optimization: Delta-first conversion.
// 1. Performs subtraction in int64 (cheaper, higher precision for local deltas).
// 2. Reduces int->float conversions from 15 to 9.
// 3. Uses inverse distance to replace 3 divisions with multiplications.
func ElasticCollision3D(posA, posB, velA, velB vmath.Vec3, massA, massB, restitution int64) (vmath.Vec3, vmath.Vec3, bool) {
	// 1. Calculate relative position in int64, then convert (Saves 3 conversions)
	dx := float64(posB.X - posA.X)
	dy := float64(posB.Y - posA.Y)
	dz := float64(posB.Z - posA.Z)

	distSq := dx*dx + dy*dy + dz*dz
	if distSq == 0 {
		return velA, velB, false
	}

	dist := math.Sqrt(distSq)

	// 2. Normal (Multiplication is faster than Division)
	invDist := 1.0 / dist
	nx, ny, nz := dx*invDist, dy*invDist, dz*invDist

	// 3. Relative Velocity in int64, then convert (Saves 3 conversions)
	relVx := float64(velA.X - velB.X)
	relVy := float64(velA.Y - velB.Y)
	relVz := float64(velA.Z - velB.Z)

	vn := relVx*nx + relVy*ny + relVz*nz

	// Separating?
	if vn <= 0 {
		return velA, velB, false
	}

	// 4. Inverse Masses
	// Optimization: Pre-calculate float masses only once
	invA := 1.0 / float64(massA)
	invB := 1.0 / float64(massB)
	invSum := invA + invB

	if invSum == 0 {
		return velA, velB, false
	}

	// Impulse scalar
	fRest := float64(restitution) / vmath.ScaleF
	j := (1.0 + fRest) * vn / invSum

	// Apply impulse
	// Impulse vector = j * normal
	jAx, jAy, jAz := j*invA*nx, j*invA*ny, j*invA*nz
	jBx, jBy, jBz := j*invB*nx, j*invB*ny, j*invB*nz

	// 5. Apply to velocities (Float math for precision, convert back once)
	// We use the original int64 velocities as the base to minimize drift
	newVelA := vmath.Vec3{
		X: velA.X - int64(jAx),
		Y: velA.Y - int64(jAy),
		Z: velA.Z - int64(jAz),
	}

	newVelB := vmath.Vec3{
		X: velB.X + int64(jBx),
		Y: velB.Y + int64(jBy),
		Z: velB.Z + int64(jBz),
	}

	return newVelA, newVelB, true
}

// ElasticCollision3DInPlace modifies velocities in place
// Optimization: Eliminates 144 bytes of stack copies per call
func ElasticCollision3DInPlace(
	posA, posB *vmath.Vec3,
	velA, velB *vmath.Vec3,
	massA, massB, restitution int64,
) bool {
	dx := float64(posB.X - posA.X)
	dy := float64(posB.Y - posA.Y)
	dz := float64(posB.Z - posA.Z)

	distSq := dx*dx + dy*dy + dz*dz
	if distSq == 0 {
		return false
	}

	dist := math.Sqrt(distSq)
	invDist := 1.0 / dist
	nx, ny, nz := dx*invDist, dy*invDist, dz*invDist

	relVx := float64(velA.X - velB.X)
	relVy := float64(velA.Y - velB.Y)
	relVz := float64(velA.Z - velB.Z)

	vn := relVx*nx + relVy*ny + relVz*nz
	if vn <= 0 {
		return false
	}

	invA := 1.0 / float64(massA)
	invB := 1.0 / float64(massB)
	invSum := invA + invB
	if invSum == 0 {
		return false
	}

	fRest := float64(restitution) / vmath.ScaleF
	j := (1.0 + fRest) * vn / invSum

	jInvA := j * invA
	jInvB := j * invB

	velA.X -= int64(jInvA * nx)
	velA.Y -= int64(jInvA * ny)
	velA.Z -= int64(jInvA * nz)
	velB.X += int64(jInvB * nx)
	velB.Y += int64(jInvB * ny)
	velB.Z += int64(jInvB * nz)

	return true
}

// ElasticCollision3DF is float64-native elastic collision
// No conversion overhead - use with Vec3F physics state
func ElasticCollision3DF(
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
	if vn <= 0 {
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

// SeparateOverlap3D pushes overlapping spheres apart
// Returns (newPosA, newPosB, separated)
func SeparateOverlap3D(posA, posB vmath.Vec3, radiusA, radiusB, massA, massB int64) (vmath.Vec3, vmath.Vec3, bool) {
	delta := vmath.V3Sub(posB, posA)
	dist := vmath.V3Mag(delta)
	minDist := radiusA + radiusB

	if dist >= minDist || dist == 0 {
		return posA, posB, false
	}

	overlap := minDist - dist
	n := vmath.Vec3{vmath.Div(delta.X, dist), vmath.Div(delta.Y, dist), vmath.Div(delta.Z, dist)}

	totalMass := massA + massB
	ratioA := vmath.Div(massB, totalMass)
	ratioB := vmath.Div(massA, totalMass)

	margin := vmath.Scale / 16 // Small extra separation

	newPosA := vmath.V3Sub(posA, vmath.V3Scale(n, vmath.Mul(overlap+margin, ratioA)))
	newPosB := vmath.V3Add(posB, vmath.V3Scale(n, vmath.Mul(overlap+margin, ratioB)))

	return newPosA, newPosB, true
}

// SeparateOverlap3DF is float64-native overlap separation
func SeparateOverlap3DF(posA, posB *vmath.Vec3F, radiusA, radiusB, massA, massB float64) bool {
	dx := posB.X - posA.X
	dy := posB.Y - posA.Y
	dz := posB.Z - posA.Z

	distSq := dx*dx + dy*dy + dz*dz
	minDist := radiusA + radiusB
	minDistSq := minDist * minDist

	if distSq >= minDistSq || distSq == 0 {
		return false
	}

	dist := math.Sqrt(distSq)
	overlap := minDist - dist
	invDist := 1.0 / dist

	nx, ny, nz := dx*invDist, dy*invDist, dz*invDist

	totalMass := massA + massB
	ratioA := massB / totalMass
	ratioB := massA / totalMass

	margin := 0.0625 // Small extra separation

	sepA := (overlap + margin) * ratioA
	sepB := (overlap + margin) * ratioB

	posA.X -= nx * sepA
	posA.Y -= ny * sepA
	posA.Z -= nz * sepA
	posB.X += nx * sepB
	posB.Y += ny * sepB
	posB.Z += nz * sepB

	return true
}

// ReflectAxis3D clamps position component and reflects velocity on boundary
func ReflectAxis3D(pos, vel *int64, lo, hi, restitution int64) bool {
	if *pos < lo {
		*pos = lo
		if *vel < 0 {
			*vel = -vmath.Mul(*vel, restitution)
		}
		return true
	}
	if *pos > hi {
		*pos = hi
		if *vel > 0 {
			*vel = -vmath.Mul(*vel, restitution)
		}
		return true
	}
	return false
}

// GravitationalAccelWithRepulsion3D returns acceleration combining gravity and soft repulsion
// Beyond repulsionRadius: gravitational attraction (inverse square)
// Within repulsionRadius: linear repulsion (strongest at center)
func GravitationalAccelWithRepulsion3D(posA, posB vmath.Vec3, massB, G, repulsionRadius, repulsionStrength int64) vmath.Vec3 {
	delta := vmath.V3Sub(posB, posA)
	dist := vmath.V3Mag(delta)

	if dist == 0 {
		return vmath.Vec3{}
	}

	// Normalized direction (A toward B)
	dirX := vmath.Div(delta.X, dist)
	dirY := vmath.Div(delta.Y, dist)
	dirZ := vmath.Div(delta.Z, dist)

	var accelMag int64

	if dist < repulsionRadius {
		// Repulsion zone: linear falloff from center
		// accel = -strength * (1 - dist/radius)
		factor := vmath.Scale - vmath.Div(dist, repulsionRadius)
		accelMag = -vmath.Mul(repulsionStrength, factor)
	} else {
		// Gravity zone: inverse square attraction
		distSq := vmath.Mul(dist, dist)
		minDistSq := vmath.Scale
		if distSq < minDistSq {
			distSq = minDistSq
		}
		accelMag = vmath.Div(vmath.Mul(G, massB), distSq)
	}

	return vmath.Vec3{
		vmath.Mul(dirX, accelMag),
		vmath.Mul(dirY, accelMag),
		vmath.Mul(dirZ, accelMag),
	}
}