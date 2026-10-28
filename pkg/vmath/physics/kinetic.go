package physics

import "github.com/lixenwraith/vif/pkg/vmath"

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
	p := vmath.PointAt(k.PreciseX, k.PreciseY)
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

// ReflectBoundsX handles horizontal boundary collision over the cells minX..maxX,
// both inclusive, returns true if reflection occurred: a position past either is
// clamped to the centre of the edge cell it passed
func ReflectBoundsX(k *Kinetic, minX, maxX int) bool {
	p := vmath.PointAt(k.PreciseX, k.PreciseY)
	if p.X < minX {
		k.PreciseX, _ = (vmath.Point{X: minX}).Center()
		k.VelX = -k.VelX
		return true
	}
	if p.X > maxX {
		k.PreciseX, _ = (vmath.Point{X: maxX}).Center()
		k.VelX = -k.VelX
		return true
	}
	return false
}

// ReflectBoundsY handles vertical boundary collision over the cells minY..maxY,
// both inclusive, returns true if reflection occurred
func ReflectBoundsY(k *Kinetic, minY, maxY int) bool {
	p := vmath.PointAt(k.PreciseX, k.PreciseY)
	if p.Y < minY {
		_, k.PreciseY = (vmath.Point{Y: minY}).Center()
		k.VelY = -k.VelY
		return true
	}
	if p.Y > maxY {
		_, k.PreciseY = (vmath.Point{Y: maxY}).Center()
		k.VelY = -k.VelY
		return true
	}
	return false
}

// ReflectBounds handles both axis boundary collisions over a width×height grid,
// returns true if any reflection occurred
func ReflectBounds(k *Kinetic, width, height int) bool {
	rx := ReflectBoundsX(k, 0, width-1)
	ry := ReflectBoundsY(k, 0, height-1)
	return rx || ry
}

// GridPos returns current integer grid position
func GridPos(k *Kinetic) (x, y int) {
	p := vmath.PointAt(k.PreciseX, k.PreciseY)
	return p.X, p.Y
}

// SetGridPos sets precise position from integer grid coordinates (centered)
func SetGridPos(k *Kinetic, x, y int) {
	k.PreciseX, k.PreciseY = vmath.Point{X: x, Y: y}.Center()
}
