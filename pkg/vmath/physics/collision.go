package physics

import "github.com/lixenwraith/vi-fighter/pkg/vmath"

// ImpulseMode defines how impulse is applied to velocity
type ImpulseMode uint8

const (
	// ImpulseAdditive adds impulse to existing velocity (standard physics)
	ImpulseAdditive ImpulseMode = iota
	// ImpulseOverride replaces velocity with impulse (stun/hard redirect)
	ImpulseOverride
)

// CollisionProfile defines collision interaction parameters
// Profiles are typically pre-defined as package variables for zero allocation
type CollisionProfile struct {
	MassRatio       float64     // Impactor/target mass ratio (1.0 = equal)
	ImpulseMin      float64     // Minimum impulse magnitude (cells/sec)
	ImpulseMax      float64     // Maximum impulse magnitude (cells/sec)
	AngleVariance   float64     // Random angle spread (radians)
	Mode            ImpulseMode // Additive or Override
	OffsetInfluence float64     // Blend factor for offset-based direction (0 = none, 1 = pure offset)
}

// ApplyCollision calculates and applies collision impulse
// dirX, dirY: impact direction (impactor velocity or radial vector)
func ApplyCollision(k *Kinetic, dirX, dirY float64, profile *CollisionProfile, rng *vmath.FastRand) {
	if dirX == 0 && dirY == 0 {
		dirX = 1.0
	}

	impulseX, impulseY := ApplyCollisionImpulse(
		dirX, dirY,
		profile.MassRatio,
		profile.AngleVariance,
		profile.ImpulseMin, profile.ImpulseMax,
		rng,
	)

	switch profile.Mode {
	case ImpulseAdditive:
		ApplyImpulse(k, impulseX, impulseY)
	case ImpulseOverride:
		SetImpulse(k, impulseX, impulseY)
	}
}

// ApplyOffsetCollision calculates collision with offset influence for multi-cell entities
// offsetX, offsetY: hit point offset from anchor in integer cells
func ApplyOffsetCollision(
	k *Kinetic,
	dirX, dirY float64,
	offsetX, offsetY int,
	profile *CollisionProfile,
	rng *vmath.FastRand,
) {
	if dirX == 0 && dirY == 0 {
		dirX = 1.0
	}

	impulseX, impulseY := ApplyOffsetCollisionImpulse(
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
		ApplyImpulse(k, impulseX, impulseY)
	case ImpulseOverride:
		SetImpulse(k, impulseX, impulseY)
	}
}

// CheckSoftCollision tests ellipse containment and computes radial direction
// Returns (radialX, radialY, true) if collision detected, (0, 0, false) otherwise
func CheckSoftCollision(
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

// ApplyCollisionImpulse calculates velocity delta from collision
// Returns impulse vector to ADD to target's current velocity; angleVar is in radians
//
// Parameters:
//   - impactorVelX/Y: impacting object's velocity (cells/sec)
//   - massRatio: impactor_mass / target_mass (1.0 = equal)
//   - angleVar: random angle half-spread in radians (0 = none)
//   - magnitudeMin/Max: impulse magnitude bounds (cells/sec)
//   - rng: random source (nil = no randomization)
func ApplyCollisionImpulse(
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

// ApplyOffsetCollisionImpulse calculates impulse for collision at offset from target anchor
// Combines impactor direction with "push away from hit point" for multi-cell entities
func ApplyOffsetCollisionImpulse(
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

// ImpulseFromProfile computes a collision impulse without applying it.
// For callers that accumulate or defer application.
func ImpulseFromProfile(dirX, dirY float64, p *CollisionProfile, rng *vmath.FastRand) (impulseX, impulseY float64) {
	if dirX == 0 && dirY == 0 {
		dirX = 1.0
	}
	return ApplyCollisionImpulse(dirX, dirY, p.MassRatio, p.AngleVariance, p.ImpulseMin, p.ImpulseMax, rng)
}
