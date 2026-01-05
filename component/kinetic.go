package component

import (
	"time"

	"github.com/lixenwraith/vi-fighter/vmath"
)

// KineticState provides a reusable kinematic container for entities requiring sub-pixel motion
// Uses Q32.32 fixed-point arithmetic for deterministic integration and high-performance physics updates
type KineticState struct {
	// PreciseX and PreciseY are sub-pixel coordinates in Q32.32 format
	PreciseX, PreciseY int64
	// VelX and VelY represent velocity in cells per second (Q32.32)
	VelX, VelY int64
	// AccelX and AccelY represent acceleration in cells per second squared (Q32.32)
	AccelX, AccelY int64

	// DeflectUntil is immunity window end time for collision knockback
	// Zero value means no active immunity
	DeflectUntil time.Time
}

// Integrate performs physics integration: v = v + a*dt; p = p + v*dt
func (k *KineticState) Integrate(dt int64) (x, y int) {
	k.VelX += vmath.Mul(k.AccelX, dt)
	k.VelY += vmath.Mul(k.AccelY, dt)
	k.PreciseX += vmath.Mul(k.VelX, dt)
	k.PreciseY += vmath.Mul(k.VelY, dt)
	return vmath.ToInt(k.PreciseX), vmath.ToInt(k.PreciseY)
}

// ApplyImpulse adds velocity delta (momentum transfer)
func (k *KineticState) ApplyImpulse(vx, vy int64) {
	k.VelX += vx
	k.VelY += vy
}

// SetImpulse overrides velocity (hard redirect/stun)
func (k *KineticState) SetImpulse(vx, vy int64) {
	k.VelX = vx
	k.VelY = vy
}

// IsImmune returns true if currently within deflection immunity window
func (k *KineticState) IsImmune(now time.Time) bool {
	return !k.DeflectUntil.IsZero() && now.Before(k.DeflectUntil)
}

// SetImmunity sets deflection immunity window
func (k *KineticState) SetImmunity(until time.Time) {
	k.DeflectUntil = until
}

// ClearImmunity removes deflection immunity
func (k *KineticState) ClearImmunity() {
	k.DeflectUntil = time.Time{}
}

// ReflectBoundsX handles horizontal boundary collision
// Returns true if reflection occurred
func (k *KineticState) ReflectBoundsX(minX, maxX int) bool {
	x := vmath.ToInt(k.PreciseX)
	if x < minX {
		k.PreciseX = vmath.FromInt(minX)
		k.VelX = -k.VelX
		return true
	}
	if x >= maxX {
		k.PreciseX = vmath.FromInt(maxX - 1)
		k.VelX = -k.VelX
		return true
	}
	return false
}

// ReflectBoundsY handles vertical boundary collision
// Returns true if reflection occurred
func (k *KineticState) ReflectBoundsY(minY, maxY int) bool {
	y := vmath.ToInt(k.PreciseY)
	if y < minY {
		k.PreciseY = vmath.FromInt(minY)
		k.VelY = -k.VelY
		return true
	}
	if y >= maxY {
		k.PreciseY = vmath.FromInt(maxY - 1)
		k.VelY = -k.VelY
		return true
	}
	return false
}

// ReflectBounds handles both axis boundary collisions
// Returns true if any reflection occurred
func (k *KineticState) ReflectBounds(width, height int) bool {
	rx := k.ReflectBoundsX(0, width)
	ry := k.ReflectBoundsY(0, height)
	return rx || ry
}

// GridPos returns current integer grid position
func (k *KineticState) GridPos() (x, y int) {
	return vmath.ToInt(k.PreciseX), vmath.ToInt(k.PreciseY)
}

// SetGridPos sets precise position from integer grid coordinates
func (k *KineticState) SetGridPos(x, y int) {
	k.PreciseX = vmath.FromInt(x)
	k.PreciseY = vmath.FromInt(y)
}