package physics

import (
	"github.com/lixenwraith/vi-fighter/vmath"
)

// CapSpeed limits the velocity vector magnitude to maxSpeed
// Returns true if velocity was clamped
func CapSpeed(velX, velY *int64, maxSpeed int64) bool {
	magSq := vmath.MagnitudeSq(*velX, *velY)
	maxSq := vmath.Mul(maxSpeed, maxSpeed)

	if magSq > maxSq {
		// Use Scale/Mag ratio to downscale
		mag := vmath.Sqrt(magSq)
		if mag == 0 {
			return false
		}
		scale := vmath.Div(maxSpeed, mag)
		*velX = vmath.Mul(*velX, scale)
		*velY = vmath.Mul(*velY, scale)
		return true
	}
	return false
}