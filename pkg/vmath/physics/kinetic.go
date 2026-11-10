package physics

import (
	"math"

	"github.com/lixenwraith/vif/pkg/vmath"
)

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

// reflectCell is one axis of a bounds reflection over the cells lo..hi, both
// inclusive: a coordinate past either lands on the centre of the edge cell it
// passed, its velocity flipped. It returns the coordinate's cell either way.
func reflectCell(pos, vel *float64, lo, hi int) (cell int, reflected bool) {
	cell = int(math.Floor(*pos))
	if cell < lo {
		cell = lo
	} else if cell > hi {
		cell = hi
	} else {
		return cell, false
	}
	*pos = float64(cell) + vmath.CellCenter
	*vel = -*vel
	return cell, true
}

// ReflectBoundsX handles horizontal boundary collision over the cells minX..maxX,
// both inclusive, returns true if reflection occurred: a position past either is
// clamped to the centre of the edge cell it passed
func ReflectBoundsX(k *Kinetic, minX, maxX int) bool {
	_, reflected := reflectCell(&k.PreciseX, &k.VelX, minX, maxX)
	return reflected
}

// ReflectBoundsY handles vertical boundary collision over the cells minY..maxY,
// both inclusive, returns true if reflection occurred
func ReflectBoundsY(k *Kinetic, minY, maxY int) bool {
	_, reflected := reflectCell(&k.PreciseY, &k.VelY, minY, maxY)
	return reflected
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

// Kinetic3D is Kinetic in a volume: a point mass's position in cells, velocity in
// cells per second and acceleration in cells per second squared. Its cell is
// vmath.PointAt3(k.Pos) and a cell's centre p.Center(); what is one vector
// expression has no function here: an impulse is V3Add onto Vel, a speed cap
// V3ClampMagnitude.
type Kinetic3D struct {
	Pos, Vel, Accel vmath.Vec3
}

// Integrate3D performs physics integration: v = v + a*dt; p = p + v*dt
func Integrate3D(k *Kinetic3D, dt float64) vmath.Point3 {
	k.Vel = vmath.V3Add(k.Vel, vmath.V3Scale(k.Accel, dt))
	return IntegratePosition3D(k, dt)
}

// ReflectBounds3D handles boundary collision on each axis over a box of cells,
// the reflected velocity scaled by restitution, returns true if any reflected
func ReflectBounds3D(k *Kinetic3D, bounds vmath.Box, restitution float64) bool {
	_, rx := reflectCell(&k.Pos.X, &k.Vel.X, bounds.X, bounds.X+bounds.Width-1)
	_, ry := reflectCell(&k.Pos.Y, &k.Vel.Y, bounds.Y, bounds.Y+bounds.Height-1)
	_, rz := reflectCell(&k.Pos.Z, &k.Vel.Z, bounds.Z, bounds.Z+bounds.Depth-1)
	if rx {
		k.Vel.X *= restitution
	}
	if ry {
		k.Vel.Y *= restitution
	}
	if rz {
		k.Vel.Z *= restitution
	}
	return rx || ry || rz
}
