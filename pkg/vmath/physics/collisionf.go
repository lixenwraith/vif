package physics

import "github.com/lixenwraith/vi-fighter/pkg/vmath"

// CollisionProfileF defines collision interaction parameters
type CollisionProfileF struct {
	MassRatio       float64     // Impactor/target mass ratio (1.0 = equal)
	ImpulseMin      float64     // Minimum impulse magnitude (cells/sec)
	ImpulseMax      float64     // Maximum impulse magnitude (cells/sec)
	AngleVariance   float64     // Random angle spread (radians)
	Mode            ImpulseMode // Additive or Override
	OffsetInfluence float64     // Blend factor for offset-based direction (0 = none, 1 = pure offset)
}

// ApplyCollisionF calculates and applies collision impulse
// dirX, dirY: impact direction (impactor velocity or radial vector)
func ApplyCollisionF(k *KineticF, dirX, dirY float64, profile *CollisionProfileF, rng *vmath.FastRand) {
	if dirX == 0 && dirY == 0 {
		dirX = 1.0
	}

	impulseX, impulseY := ApplyCollisionImpulseF(
		dirX, dirY,
		profile.MassRatio,
		profile.AngleVariance,
		profile.ImpulseMin, profile.ImpulseMax,
		rng,
	)

	switch profile.Mode {
	case ImpulseAdditive:
		ApplyImpulseF(k, impulseX, impulseY)
	case ImpulseOverride:
		SetImpulseF(k, impulseX, impulseY)
	}
}

// ApplyOffsetCollisionF calculates collision with offset influence for multi-cell entities
// offsetX, offsetY: hit point offset from anchor in integer cells
func ApplyOffsetCollisionF(
	k *KineticF,
	dirX, dirY float64,
	offsetX, offsetY int,
	profile *CollisionProfileF,
	rng *vmath.FastRand,
) {
	if dirX == 0 && dirY == 0 {
		dirX = 1.0
	}

	impulseX, impulseY := ApplyOffsetCollisionImpulseF(
		dirX, dirY,
		offsetX, offsetY,
		profile.OffsetInfluence,
		profile.MassRatio,
		profile.AngleVariance,
		profile.ImpulseMin, profile.ImpulseMax,
		rng,
	)

	switch profile.Mode {
	case ImpulseAdditive:
		ApplyImpulseF(k, impulseX, impulseY)
	case ImpulseOverride:
		SetImpulseF(k, impulseX, impulseY)
	}
}

// CheckSoftCollisionF tests ellipse containment and computes radial direction
func CheckSoftCollisionF(
	entityX, entityY int,
	sourceX, sourceY int,
	invRxSq, invRySq float64,
) (radialX, radialY float64, hit bool) {
	if !vmath.EllipseContainsPointF(entityX, entityY, sourceX, sourceY, invRxSq, invRySq) {
		return 0, 0, false
	}

	radialX = float64(entityX - sourceX)
	radialY = float64(entityY - sourceY)

	if radialX == 0 && radialY == 0 {
		radialX = 1.0
	}

	return radialX, radialY, true
}

// randSpread returns a uniform value in [-half, half); 0 when disabled
func randSpread(half float64, rng *vmath.FastRand) float64 {
	if half <= 0 || rng == nil {
		return 0
	}
	return (rng.Float64()*2.0 - 1.0) * half
}

// ApplyCollisionImpulseF calculates velocity delta from collision
// Returns impulse vector to ADD to target's current velocity.
// angleVar is in radians and rotates the direction directly; the fixed path's
// RadiansToRotation bridge has no float equivalent.
func ApplyCollisionImpulseF(
	impactorVelX, impactorVelY float64,
	massRatio float64,
	angleVar float64,
	magnitudeMin, magnitudeMax float64,
	rng *vmath.FastRand,
) (impulseX, impulseY float64) {
	dirX, dirY := vmath.Normalize2DF(impactorVelX, impactorVelY)
	if dirX == 0 && dirY == 0 {
		return 0, 0
	}

	if a := randSpread(angleVar, rng); a != 0 {
		dirX, dirY = vmath.RotateVectorF(dirX, dirY, a)
	}

	magnitude := magnitudeMin
	if magnitudeMax > magnitudeMin && rng != nil {
		magnitude += rng.Float64() * (magnitudeMax - magnitudeMin)
	}
	magnitude *= massRatio

	return dirX * magnitude, dirY * magnitude
}

// ApplyOffsetCollisionImpulseF calculates impulse for collision at offset from target anchor
// Combines impactor direction with "push away from hit point" for multi-cell entities
func ApplyOffsetCollisionImpulseF(
	impactorVelX, impactorVelY float64,
	offsetX, offsetY int,
	offsetInfluence float64,
	massRatio float64,
	angleVar float64,
	magnitudeMin, magnitudeMax float64,
	rng *vmath.FastRand,
) (impulseX, impulseY float64) {
	baseX, baseY := vmath.Normalize2DF(impactorVelX, impactorVelY)
	if baseX == 0 && baseY == 0 {
		return 0, 0
	}

	// Offset direction: push away from hit point (negate offset)
	if offsetInfluence > 0 && (offsetX != 0 || offsetY != 0) {
		offDirX, offDirY := vmath.Normalize2DF(float64(-offsetX), float64(-offsetY))

		inv := 1.0 - offsetInfluence
		baseX = baseX*inv + offDirX*offsetInfluence
		baseY = baseY*inv + offDirY*offsetInfluence

		baseX, baseY = vmath.Normalize2DF(baseX, baseY)
	}

	if a := randSpread(angleVar, rng); a != 0 {
		baseX, baseY = vmath.RotateVectorF(baseX, baseY, a)
	}

	magnitude := magnitudeMin
	if magnitudeMax > magnitudeMin && rng != nil {
		magnitude += rng.Float64() * (magnitudeMax - magnitudeMin)
	}
	magnitude *= massRatio

	return baseX * magnitude, baseY * magnitude
}

// ImpulseFromProfileF computes a collision impulse without applying it
func ImpulseFromProfileF(dirX, dirY float64, p *CollisionProfileF, rng *vmath.FastRand) (impulseX, impulseY float64) {
	if dirX == 0 && dirY == 0 {
		dirX = 1.0
	}
	return ApplyCollisionImpulseF(dirX, dirY, p.MassRatio, p.AngleVariance, p.ImpulseMin, p.ImpulseMax, rng)
}
