package vmath

// GravitationalForce3D returns acceleration vector on body at posA toward posB
// G: gravitational constant (Q32.32)
// massB: attracting body mass (Q32.32)
// Returns acceleration (force/massA), not force
func GravitationalAccel3D(posA, posB Vec3, massB, G int64) Vec3 {
	delta := V3Sub(posB, posA)
	distSq := V3MagSq(delta)

	// Prevent singularity, clamp minimum distance
	minDistSq := int64(Scale) // 1.0 in Q32.32
	if distSq < minDistSq {
		distSq = minDistSq
	}

	// accel = G * massB / distSq, direction normalized
	dist := Sqrt(distSq)
	accelMag := Div(Mul(G, massB), distSq)

	return Vec3{
		Mul(Div(delta.X, dist), accelMag),
		Mul(Div(delta.Y, dist), accelMag),
		Mul(Div(delta.Z, dist), accelMag),
	}
}

// ElasticCollision3D computes post-collision velocities
// Returns (newVelA, newVelB, collided)
func ElasticCollision3D(posA, posB, velA, velB Vec3, massA, massB, restitution int64) (Vec3, Vec3, bool) {
	delta := V3Sub(posB, posA)
	dist := V3Mag(delta)
	if dist == 0 {
		return velA, velB, false
	}

	// Normal from A to B
	n := Vec3{Div(delta.X, dist), Div(delta.Y, dist), Div(delta.Z, dist)}

	// Relative velocity
	relVel := V3Sub(velA, velB)
	vn := V3Dot(relVel, n)

	// Already separating
	if vn <= 0 {
		return velA, velB, false
	}

	// Inverse masses
	invA := Div(Scale, massA)
	invB := Div(Scale, massB)
	invSum := invA + invB
	if invSum == 0 {
		return velA, velB, false
	}

	// Impulse scalar: j = (1 + e) * vn / (1/mA + 1/mB)
	j := Div(Mul(Scale+restitution, vn), invSum)

	newVelA := V3Sub(velA, V3Scale(n, Mul(j, invA)))
	newVelB := V3Add(velB, V3Scale(n, Mul(j, invB)))

	return newVelA, newVelB, true
}

// SeparateOverlap3D pushes overlapping spheres apart
// Returns (newPosA, newPosB, separated)
func SeparateOverlap3D(posA, posB Vec3, radiusA, radiusB, massA, massB int64) (Vec3, Vec3, bool) {
	delta := V3Sub(posB, posA)
	dist := V3Mag(delta)
	minDist := radiusA + radiusB

	if dist >= minDist || dist == 0 {
		return posA, posB, false
	}

	overlap := minDist - dist
	n := Vec3{Div(delta.X, dist), Div(delta.Y, dist), Div(delta.Z, dist)}

	totalMass := massA + massB
	ratioA := Div(massB, totalMass)
	ratioB := Div(massA, totalMass)

	margin := int64(Scale / 16) // Small extra separation

	newPosA := V3Sub(posA, V3Scale(n, Mul(overlap+margin, ratioA)))
	newPosB := V3Add(posB, V3Scale(n, Mul(overlap+margin, ratioB)))

	return newPosA, newPosB, true
}

// ReflectAxis3D clamps position component and reflects velocity on boundary
func ReflectAxis3D(pos, vel *int64, lo, hi, restitution int64) bool {
	if *pos < lo {
		*pos = lo
		if *vel < 0 {
			*vel = -Mul(*vel, restitution)
		}
		return true
	}
	if *pos > hi {
		*pos = hi
		if *vel > 0 {
			*vel = -Mul(*vel, restitution)
		}
		return true
	}
	return false
}