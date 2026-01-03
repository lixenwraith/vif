package vmath


// OrbitalVelocity returns tangential velocity for circular orbit
// attraction: centripetal acceleration at unit distance (G*M equivalent)
// radius: orbital radius (Q16.16)
// Returns velocity magnitude for stable circular orbit
func OrbitalVelocity(attraction, radius int32) int32 {
	// v = sqrt(a * r)
	return Sqrt(Mul(attraction, radius))
}

// OrbitalInsert returns velocity vector for circular orbit insertion
// dx, dy: position relative to center (Q16.16)
// attraction: centripetal acceleration factor
// clockwise: orbit direction
func OrbitalInsert(dx, dy, attraction int32, clockwise bool) (vx, vy int32) {
	radius := Magnitude(dx, dy)
	if radius == 0 {
		return 0, 0
	}

	speed := OrbitalVelocity(attraction, radius)

	// Tangent is perpendicular to radius
	tx, ty := Perpendicular(dx, dy)
	tx, ty = Normalize2D(tx, ty)

	if clockwise {
		tx, ty = -tx, -ty
	}

	return Mul(tx, speed), Mul(ty, speed)
}

// OrbitalAttraction returns acceleration toward center for orbital motion
// dx, dy: position relative to center (Q16.16)
// attraction: base attraction strength
// Returns acceleration vector pointing toward center
func OrbitalAttraction(dx, dy, attraction int32) (ax, ay int32) {
	distSq := Mul(dx, dx) + Mul(dy, dy)
	if distSq == 0 {
		return 0, 0
	}

	// a = attraction / r² * direction (inverse square)
	// For linear: a = attraction * direction
	dirX, dirY := Normalize2D(-dx, -dy) // toward center

	// Linear attraction (simpler, more controllable)
	return Mul(dirX, attraction), Mul(dirY, attraction)
}

// OrbitalDamp applies damping to circularize an elliptical orbit
// vx, vy: current velocity
// dx, dy: position relative to center
// damping: factor per second (Q16.16, Scale = full damp)
// dt: delta time
// Returns damped velocity that trends toward circular
func OrbitalDamp(vx, vy, dx, dy, damping, dt int32) (nvx, nvy int32) {
	// Decompose velocity into radial and tangential
	dist := Magnitude(dx, dy)
	if dist == 0 {
		return vx, vy
	}

	// Radial unit vector
	rx := Div(dx, dist)
	ry := Div(dy, dist)

	// Radial component (dot product)
	radialSpeed := Mul(vx, rx) + Mul(vy, ry)

	// Damp radial component toward zero (circularizes orbit)
	dampFactor := Scale - Mul(damping, dt)
	if dampFactor < 0 {
		dampFactor = 0
	}

	newRadialSpeed := Mul(radialSpeed, dampFactor)
	deltaRadial := newRadialSpeed - radialSpeed

	return vx + Mul(deltaRadial, rx), vy + Mul(deltaRadial, ry)
}