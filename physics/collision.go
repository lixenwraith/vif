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
	MassRatio        int32         // Impactor/target mass ratio (Q16.16, Scale = equal)
	ImpulseMin       int32         // Minimum impulse magnitude (Q16.16 cells/sec)
	ImpulseMax       int32         // Maximum impulse magnitude (Q16.16 cells/sec)
	AngleVariance    int32         // Random angle spread in Q16.16 radians
	Mode             ImpulseMode   // Additive or Override
	ImmunityDuration time.Duration // Post-collision immunity window
	OffsetInfluence  int32         // Blend factor for offset-based direction (0 = none)
}

// ApplyCollision calculates and applies collision impulse to kinetic state
// Returns true if impulse was applied (false if immune or zero impulse)
// dirX, dirY: impact direction in Q16.16 (typically impactor velocity or radial vector)
func ApplyCollision(
	kinetic *component.KineticState,
	dirX, dirY int32,
	profile *CollisionProfile,
	rng *vmath.FastRand,
	now time.Time,
) bool {
	// Immunity check
	if kinetic.IsImmune(now) {
		return false
	}

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

	// Set immunity
	if profile.ImmunityDuration > 0 {
		kinetic.SetImmunity(now.Add(profile.ImmunityDuration))
	}

	return true
}

// ApplyOffsetCollision calculates collision with offset influence for multi-cell entities
// offsetX, offsetY: hit point offset from anchor in integer cells
func ApplyOffsetCollision(
	kinetic *component.KineticState,
	dirX, dirY int32,
	offsetX, offsetY int,
	profile *CollisionProfile,
	rng *vmath.FastRand,
	now time.Time,
) bool {
	// Immunity check
	if kinetic.IsImmune(now) {
		return false
	}

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

	// Set immunity
	if profile.ImmunityDuration > 0 {
		kinetic.SetImmunity(now.Add(profile.ImmunityDuration))
	}

	return true
}