// Package profile composes engine primitives (pkg/vmath/physics) with game
// tuning (internal/parameter) into the concrete collision, homing and combat
// profiles the systems consume.
//
// Boundary: parameter holds scalars and imports nothing from physics; profile
// holds anything that composes them into an engine struct or a lookup table.
package profile

// Mass is an entity's collision mass in relative units
// Baseline: a single-cell entity is 1.0
type Mass = float64

// Entity masses. Composite masses scale roughly with footprint and role,
// not with cell count.
const (
	MassDust      Mass = 0.1
	MassCursor    Mass = 1.0
	MassCleaner   Mass = 1.0
	MassDrain     Mass = 1.0
	MassSwarm     Mass = 2.0
	MassEye       Mass = 5.0
	MassSnakeBody Mass = 2.0
	MassSnakeHead Mass = 8.0
	MassQuasar    Mass = 10.0
	MassExplosion Mass = 10.0
	// TODO: move to physics, rewire storm system
	// StormSystem folds 2D impulses back into its 3D velocity (see absorbExternalImpulse)
	MassStorm Mass = 100.0

	// MassPylon marks the pylon effectively immovable. It is a soft-collision
	// source only, never a knockback target, and SoftRatioMax is what actually
	// bounds its push. Reserved for future cursor pushback.
	MassPylon Mass = 1000.0
)

// Mass ratio clamp. Below Min the impactor cannot meaningfully move the
// target; above Max the response saturates instead of launching it.
const (
	MassRatioMin = 1.0 / 64.0
	MassRatioMax = 16.0

	// SoftRatioMax bounds inter-species scatter. Soft collision separates
	// stacked entities rather than transferring momentum, so a heavy source
	// must not launch a light target.
	SoftRatioMax = 2.0
)

// softRatio is massRatio under the tighter scatter bound
func softRatio(impactor, target Mass) float64 {
	r := massRatio(impactor, target)
	if r > SoftRatioMax {
		return SoftRatioMax
	}
	return r
}

// massRatio returns impactor/target mass clamped to the usable band
func massRatio(impactor, target Mass) float64 {
	r := impactor / target
	if r < MassRatioMin {
		return MassRatioMin
	}
	if r > MassRatioMax {
		return MassRatioMax
	}
	return r
}
