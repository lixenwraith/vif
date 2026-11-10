package vmath

// Line walks a segment's cells one step at a time, start and end included, by the
// integer Bresenham error term: it starts at dx-dy and, doubled, steps x above -dy and
// y below dx, the same rule in every octant. A walk and its reverse can part on a tie.
// Its state is eight int32s in four pairs, the shape the compiler keeps in registers.
type Line struct {
	cell, step, span linePair // the current cell, the unit step and the extent
	walk             lineWalk
}

type linePair struct{ x, y int32 }

type lineWalk struct{ err, steps int32 }

// NewLine starts a walk on (x0, y0) toward (x1, y1)
func NewLine(x0, y0, x1, y1 int) Line {
	sx, dx := int32(1), int32(x1-x0)
	if dx < 0 {
		sx, dx = -1, -dx
	}
	sy, dy := int32(1), int32(y1-y0)
	if dy < 0 {
		sy, dy = -1, -dy
	}
	return Line{
		cell: linePair{int32(x0), int32(y0)}, step: linePair{sx, sy}, span: linePair{dx, dy},
		walk: lineWalk{err: dx - dy, steps: max(dx, dy)},
	}
}

// Next returns the walk one cell on, or the same walk on its end
func (l Line) Next() Line {
	if l.walk.steps == 0 {
		return l
	}
	l.walk.steps--
	e2 := 2 * l.walk.err
	if e2 > -l.span.y {
		l.walk.err -= l.span.y
		l.cell.x += l.step.x
	}
	if e2 < l.span.x {
		l.walk.err += l.span.x
		l.cell.y += l.step.y
	}
	return l
}

func (l Line) X() int { return int(l.cell.x) }

func (l Line) Y() int { return int(l.cell.y) }

// AtEnd reports whether the current cell is the end
func (l Line) AtEnd() bool { return l.walk.steps == 0 }

// Steps returns the steps left to the end, max(|dx|, |dy|) from the start
func (l Line) Steps() int { return int(l.walk.steps) }

// Line3 walks a segment's cells through a volume, start and end included: the axis
// of the longest span steps every cell, and each other axis when its error term,
// started at twice its span less the longest, stands above zero. A segment in one
// plane walks the cells Line does. The axes are kept from the longest span's on in
// turn, so a step indexes nothing; the walk is too large for registers.
type Line3 struct {
	a, b, c       int32 // the cell, from the axis of the longest span on
	sa, sb, sc    int32 // the unit steps
	da, db, dc    int32 // twice the spans
	eb, ec, steps int32 // the cross axes' error terms and the steps left
	drive         uint8 // the axis of the longest span: x, y, z
}

// NewLine3 starts a walk on p toward q; of equal spans the first axis drives
func NewLine3(p, q Point3) Line3 {
	x, y, z := int32(p.X), int32(p.Y), int32(p.Z)
	dx, dy, dz := int32(q.X-p.X), int32(q.Y-p.Y), int32(q.Z-p.Z)
	sx, sy, sz := int32(1), int32(1), int32(1)
	if dx < 0 {
		dx, sx = -dx, -1
	}
	if dy < 0 {
		dy, sy = -dy, -1
	}
	if dz < 0 {
		dz, sz = -dz, -1
	}
	switch {
	case dx >= dy && dx >= dz:
		return Line3{a: x, b: y, c: z, sa: sx, sb: sy, sc: sz, da: 2 * dx, db: 2 * dy, dc: 2 * dz,
			eb: 2*dy - dx, ec: 2*dz - dx, steps: dx}
	case dy >= dz:
		return Line3{a: y, b: z, c: x, sa: sy, sb: sz, sc: sx, da: 2 * dy, db: 2 * dz, dc: 2 * dx,
			eb: 2*dz - dy, ec: 2*dx - dy, steps: dy, drive: 1}
	}
	return Line3{a: z, b: x, c: y, sa: sz, sb: sx, sc: sy, da: 2 * dz, db: 2 * dx, dc: 2 * dy,
		eb: 2*dx - dz, ec: 2*dy - dz, steps: dz, drive: 2}
}

// Advance steps the walk one cell on in place, or leaves it on its end: what a
// loop over a walk calls, Next copying the walk out and back
func (l *Line3) Advance() {
	if l.steps == 0 {
		return
	}
	l.steps--
	l.a += l.sa
	if l.eb > 0 {
		l.b += l.sb
		l.eb -= l.da
	}
	l.eb += l.db
	if l.ec > 0 {
		l.c += l.sc
		l.ec -= l.da
	}
	l.ec += l.dc
}

// Next returns the walk one cell on, or the same walk on its end
func (l Line3) Next() Line3 {
	l.Advance()
	return l
}

// Point is the current cell
func (l Line3) Point() Point3 {
	switch l.drive {
	case 0:
		return Point3{X: int(l.a), Y: int(l.b), Z: int(l.c)}
	case 1:
		return Point3{X: int(l.c), Y: int(l.a), Z: int(l.b)}
	}
	return Point3{X: int(l.b), Y: int(l.c), Z: int(l.a)}
}

// AtEnd reports whether the current cell is the end
func (l Line3) AtEnd() bool { return l.steps == 0 }

// Steps returns the steps left to the end, the longest span from the start
func (l Line3) Steps() int { return int(l.steps) }
