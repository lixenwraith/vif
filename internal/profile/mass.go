// Package profile composes engine primitives (pkg/vmath/physics) with game
// tuning (internal/parameter) into the concrete collision, homing and combat
// profiles the systems consume.
//
// Boundary: parameter holds scalars and their fixed-point duals and imports
// nothing from physics; profile holds anything that composes them into an
// engine struct or a lookup table.
package profile

import "github.com/lixenwraith/vi-fighter/pkg/vmath"

// Mass is an entity's collision mass in relative units (Q32.32)
// Baseline: a single-cell entity is vmath.Scale (1.0)
type Mass = int64

// Entity masses. Composite masses scale roughly with footprint and role,
// not with cell count.
const (
	MassDust      Mass = vmath.Scale / 10
	MassCursor    Mass = vmath.Scale
	MassCleaner   Mass = vmath.Scale
	MassDrain     Mass = vmath.Scale
	MassSwarm     Mass = vmath.Scale * 2
	MassEye       Mass = vmath.Scale * 5
	MassSnakeBody Mass = vmath.Scale * 2
	MassSnakeHead Mass = vmath.Scale * 8
	MassQuasar    Mass = vmath.Scale * 10
	MassExplosion Mass = vmath.Scale * 10
	// TODO: move to physics, rewire storm system
	// StormSystem folds 2D impulses back into its 3D velocity (see absorbExternalImpulse)
	MassStorm Mass = vmath.Scale * 100

	// MassPylon marks the pylon effectively immovable. It is a soft-collision
	// source only, never a knockback target, and SoftRatioMax is what actually
	// bounds its push. Reserved for future cursor pushback.
	MassPylon Mass = vmath.Scale * 1000
)

// Mass ratio clamp. Below Min the impactor cannot meaningfully move the
// target; above Max the response saturates instead of launching it.
const (
	MassRatioMin = vmath.Scale / 64
	MassRatioMax = vmath.Scale * 16

	// SoftRatioMax bounds inter-species scatter. Soft collision separates
	// stacked entities rather than transferring momentum, so a heavy source
	// must not launch a light target. Replaces the fourth-root dampening
	// previously applied in SoftCollisionSystem.
	SoftRatioMax = vmath.Scale * 2
)

// softRatio is massRatio under the tighter scatter bound
func softRatio(impactor, target Mass) int64 {
	r := massRatio(impactor, target)
	if r > SoftRatioMax {
		return SoftRatioMax
	}
	return r
}

// massRatio returns impactor/target mass clamped to the usable band
func massRatio(impactor, target Mass) int64 {
	r := vmath.Div(impactor, target)
	if r < MassRatioMin {
		return MassRatioMin
	}
	if r > MassRatioMax {
		return MassRatioMax
	}
	return r
}
