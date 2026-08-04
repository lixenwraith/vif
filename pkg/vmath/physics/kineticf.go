package physics

import (
	"math"

	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// KineticF is a 2D point-mass state in float64.
// Position is a sub-cell coordinate in cells; grid cells are vmath.Point.
type KineticF struct {
	// PreciseX and PreciseY are sub-cell coordinates (cells)
	PreciseX, PreciseY float64
	// VelX and VelY are velocity in cells per second
	VelX, VelY float64
	// AccelX and AccelY are acceleration in cells per second squared
	AccelX, AccelY float64
}

// IntegrateF performs physics integration: v = v + a*dt; p = p + v*dt
func IntegrateF(k *KineticF, dt float64) (x, y int) {
	k.VelX += k.AccelX * dt
	k.VelY += k.AccelY * dt
	k.PreciseX += k.VelX * dt
	k.PreciseY += k.VelY * dt
	p := vmath.PointAtF(k.PreciseX, k.PreciseY)
	return p.X, p.Y
}

// ApplyImpulseF adds velocity delta (momentum transfer)
func ApplyImpulseF(k *KineticF, vx, vy float64) {
	k.VelX += vx
	k.VelY += vy
}

// SetImpulseF overrides velocity (hard redirect/stun)
func SetImpulseF(k *KineticF, vx, vy float64) {
	k.VelX = vx
	k.VelY = vy
}

// ReflectBoundsXF handles horizontal boundary collision, returns true if reflection occurred
// Clamps to centered position within valid cell range [minX, maxX)
func ReflectBoundsXF(k *KineticF, minX, maxX int) bool {
	x := int(math.Floor(k.PreciseX))
	if x < minX {
		k.PreciseX = float64(minX) + vmath.CellCenterF
		k.VelX = -k.VelX
		return true
	}
	if x >= maxX {
		k.PreciseX = float64(maxX-1) + vmath.CellCenterF
		k.VelX = -k.VelX
		return true
	}
	return false
}

// ReflectBoundsYF handles vertical boundary collision, returns true if reflection occurred
func ReflectBoundsYF(k *KineticF, minY, maxY int) bool {
	y := int(math.Floor(k.PreciseY))
	if y < minY {
		k.PreciseY = float64(minY) + vmath.CellCenterF
		k.VelY = -k.VelY
		return true
	}
	if y >= maxY {
		k.PreciseY = float64(maxY-1) + vmath.CellCenterF
		k.VelY = -k.VelY
		return true
	}
	return false
}

// ReflectBoundsF handles both axis boundary collisions, returns true if any reflection occurred
func ReflectBoundsF(k *KineticF, width, height int) bool {
	rx := ReflectBoundsXF(k, 0, width)
	ry := ReflectBoundsYF(k, 0, height)
	return rx || ry
}

// GridPosF returns current integer grid position
func GridPosF(k *KineticF) (x, y int) {
	p := vmath.PointAtF(k.PreciseX, k.PreciseY)
	return p.X, p.Y
}

// SetGridPosF sets precise position from integer grid coordinates (centered)
func SetGridPosF(k *KineticF, x, y int) {
	k.PreciseX, k.PreciseY = vmath.Point{X: x, Y: y}.CenterF()
}
