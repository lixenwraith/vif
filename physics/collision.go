package physics

import (
	"time"

	"github.com/lixenwraith/vi-fighter/component"
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

// ApplyCollision calculates and applies collision impulse to kinetic state
// Returns true if impulse was applied (false if immune or zero impulse)
// dirX, dirY: impact direction in Q32.32 (typically impactor velocity or radial vector)
func ApplyCollision(
	kinetic *component.Kinetic,
	dirX, dirY int64,
	profile *CollisionProfile,
	rng *vmath.FastRand,
) bool {
	// Zero direction fallback
	if dirX == 0 && dirY == 0 {
		dirX = vmath.Scale
	}

	// Calculate impulse
	impulseX, impulseY := vmath.ApplyCollisionImpulse(
		dirX, dirY,
		profile.MassRatio,
		profile.AngleVariance,
		profile.ImpulseMin,
		profile.ImpulseMax,
		rng,
	)

	if impulseX == 0 && impulseY == 0 {
		return false
	}

	// Apply based on mode
	switch profile.Mode {
	case ImpulseAdditive:
		kinetic.ApplyImpulse(impulseX, impulseY)
	case ImpulseOverride:
		kinetic.SetImpulse(impulseX, impulseY)
	}

	return true
}

// ApplyOffsetCollision calculates collision with offset influence for multi-cell entities
// offsetX, offsetY: hit point offset from anchor in integer cells
func ApplyOffsetCollision(
	kinetic *component.Kinetic,
	dirX, dirY int64,
	offsetX, offsetY int,
	profile *CollisionProfile,
	rng *vmath.FastRand,
) bool {
	// Zero direction fallback
	if dirX == 0 && dirY == 0 {
		dirX = vmath.Scale
	}

	// Calculate impulse with offset influence
	impulseX, impulseY := vmath.ApplyOffsetCollisionImpulse(
		dirX, dirY,
		offsetX, offsetY,
		profile.OffsetInfluence,
		profile.MassRatio,
		profile.AngleVariance,
		profile.ImpulseMin,
		profile.ImpulseMax,
		rng,
	)

	if impulseX == 0 && impulseY == 0 {
		return false
	}

	// Apply based on mode
	switch profile.Mode {
	case ImpulseAdditive:
		kinetic.ApplyImpulse(impulseX, impulseY)
	case ImpulseOverride:
		kinetic.SetImpulse(impulseX, impulseY)
	}

	return true
}