package physics

import "github.com/lixenwraith/vi-fighter/pkg/vmath"

// Kinetic is a 2D point-mass state in float64.
// Position is a sub-cell coordinate in cells; grid cells are vmath.Point.
type Kinetic struct {
	// PreciseX and PreciseY are sub-cell coordinates (cells)
	PreciseX, PreciseY float64
	// VelX and VelY are velocity in cells per second
	VelX, VelY float64
	// AccelX and AccelY are acceleration in cells per second squared
	AccelX, AccelY float64
}

// Integrate performs physics integration: v = v + a*dt; p = p + v*dt
func Integrate(k *Kinetic, dt float64) (x, y int) {
	k.VelX += k.AccelX * dt
	k.VelY += k.AccelY * dt
	k.PreciseX += k.VelX * dt
	k.PreciseY += k.VelY * dt
	p := vmath.PointAtF(k.PreciseX, k.PreciseY)
	return p.X, p.Y
}

// ApplyImpulse adds velocity delta (momentum transfer)
func ApplyImpulse(k *Kinetic, vx, vy float64) {
	k.VelX += vx
	k.VelY += vy
}

// SetImpulse overrides velocity (hard redirect/stun)
func SetImpulse(k *Kinetic, vx, vy float64) {
	k.VelX = vx
	k.VelY = vy
}

// ReflectBoundsX handles horizontal boundary collision, returns true if reflection occurred
// Clamps to centered position within valid cell range [minX, maxX)
func ReflectBoundsX(k *Kinetic, minX, maxX int) bool {
	p := vmath.PointAtF(k.PreciseX, k.PreciseY)
	if p.X < minX {
		k.PreciseX, _ = (vmath.Point{X: minX}).CenterF()
		k.VelX = -k.VelX
		return true
	}
	if p.X >= maxX {
		k.PreciseX, _ = (vmath.Point{X: maxX - 1}).CenterF()
		k.VelX = -k.VelX
		return true
	}
	return false
}

// ReflectBoundsY handles vertical boundary collision, returns true if reflection occurred
func ReflectBoundsY(k *Kinetic, minY, maxY int) bool {
	p := vmath.PointAtF(k.PreciseX, k.PreciseY)
	if p.Y < minY {
		_, k.PreciseY = (vmath.Point{Y: minY}).CenterF()
		k.VelY = -k.VelY
		return true
	}
	if p.Y >= maxY {
		_, k.PreciseY = (vmath.Point{Y: maxY - 1}).CenterF()
		k.VelY = -k.VelY
		return true
	}
	return false
}

// ReflectBounds handles both axis boundary collisions, returns true if any reflection occurred
func ReflectBounds(k *Kinetic, width, height int) bool {
	rx := ReflectBoundsX(k, 0, width)
	ry := ReflectBoundsY(k, 0, height)
	return rx || ry
}

// GridPos returns current integer grid position
func GridPos(k *Kinetic) (x, y int) {
	p := vmath.PointAtF(k.PreciseX, k.PreciseY)
	return p.X, p.Y
}

// SetGridPos sets precise position from integer grid coordinates (centered)
func SetGridPos(k *Kinetic, x, y int) {
	k.PreciseX, k.PreciseY = vmath.Point{X: x, Y: y}.CenterF()
}
