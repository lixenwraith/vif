package physics

import (
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// Integrate performs physics integration: v = v + a*dt; p = p + v*dt
func Integrate(k *core.Kinetic, dt int64) (x, y int) {
	k.VelX += vmath.Mul(k.AccelX, dt)
	k.VelY += vmath.Mul(k.AccelY, dt)
	k.PreciseX += vmath.Mul(k.VelX, dt)
	k.PreciseY += vmath.Mul(k.VelY, dt)
	return vmath.ToInt(k.PreciseX), vmath.ToInt(k.PreciseY)
}

// ApplyImpulse adds velocity delta (momentum transfer)
func ApplyImpulse(k *core.Kinetic, vx, vy int64) {
	k.VelX += vx
	k.VelY += vy
}

// SetImpulse overrides velocity (hard redirect/stun)
func SetImpulse(k *core.Kinetic, vx, vy int64) {
	k.VelX = vx
	k.VelY = vy
}

// ReflectBoundsX handles horizontal boundary collision, returns true if reflection occurred
// Clamps to centered position within valid cell range [minX, maxX)
func ReflectBoundsX(k *core.Kinetic, minX, maxX int) bool {
	x := vmath.ToInt(k.PreciseX)
	if x < minX {
		k.PreciseX = vmath.FromInt(minX) + vmath.CellCenter
		k.VelX = -k.VelX
		return true
	}
	if x >= maxX {
		k.PreciseX = vmath.FromInt(maxX-1) + vmath.CellCenter
		k.VelX = -k.VelX
		return true
	}
	return false
}

// ReflectBoundsY handles vertical boundary collision, returns true if reflection occurred
// Clamps to centered position within valid cell range [minY, maxY)
func ReflectBoundsY(k *core.Kinetic, minY, maxY int) bool {
	y := vmath.ToInt(k.PreciseY)
	if y < minY {
		k.PreciseY = vmath.FromInt(minY) + vmath.CellCenter
		k.VelY = -k.VelY
		return true
	}
	if y >= maxY {
		k.PreciseY = vmath.FromInt(maxY-1) + vmath.CellCenter
		k.VelY = -k.VelY
		return true
	}
	return false
}

// ReflectBounds handles both axis boundary collisions, returns true if any reflection occurred
func ReflectBounds(k *core.Kinetic, width, height int) bool {
	rx := ReflectBoundsX(k, 0, width)
	ry := ReflectBoundsY(k, 0, height)
	return rx || ry
}

// GridPos returns current integer grid position
func GridPos(k *core.Kinetic) (x, y int) {
	return vmath.ToInt(k.PreciseX), vmath.ToInt(k.PreciseY)
}

// SetGridPos sets precise position from integer grid coordinates (centered)
func SetGridPos(k *core.Kinetic, x, y int) {
	k.PreciseX, k.PreciseY = vmath.CenteredFromGrid(x, y)
}