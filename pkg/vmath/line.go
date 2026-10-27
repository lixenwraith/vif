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
