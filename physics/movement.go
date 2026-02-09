package physics

import (
	"math"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// CapSpeedVal limits velocity magnitude, returns clamped values
// Optimization: Pass-by-value enables inlining, avoids pointer chase
func CapSpeedVal(velX, velY, maxSpeed int64) (int64, int64) {
	fvx, fvy := float64(velX), float64(velY)
	magSq := fvx*fvx + fvy*fvy
	fMax := float64(maxSpeed)
	maxSq := fMax * fMax

	if magSq <= maxSq {
		return velX, velY
	}

	scale := fMax / math.Sqrt(magSq)
	return int64(fvx * scale), int64(fvy * scale)
}

// WallQueryFunc returns true if the footprint at the given top-left coordinates is blocked
type WallQueryFunc func(topLeftX, topLeftY int) bool

// IntegrateWithBounce performs physics integration with sub-stepping and restitution.
//
// Parameters:
//   - k: Kinetic component (position updated in-place)
//   - dtFixed: Delta time in Q32.32
//   - headerOffsetX, headerOffsetY: Offset from Kinetic Position (Header) to Top-Left of collision box
//   - boundMinX, boundMaxX, boundMinY, boundMaxY: Valid screen coordinate ranges
//   - wallRestitution: Velocity retained after bounce (Scale = 1.0/Elastic, 0 = Sticky)
//   - checkWall: Callback to check collision
//
// Returns:
//   - finalGridX, finalGridY: The integer grid coordinates after integration
//   - hitWall: True if any wall or boundary collision occurred
func IntegrateWithBounce(
	k *core.Kinetic,
	dtFixed int64,
	headerOffsetX, headerOffsetY int,
	boundMinX, boundMaxX int,
	boundMinY, boundMaxY int,
	wallRestitution int64,
	checkWall WallQueryFunc,
) (int, int, bool) {
	// 1. Calculate step count to prevent tunneling
	potentialDistX := vmath.Abs(vmath.Mul(k.VelX, dtFixed))
	potentialDistY := vmath.Abs(vmath.Mul(k.VelY, dtFixed))
	maxDist := potentialDistX
	if potentialDistY > maxDist {
		maxDist = potentialDistY
	}

	stepLimit := vmath.FromFloat(0.45)
	steps := 1
	if maxDist > stepLimit {
		steps = int(vmath.Div(maxDist, stepLimit)) + 1
	}
	if steps > 20 {
		steps = 20
	}

	dtStep := dtFixed / int64(steps)
	hitAny := false

	// 2. Sub-step integration
	for i := 0; i < steps; i++ {
		// --- X Axis Movement ---
		startPreciseX := k.PreciseX
		k.PreciseX += vmath.Mul(k.VelX, dtStep)

		// Check Screen Bounds X
		if ReflectBoundsX(k, boundMinX, boundMaxX) {
			hitAny = true
			// Apply restitution on reflection (ReflectBoundsX only flips sign)
			// We effectively applied -1.0, now scale by restitution magnitude
			// Since sign is already flipped, we just multiply by positive restitution
			k.VelX = vmath.Mul(k.VelX, wallRestitution)
		} else {
			// Check Wall X
			gridX := vmath.ToInt(k.PreciseX)
			gridY := vmath.ToInt(k.PreciseY)

			if checkWall(gridX-headerOffsetX, gridY-headerOffsetY) {
				hitAny = true
				k.PreciseX = startPreciseX
				k.VelX = -vmath.Mul(k.VelX, wallRestitution)
			}
		}

		// --- Y Axis Movement ---
		startPreciseY := k.PreciseY
		k.PreciseY += vmath.Mul(k.VelY, dtStep)

		// Check Screen Bounds Y
		if ReflectBoundsY(k, boundMinY, boundMaxY) {
			hitAny = true
			k.VelY = vmath.Mul(k.VelY, wallRestitution)
		} else {
			// Check Wall Y
			gridX := vmath.ToInt(k.PreciseX)
			gridY := vmath.ToInt(k.PreciseY)

			if checkWall(gridX-headerOffsetX, gridY-headerOffsetY) {
				hitAny = true
				k.PreciseY = startPreciseY
				k.VelY = -vmath.Mul(k.VelY, wallRestitution)
			}
		}
	}

	return vmath.ToInt(k.PreciseX), vmath.ToInt(k.PreciseY), hitAny
}