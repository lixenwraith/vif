package component

import (
	"github.com/lixenwraith/vi-fighter/vmath"
)

// KineticState provides a reusable kinematic container for entities requiring sub-pixel motion
// Uses Q16.16 fixed-point arithmetic for deterministic integration and high-performance physics updates
// Does not track discrete grid history; logic-layer latching
// (e.g., cell-entry detection) is the responsibility of the calling component
type KineticState struct {
	// PreciseX and PreciseY are sub-pixel coordinates in Q16.16 format
	PreciseX, PreciseY int32
	// VelX and VelY represent velocity in cells per second (Q16.16)
	VelX, VelY int32
	// AccelX and AccelY represent acceleration in cells per second squared (Q16.16)
	AccelX, AccelY int32
}

// Physics Integration: v = v + a*dt; p = p + v*dt
func (k *KineticState) Integrate(dt int32) (x, y int) {
	k.VelX += vmath.Mul(k.AccelX, dt)
	k.VelY += vmath.Mul(k.AccelY, dt)
	k.PreciseX += vmath.Mul(k.VelX, dt)
	k.PreciseY += vmath.Mul(k.VelY, dt)
	return vmath.ToInt(k.PreciseX), vmath.ToInt(k.PreciseY)
}