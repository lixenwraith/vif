package physics

import (
	"math"

	"github.com/lixenwraith/vif/pkg/vmath"
)

// WallQueryFunc returns true if the footprint at the given top-left coordinates is blocked
type WallQueryFunc func(topLeftX, topLeftY int) bool

// CapSpeed limits velocity magnitude, returns clamped values
func CapSpeed(velX, velY, maxSpeed float64) (float64, float64) {
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

// BounceStats describes the work and resolved contacts from one integration.
type BounceStats struct {
	Steps               int
	WallCollisions      int
	BoundaryReflections int
}

// Hit reports whether any wall or boundary contact was resolved.
func (s BounceStats) Hit() bool { return s.WallCollisions != 0 || s.BoundaryReflections != 0 }

// IntegrateWithBounce integrates k over dt seconds in sub-steps, bouncing off its
// bounds and off walls. headerOffset runs from the position's cell to the top-left
// of the collision box checkWall is asked about; the bounds are the first and
// last cells, both inclusive, the position may hold; wallRestitution is the speed
// a bounce keeps (1 elastic, 0 sticky). Returns the cell reached and whether it hit.
func IntegrateWithBounce(
	k *Kinetic,
	dt float64,
	headerOffsetX, headerOffsetY int,
	boundMinX, boundMaxX int,
	boundMinY, boundMaxY int,
	wallRestitution float64,
	checkWall WallQueryFunc,
) (int, int, bool) {
	x, y, stats := IntegrateWithBounceStats(
		k, dt, headerOffsetX, headerOffsetY,
		boundMinX, boundMaxX, boundMinY, boundMaxY,
		wallRestitution, checkWall,
	)
	return x, y, stats.Hit()
}

// IntegrateWithBounceStats performs the same integration and exposes contact telemetry.
func IntegrateWithBounceStats(
	k *Kinetic,
	dt float64,
	headerOffsetX, headerOffsetY int,
	boundMinX, boundMaxX int,
	boundMinY, boundMaxY int,
	wallRestitution float64,
	checkWall WallQueryFunc,
) (int, int, BounceStats) {
	// Match Integrate's semi-implicit Euler ordering. Acceleration is applied
	// once for the whole tick before swept step sizing; applying it inside each
	// sub-step would make the result depend on collision/tunnelling work.
	k.VelX += k.AccelX * dt
	k.VelY += k.AccelY * dt

	steps := bounceSteps(math.Max(math.Abs(k.VelX*dt), math.Abs(k.VelY*dt)))
	dtStep := dt / float64(steps)
	stats := BounceStats{Steps: steps}

	// An axis moves, then reflects off its bounds or steps back from a wall; the
	// cell is kept across the moves, so each floors one coordinate
	x, y := GridPos(k)
	for range steps {
		start := k.PreciseX
		k.PreciseX += k.VelX * dtStep
		if cell, reflected := reflectCell(&k.PreciseX, &k.VelX, boundMinX, boundMaxX); reflected {
			stats.BoundaryReflections++
			k.VelX *= wallRestitution
			x = cell
		} else if checkWall(cell-headerOffsetX, y-headerOffsetY) {
			stats.WallCollisions++
			k.PreciseX = start
			k.VelX = -k.VelX * wallRestitution
		} else {
			x = cell
		}

		start = k.PreciseY
		k.PreciseY += k.VelY * dtStep
		if cell, reflected := reflectCell(&k.PreciseY, &k.VelY, boundMinY, boundMaxY); reflected {
			stats.BoundaryReflections++
			k.VelY *= wallRestitution
			y = cell
		} else if checkWall(x-headerOffsetX, cell-headerOffsetY) {
			stats.WallCollisions++
			k.PreciseY = start
			k.VelY = -k.VelY * wallRestitution
		} else {
			y = cell
		}
	}
	return x, y, stats
}

// bounceSteps is the sub-steps a tick takes for its longest axis travel, each
// within bounceStepLimit until the cap
func bounceSteps(maxDist float64) int {
	steps := 1
	if maxDist > bounceStepLimit {
		steps = int(maxDist/bounceStepLimit) + 1
	}
	return min(steps, bounceMaxSteps)
}

// WallQueryFunc3D returns true if the body whose corner nearest the origin lies
// on the given cell is blocked
type WallQueryFunc3D func(corner vmath.Point3) bool

// IntegrateWithBounce3D is IntegrateWithBounceStats in a volume: header is the
// offset from the body's cell to its corner nearest the origin, and bounds the
// cells the position may hold
func IntegrateWithBounce3D(
	k *Kinetic3D,
	dt float64,
	header vmath.Point3,
	bounds vmath.Box,
	wallRestitution float64,
	checkWall WallQueryFunc3D,
) (vmath.Point3, BounceStats) {
	k.Vel = vmath.V3Add(k.Vel, vmath.V3Scale(k.Accel, dt))

	steps := bounceSteps(max(math.Abs(k.Vel.X*dt), math.Abs(k.Vel.Y*dt), math.Abs(k.Vel.Z*dt)))
	dtStep := dt / float64(steps)
	stats := BounceStats{Steps: steps}

	corner := vmath.PointAt3(k.Pos).Sub(header)
	move := func(pos, vel *float64, at *int, header, lo, hi int) {
		start, was := *pos, *at
		*pos += *vel * dtStep
		cell, reflected := reflectCell(pos, vel, lo, hi)
		*at = cell - header
		if reflected {
			stats.BoundaryReflections++
			*vel *= wallRestitution
		} else if checkWall(corner) {
			stats.WallCollisions++
			*pos, *at = start, was
			*vel = -*vel * wallRestitution
		}
	}
	for range steps {
		move(&k.Pos.X, &k.Vel.X, &corner.X, header.X, bounds.X, bounds.X+bounds.Width-1)
		move(&k.Pos.Y, &k.Vel.Y, &corner.Y, header.Y, bounds.Y, bounds.Y+bounds.Height-1)
		move(&k.Pos.Z, &k.Vel.Z, &corner.Z, header.Z, bounds.Z, bounds.Z+bounds.Depth-1)
	}
	return corner.Add(header), stats
}
