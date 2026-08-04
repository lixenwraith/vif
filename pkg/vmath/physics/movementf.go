package physics

import (
	"math"

	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// CapSpeedF limits velocity magnitude, returns clamped values
func CapSpeedF(velX, velY, maxSpeed float64) (float64, float64) {
	magSq := velX*velX + velY*velY
	if magSq <= maxSpeed*maxSpeed {
		return velX, velY
	}
	scale := maxSpeed / math.Sqrt(magSq)
	return velX * scale, velY * scale
}

// bounceStepLimit is the per-sub-step distance cap (cells) that prevents tunneling
const bounceStepLimit = 0.45

// bounceMaxSteps caps sub-stepping cost at extreme velocities
const bounceMaxSteps = 20

// IntegrateWithBounceF performs physics integration with sub-stepping and restitution.
// Parameters and return values mirror IntegrateWithBounce; wallRestitution is 1.0 for
// elastic, 0 for sticky.
func IntegrateWithBounceF(
	k *KineticF,
	dt float64,
	headerOffsetX, headerOffsetY int,
	boundMinX, boundMaxX int,
	boundMinY, boundMaxY int,
	wallRestitution float64,
	checkWall WallQueryFunc,
) (int, int, bool) {
	// 1. Calculate step count to prevent tunneling
	maxDist := math.Max(math.Abs(k.VelX*dt), math.Abs(k.VelY*dt))

	steps := 1
	if maxDist > bounceStepLimit {
		steps = int(maxDist/bounceStepLimit) + 1
	}
	if steps > bounceMaxSteps {
		steps = bounceMaxSteps
	}

	dtStep := dt / float64(steps)
	hitAny := false

	// 2. Sub-step integration
	for range steps {
		// --- X Axis Movement ---
		startPreciseX := k.PreciseX
		k.PreciseX += k.VelX * dtStep

		if ReflectBoundsXF(k, boundMinX, boundMaxX) {
			hitAny = true
			// ReflectBoundsXF only flips the sign; scale by restitution magnitude
			k.VelX *= wallRestitution
		} else {
			p := vmath.PointAtF(k.PreciseX, k.PreciseY)
			if checkWall(p.X-headerOffsetX, p.Y-headerOffsetY) {
				hitAny = true
				k.PreciseX = startPreciseX
				k.VelX = -k.VelX * wallRestitution
			}
		}

		// --- Y Axis Movement ---
		startPreciseY := k.PreciseY
		k.PreciseY += k.VelY * dtStep

		if ReflectBoundsYF(k, boundMinY, boundMaxY) {
			hitAny = true
			k.VelY *= wallRestitution
		} else {
			p := vmath.PointAtF(k.PreciseX, k.PreciseY)
			if checkWall(p.X-headerOffsetX, p.Y-headerOffsetY) {
				hitAny = true
				k.PreciseY = startPreciseY
				k.VelY = -k.VelY * wallRestitution
			}
		}
	}

	p := vmath.PointAtF(k.PreciseX, k.PreciseY)
	return p.X, p.Y, hitAny
}
