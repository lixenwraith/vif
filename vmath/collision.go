package vmath

// Mass ratio constants for collision physics (Q16.16)
// Use with ApplyCollisionImpulse for different entity weights
const (
	MassRatioEqual = Scale     // 1.0 - equal mass entities
	MassRatioLight = Scale / 2 // 0.5 - light impactor (less momentum transfer)
	MassRatioHeavy = Scale * 2 // 2.0 - heavy impactor (more momentum transfer)
)

// Entity mass constants (Q16.16, relative units)
// Baseline: single-cell entity = Scale (1.0)
const (
	MassDrain   = Scale      // 1.0 - single drain entity
	MassCleaner = Scale      // 1.0 - cleaner block
	MassQuasar  = Scale * 10 // 10.0 - fused from 10 drains
)

// Pre-computed mass ratios for collision (impactor_mass / target_mass)
const (
	MassRatioCleanerToDrain  = Scale      // 1.0 - equal mass
	MassRatioCleanerToQuasar = Scale / 10 // 0.1 - cleaner is 10x lighter
)

// OffsetInfluenceDefault is standard blend factor for offset-based impulse
// Scale/3 ≈ 0.33 - offset contributes 1/3 to final direction
const OffsetInfluenceDefault = Scale / 3

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

// ApplyOffsetCollisionImpulse calculates impulse for collision at offset from target anchor
// Combines impactor direction with "push away from hit point" effect for multi-cell entities
//
// Parameters:
//   - impactorVelX/Y: impacting object's velocity (Q16.16)
//   - offsetX/Y: hit position relative to target anchor (integer cells, converted internally)
//   - offsetInfluence: blend factor for offset direction (0 = pure impactor, Scale = equal blend)
//   - massRatio: impactor_mass / target_mass (Q16.16)
//   - angleVarRad: random angle variance in Q16.16 radians
//   - magnitudeMin/Max: impulse magnitude bounds (Q16.16 cells/sec)
//   - rng: random source (nil = no randomization)
func ApplyOffsetCollisionImpulse(
	impactorVelX, impactorVelY int32,
	offsetX, offsetY int,
	offsetInfluence int32,
	massRatio int32,
	angleVarRad int32,
	magnitudeMin, magnitudeMax int32,
	rng *FastRand,
) (impulseX, impulseY int32) {
	// Base direction from impactor velocity
	baseX, baseY := Normalize2D(impactorVelX, impactorVelY)
	if baseX == 0 && baseY == 0 {
		return 0, 0
	}

	// Offset direction: push away from hit point (negate offset)
	// Convert cell offset to Q16.16 for normalization
	if offsetInfluence > 0 && (offsetX != 0 || offsetY != 0) {
		offX := FromInt(-offsetX)
		offY := FromInt(-offsetY)
		offDirX, offDirY := Normalize2D(offX, offY)

		// Blend: base * (1 - influence) + offset * influence
		invInfluence := Scale - offsetInfluence
		baseX = Mul(baseX, invInfluence) + Mul(offDirX, offsetInfluence)
		baseY = Mul(baseY, invInfluence) + Mul(offDirY, offsetInfluence)

		// Re-normalize blended direction
		baseX, baseY = Normalize2D(baseX, baseY)
	}

	// Apply angle variance
	if angleVarRad > 0 && rng != nil {
		angleVarRot := Mul(angleVarRad, RadiansToRotation)
		angleRange := int(angleVarRot) * 2
		if angleRange > 0 {
			randomAngle := int32(rng.Intn(angleRange)) - angleVarRot
			baseX, baseY = RotateVector(baseX, baseY, randomAngle)
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

	return Mul(baseX, magnitude), Mul(baseY, magnitude)
}