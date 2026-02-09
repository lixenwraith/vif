package physics

import (
	"time"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/vmath"
)

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
	MassRatio        int64         // Impactor/target mass ratio (Q32.32, Scale = equal)
	ImpulseMin       int64         // Minimum impulse magnitude (Q32.32 cells/sec)
	ImpulseMax       int64         // Maximum impulse magnitude (Q32.32 cells/sec)
	AngleVariance    int64         // Random angle spread in Q32.32 radians
	Mode             ImpulseMode   // Additive or Override
	ImmunityDuration time.Duration // Post-collision immunity window
	OffsetInfluence  int64         // Blend factor for offset-based direction (0 = none)
}

// ApplyCollision calculates and applies collision impulse
// dirX, dirY: impact direction (impactor velocity or radial vector)
func ApplyCollision(
	k *core.Kinetic,
	dirX, dirY int64,
	profile *CollisionProfile,
	rng *vmath.FastRand,
) {
	// Zero direction fallback
	if dirX == 0 && dirY == 0 {
		dirX = vmath.Scale
	}

	// Calculate impulse
	impulseX, impulseY := ApplyCollisionImpulse(
		dirX, dirY,
		profile.MassRatio,
		profile.AngleVariance,
		profile.ImpulseMin,
		profile.ImpulseMax,
		rng,
	)

	// Apply based on mode
	switch profile.Mode {
	case ImpulseAdditive:
		ApplyImpulse(k, impulseX, impulseY)
	case ImpulseOverride:
		SetImpulse(k, impulseX, impulseY)
	}

}

// ApplyOffsetCollision calculates collision with offset influence for multi-cell entities
// dirX, dirY: impact direction (impactor velocity or radial vector)
// offsetX, offsetY: hit point offset from anchor in integer cells
func ApplyOffsetCollision(
	k *core.Kinetic,
	dirX, dirY int64,
	offsetX, offsetY int,
	profile *CollisionProfile,
	rng *vmath.FastRand,
) {
	// Zero direction fallback
	if dirX == 0 && dirY == 0 {
		dirX = vmath.Scale
	}

	// Calculate impulse with offset influence
	impulseX, impulseY := ApplyOffsetCollisionImpulse(
		dirX, dirY,
		offsetX, offsetY,
		profile.OffsetInfluence,
		profile.MassRatio,
		profile.AngleVariance,
		profile.ImpulseMin,
		profile.ImpulseMax,
		rng,
	)

	// Apply based on mode
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
	invRxSq, invRySq int64,
) (radialX, radialY int64, hit bool) {
	if !vmath.EllipseContainsPoint(entityX, entityY, sourceX, sourceY, invRxSq, invRySq) {
		return 0, 0, false
	}

	radialX = vmath.FromInt(entityX - sourceX)
	radialY = vmath.FromInt(entityY - sourceY)

	if radialX == 0 && radialY == 0 {
		radialX = vmath.Scale
	}

	return radialX, radialY, true
}

// RadiansToRotation converts Q32.32 radians to rotation units
// Rotation units: Scale = full rotation (2π)
// Usage: rotUnits = Mul(radians, RadiansToRotation)
// Q16.16: const RadiansToRotation int32 = 10430 // ≈ Scale / (2π)
// Q32.32: const RadiansToRotation int64 = 683565275 // ≈ Scale / (2π)
const RadiansToRotation int64 = 683565275 // ≈ Scale / (2π)

