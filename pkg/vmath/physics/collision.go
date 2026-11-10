package physics

import (
	"math"

	"github.com/lixenwraith/vif/pkg/vmath"
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

	ApplyImpulseMode(k, impulseX, impulseY, profile.Mode)
}

// ApplyImpulseMode applies a computed impulse in a profile's mode
func ApplyImpulseMode(k *Kinetic, impulseX, impulseY float64, mode ImpulseMode) {
	switch mode {
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
	ApplyImpulseMode(k, impulseX, impulseY, profile.Mode)
}

// CheckSoftCollision tests ellipse containment and computes radial direction
// Returns (radialX, radialY, true) if collision detected, (0, 0, false) otherwise
func CheckSoftCollision(
	entityX, entityY int,
	sourceX, sourceY int,
	invRxSq, invRySq float64,
) (radialX, radialY float64, hit bool) {
	if !vmath.EllipseContainsPoint(entityX, entityY, sourceX, sourceY, invRxSq, invRySq) {
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

// impulseMagnitude draws an impulse's size between its bounds, the lower without
// a source, and scales it by the mass ratio
func impulseMagnitude(magnitudeMin, magnitudeMax, massRatio float64, rng *vmath.FastRand) float64 {
	magnitude := magnitudeMin
	if magnitudeMax > magnitudeMin && rng != nil {
		magnitude += rng.Float64() * (magnitudeMax - magnitudeMin)
	}
	return magnitude * massRatio
}

// ApplyCollisionImpulse returns the impulse to add to a target's velocity along
// the impactor's (cells/sec): a size between magnitudeMin and magnitudeMax
// scaled by massRatio, impactor over target, its direction turned within
// angleVar radians either way. A nil rng draws neither, the size its minimum.
func ApplyCollisionImpulse(
	impactorVelX, impactorVelY float64,
	massRatio float64,
	angleVar float64,
	magnitudeMin, magnitudeMax float64,
	rng *vmath.FastRand,
) (impulseX, impulseY float64) {
	dirX, dirY := vmath.Normalize2D(impactorVelX, impactorVelY)
	if dirX == 0 && dirY == 0 {
		return 0, 0
	}

	if a := randSpread(angleVar, rng); a != 0 {
		dirX, dirY = vmath.RotateVector(dirX, dirY, a)
	}

	magnitude := impulseMagnitude(magnitudeMin, magnitudeMax, massRatio, rng)
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
	baseX, baseY := vmath.Normalize2D(impactorVelX, impactorVelY)
	if baseX == 0 && baseY == 0 {
		return 0, 0
	}

	// Offset direction: push away from hit point (negate offset)
	if offsetInfluence > 0 && (offsetX != 0 || offsetY != 0) {
		offDirX, offDirY := vmath.Normalize2D(float64(-offsetX), float64(-offsetY))

		inv := 1.0 - offsetInfluence
		baseX = baseX*inv + offDirX*offsetInfluence
		baseY = baseY*inv + offDirY*offsetInfluence

		baseX, baseY = vmath.Normalize2D(baseX, baseY)
	}

	if a := randSpread(angleVar, rng); a != 0 {
		baseX, baseY = vmath.RotateVector(baseX, baseY, a)
	}

	magnitude := impulseMagnitude(magnitudeMin, magnitudeMax, massRatio, rng)
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

// ApplyCollision3D calculates and applies a collision impulse along dir, the
// impact direction in a volume
func ApplyCollision3D(k *Kinetic3D, dir vmath.Vec3, profile *CollisionProfile, rng *vmath.FastRand) {
	ApplyImpulseMode3D(k, ImpulseFromProfile3D(dir, profile, rng), profile.Mode)
}

// ApplyImpulseMode3D applies a computed impulse in a profile's mode
func ApplyImpulseMode3D(k *Kinetic3D, impulse vmath.Vec3, mode ImpulseMode) {
	switch mode {
	case ImpulseAdditive:
		k.Vel = vmath.V3Add(k.Vel, impulse)
	case ImpulseOverride:
		k.Vel = impulse
	}
}

// ApplyOffsetCollision3D is ApplyOffsetCollision in a volume, offset the hit
// point's cells from the anchor
func ApplyOffsetCollision3D(
	k *Kinetic3D,
	dir vmath.Vec3,
	offset vmath.Point3,
	profile *CollisionProfile,
	rng *vmath.FastRand,
) {
	if dir == (vmath.Vec3{}) {
		dir.X = 1.0
	}
	ApplyImpulseMode3D(k, ApplyOffsetCollisionImpulse3D(
		dir, offset,
		profile.OffsetInfluence,
		profile.MassRatio,
		profile.AngleVariance,
		profile.ImpulseMin, profile.ImpulseMax,
		rng,
	), profile.Mode)
}

// CheckSoftCollision3D tests ellipsoid containment and computes the radial
// direction from source to entity, along x where the two share a cell
func CheckSoftCollision3D(entity, source vmath.Point3, invRadiiSq vmath.Vec3) (radial vmath.Vec3, hit bool) {
	if !vmath.EllipsoidContainsPoint(entity, source, invRadiiSq) {
		return vmath.Vec3{}, false
	}
	d := entity.Sub(source)
	if d == (vmath.Point3{}) {
		d.X = 1
	}
	return vmath.Vec3{X: float64(d.X), Y: float64(d.Y), Z: float64(d.Z)}, true
}

// spread3D turns a unit direction by an angle within half about an axis drawn
// from all those square to it: a cone, of which the flat spread is a slice
func spread3D(dir vmath.Vec3, half float64, rng *vmath.FastRand) vmath.Vec3 {
	angle := randSpread(half, rng)
	if angle == 0 {
		return dir
	}
	// Any vector off the direction crosses it into one square to it
	off := vmath.Vec3{Z: 1}
	if math.Abs(dir.Z) > 0.9 {
		off = vmath.Vec3{X: 1}
	}
	axis := vmath.V3Rotate(vmath.V3Normalize(vmath.V3Cross(dir, off)), dir, rng.Float64()*vmath.TwoPi)
	return vmath.V3Rotate(dir, axis, angle)
}

// ApplyCollisionImpulse3D is ApplyCollisionImpulse in a volume: the impulse to
// add to a target's velocity, its direction spread within angleVar round the
// impactor's
func ApplyCollisionImpulse3D(
	impactorVel vmath.Vec3,
	massRatio float64,
	angleVar float64,
	magnitudeMin, magnitudeMax float64,
	rng *vmath.FastRand,
) vmath.Vec3 {
	dir := vmath.V3Normalize(impactorVel)
	if dir == (vmath.Vec3{}) {
		return dir
	}
	dir = spread3D(dir, angleVar, rng)
	return vmath.V3Scale(dir, impulseMagnitude(magnitudeMin, magnitudeMax, massRatio, rng))
}

// ApplyOffsetCollisionImpulse3D is ApplyOffsetCollisionImpulse in a volume: the
// impactor's direction blended with the push away from the hit point
func ApplyOffsetCollisionImpulse3D(
	impactorVel vmath.Vec3,
	offset vmath.Point3,
	offsetInfluence float64,
	massRatio float64,
	angleVar float64,
	magnitudeMin, magnitudeMax float64,
	rng *vmath.FastRand,
) vmath.Vec3 {
	base := vmath.V3Normalize(impactorVel)
	if base == (vmath.Vec3{}) {
		return base
	}
	if offsetInfluence > 0 && offset != (vmath.Point3{}) {
		away := vmath.V3Normalize(vmath.Vec3{X: float64(-offset.X), Y: float64(-offset.Y), Z: float64(-offset.Z)})
		base = vmath.V3Normalize(vmath.V3Lerp(base, away, offsetInfluence))
	}
	base = spread3D(base, angleVar, rng)
	return vmath.V3Scale(base, impulseMagnitude(magnitudeMin, magnitudeMax, massRatio, rng))
}

// ImpulseFromProfile3D computes a collision impulse without applying it, along x
// for no direction
func ImpulseFromProfile3D(dir vmath.Vec3, p *CollisionProfile, rng *vmath.FastRand) vmath.Vec3 {
	if dir == (vmath.Vec3{}) {
		dir.X = 1.0
	}
	return ApplyCollisionImpulse3D(dir, p.MassRatio, p.AngleVariance, p.ImpulseMin, p.ImpulseMax, rng)
}
