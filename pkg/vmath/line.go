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
// plane walks the cells Line does.
type Line3 struct {
	cell, step, span [3]int32
	err              [2]int32 // the cross axes' error terms, the axes after drive in turn
	steps            int32
	drive            uint8 // the axis of the longest span
}

// NewLine3 starts a walk on a toward b
func NewLine3(a, b Point) Line3 {
	l := Line3{cell: [3]int32{int32(a.X), int32(a.Y), int32(a.Z)}}
	for i, d := range [3]int{b.X - a.X, b.Y - a.Y, b.Z - a.Z} {
		l.step[i], l.span[i] = 1, int32(d)
		if d < 0 {
			l.step[i], l.span[i] = -1, int32(-d)
		}
		if l.span[i] > l.span[l.drive] {
			l.drive = uint8(i)
		}
	}
	c1, c2 := (l.drive+1)%3, (l.drive+2)%3
	l.err = [2]int32{2*l.span[c1] - l.span[l.drive], 2*l.span[c2] - l.span[l.drive]}
	l.steps = l.span[l.drive]
	return l
}

// Next returns the walk one cell on, or the same walk on its end
func (l Line3) Next() Line3 {
	if l.steps == 0 {
		return l
	}
	l.steps--
	d := l.drive
	l.cell[d] += l.step[d]
	for k, c := range [2]uint8{(d + 1) % 3, (d + 2) % 3} {
		if l.err[k] > 0 {
			l.cell[c] += l.step[c]
			l.err[k] -= 2 * l.span[d]
		}
		l.err[k] += 2 * l.span[c]
	}
	return l
}

// Point is the current cell
func (l Line3) Point() Point { return Point{X: int(l.cell[0]), Y: int(l.cell[1]), Z: int(l.cell[2])} }

// AtEnd reports whether the current cell is the end
func (l Line3) AtEnd() bool { return l.steps == 0 }

// Steps returns the steps left to the end, the longest span from the start
func (l Line3) Steps() int { return int(l.steps) }