// ApplyCollisionImpulse calculates velocity delta from collision
// Returns impulse vector to ADD to target's current velocity
//
// Parameters:
//   - impactorVelX/Y: impacting object's velocity (Q32.32)
//   - massRatio: impactor_mass / target_mass (Q32.32, Scale = equal)
//   - angleVarRad: random angle variance in Q32.32 radians (0 = none)
//   - magnitudeMin/Max: impulse magnitude bounds (Q32.32 cells/sec)
//   - rng: random source (nil = no randomization)
func ApplyCollisionImpulse(
	impactorVelX, impactorVelY int64,
	massRatio int64,
	angleVarRad int64,
	magnitudeMin, magnitudeMax int64,
	rng *vmath.FastRand,
) (impulseX, impulseY int64) {
	// Direction from impactor velocity
	dirX, dirY := vmath.Normalize2D(impactorVelX, impactorVelY)

	// Zero velocity fallback
	if dirX == 0 && dirY == 0 {
		return 0, 0
	}

	// Apply angle variance (convert radians to rotation units)
	if angleVarRad > 0 && rng != nil {
		angleVarRot := vmath.Mul(angleVarRad, RadiansToRotation)
		angleRange := int(angleVarRot) * 2
		if angleRange > 0 {
			randomAngle := int64(rng.Intn(angleRange)) - angleVarRot
			dirX, dirY = vmath.RotateVector(dirX, dirY, randomAngle)
		}
	}

	// Random magnitude in [min, max]
	magnitude := magnitudeMin
	if magnitudeMax > magnitudeMin && rng != nil {
		magRange := int(magnitudeMax - magnitudeMin)
		magnitude += int64(rng.Intn(magRange))
	}

	// Scale by mass ratio
	magnitude = vmath.Mul(magnitude, massRatio)

	return vmath.Mul(dirX, magnitude), vmath.Mul(dirY, magnitude)
}

// ApplyOffsetCollisionImpulse calculates impulse for collision at offset from target anchor
// Combines impactor direction with "push away from hit point" effect for multi-cell entities
//
// Parameters:
//   - impactorVelX/Y: impacting object's velocity (Q32.32)
//   - offsetX/Y: hit position relative to target anchor (integer cells, converted internally)
//   - offsetInfluence: blend factor for offset direction (0 = pure impactor, Scale = equal blend)
//   - massRatio: impactor_mass / target_mass (Q32.32)
//   - angleVarRad: random angle variance in Q32.32 radians
//   - magnitudeMin/Max: impulse magnitude bounds (Q32.32 cells/sec)
//   - rng: random source (nil = no randomization)
func ApplyOffsetCollisionImpulse(
	impactorVelX, impactorVelY int64,
	offsetX, offsetY int,
	offsetInfluence int64,
	massRatio int64,
	angleVarRad int64,
	magnitudeMin, magnitudeMax int64,
	rng *vmath.FastRand,
) (impulseX, impulseY int64) {
	// Base direction from impactor velocity
	baseX, baseY := vmath.Normalize2D(impactorVelX, impactorVelY)
	if baseX == 0 && baseY == 0 {
		return 0, 0
	}

	// Offset direction: push away from hit point (negate offset)
	// Convert cell offset to Q32.32 for normalization
	if offsetInfluence > 0 && (offsetX != 0 || offsetY != 0) {
		offX := vmath.FromInt(-offsetX)
		offY := vmath.FromInt(-offsetY)
		offDirX, offDirY := vmath.Normalize2D(offX, offY)

		// Blend: base * (1 - influence) + offset * influence
		invInfluence := vmath.Scale - offsetInfluence
		baseX = vmath.Mul(baseX, invInfluence) + vmath.Mul(offDirX, offsetInfluence)
		baseY = vmath.Mul(baseY, invInfluence) + vmath.Mul(offDirY, offsetInfluence)

		// Re-normalize blended direction
		baseX, baseY = vmath.Normalize2D(baseX, baseY)
	}

	// Apply angle variance
	if angleVarRad > 0 && rng != nil {
		angleVarRot := vmath.Mul(angleVarRad, RadiansToRotation)
		angleRange := int(angleVarRot) * 2
		if angleRange > 0 {
			randomAngle := int64(rng.Intn(angleRange)) - angleVarRot
			baseX, baseY = vmath.RotateVector(baseX, baseY, randomAngle)
		}
	}

	// Random magnitude in [min, max]
	magnitude := magnitudeMin
	if magnitudeMax > magnitudeMin && rng != nil {
		magRange := int(magnitudeMax - magnitudeMin)
		magnitude += int64(rng.Intn(magRange))
	}

	// Scale by mass ratio
	magnitude = vmath.Mul(magnitude, massRatio)

	return vmath.Mul(baseX, magnitude), vmath.Mul(baseY, magnitude)
}