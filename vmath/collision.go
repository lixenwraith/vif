package vmath

// Mass ratio constants for collision physics (Q16.16)
// Use with ApplyCollisionImpulse for different entity weights
const (
	MassRatioEqual = Scale     // 1.0 - equal mass entities
	MassRatioLight = Scale / 2 // 0.5 - light impactor (less momentum transfer)
	MassRatioHeavy = Scale * 2 // 2.0 - heavy impactor (more momentum transfer)
)

// RadiansToRotation converts Q16.16 radians to rotation units
// Rotation units: Scale (65536) = full rotation (2π)
// Usage: rotUnits = Mul(radians, RadiansToRotation)
const RadiansToRotation int32 = 10430 // ≈ Scale / (2π)

// ApplyCollisionImpulse calculates velocity delta from collision
// Returns impulse vector to ADD to target's current velocity
//
// Parameters:
//   - impactorVelX/Y: impacting object's velocity (Q16.16)
//   - massRatio: impactor_mass / target_mass (Q16.16, Scale = equal)
//   - angleVarRad: random angle variance in Q16.16 radians (0 = none)
//   - magnitudeMin/Max: impulse magnitude bounds (Q16.16 cells/sec)
//   - rng: random source (nil = no randomization)
func ApplyCollisionImpulse(
	impactorVelX, impactorVelY int32,
	massRatio int32,
	angleVarRad int32,
	magnitudeMin, magnitudeMax int32,
	rng *FastRand,
) (impulseX, impulseY int32) {
	// Direction from impactor velocity
	dirX, dirY := Normalize2D(impactorVelX, impactorVelY)

	// Zero velocity fallback
	if dirX == 0 && dirY == 0 {
		return 0, 0
	}

	// Apply angle variance (convert radians to rotation units)
	if angleVarRad > 0 && rng != nil {
		angleVarRot := Mul(angleVarRad, RadiansToRotation)
		angleRange := int(angleVarRot) * 2
		if angleRange > 0 {
			randomAngle := int32(rng.Intn(angleRange)) - angleVarRot
			dirX, dirY = RotateVector(dirX, dirY, randomAngle)
		}
	}

	// Random magnitude in [min, max]
	magnitude := magnitudeMin
	if magnitudeMax > magnitudeMin && rng != nil {
		magRange := int(magnitudeMax - magnitudeMin)
		magnitude += int32(rng.Intn(magRange))
	}

	// Scale by mass ratio
	magnitude = Mul(magnitude, massRatio)

	return Mul(dirX, magnitude), Mul(dirY, magnitude)
}