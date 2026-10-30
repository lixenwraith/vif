package position

// Column is one map column of a rock field: its ceiling fills rows [0, Ceil) and
// its floor rows [Floor, map height)
type Column struct {
	Ceil, Floor int
}

// Rock reports whether row y of the column is rock
func (c Column) Rock(y int) bool { return y < c.Ceil || y >= c.Floor }

// Rock is rock no entity holds, a column for each map column from x 0, blocking
// Mask as a wall does; every wall query reads it beside the blockers. Its writer
// lays it from state of its own, so a capture carries that state and never the
// field. Origin is the profile column at map column 0, which what draws the rock
// anchors to.
type Rock struct {
	Mask   Mask
	Origin int64
	cols   []Column
	// The columns past the right end the next shift can bring in, for drawing the
	// field carried on between ticks; they block nothing
	ahead []Column
}

// Width is how many map columns the field covers, from x 0
func (r *Rock) Width() int { return len(r.cols) }

// Columns is the field's columns from map column 0, to read and not to keep
func (r *Rock) Columns() []Column { return r.cols }

// Ahead is the columns past the field's right end the next shift can bring in, to
// read and not to keep
func (r *Rock) Ahead() []Column { return r.ahead }

// Column returns map column x's rock, and false outside the field
func (r *Rock) Column(x int) (Column, bool) {
	if x < 0 || x >= len(r.cols) {
		return Column{}, false
	}
	return r.cols[x], true
}

// blocks reports whether the field's rock fills (x, y) for mask (0 = any wall);
// the caller has bounded y by the map
func (r *Rock) blocks(x, y int, mask Mask) bool {
	if x < 0 || x >= len(r.cols) || (mask != 0 && r.Mask&mask == 0) {
		return false
	}
	return r.cols[x].Rock(y)
}

// blocksArea reports whether the field's rock fills any cell of [x0, x1) × [y0, y1)
// for mask; the caller has bounded the rows by the map
func (r *Rock) blocksArea(x0, y0, x1, y1 int, mask Mask) bool {
	if (mask != 0 && r.Mask&mask == 0) || y0 >= y1 {
		return false
	}
	for x := max(x0, 0); x < min(x1, len(r.cols)); x++ {
		if c := r.cols[x]; y0 < c.Ceil || y1 > c.Floor {
			return true
		}
	}
	return false
}

// mark sets every rock cell within w×h in cells
func (r *Rock) mark(w, h int, cells []bool) {
	for x, c := range r.cols[:min(len(r.cols), w)] {
		for y := range min(c.Ceil, h) {
			cells[y*w+x] = true
		}
		for y := max(c.Floor, 0); y < h; y++ {
			cells[y*w+x] = true
		}
	}
}

// Rock returns the rock field, or nil when none stands; it changes only through
// SetRock, SetRockAhead and ShiftRock.
func (x *Index[E]) Rock() *Rock { return x.rock }

// SetRock lays cols as the rock field from map column 0, origin its first profile
// column; no columns lift the field
func (x *Index[E]) SetRock(mask Mask, origin int64, cols []Column) {
	x.epoch++
	x.log.lose()
	if len(cols) == 0 {
		x.rock = nil
		return
	}
	if x.rock == nil {
		x.rock = &Rock{}
	}
	x.rock.Mask, x.rock.Origin = mask, origin
	x.rock.cols = append(x.rock.cols[:0], cols...)
	x.rock.ahead = x.rock.ahead[:0]
}

// SetRockAhead keeps cols as the columns past the field's right end, in entry order
func (x *Index[E]) SetRockAhead(cols []Column) {
	if x.rock != nil {
		x.rock.ahead = append(x.rock.ahead[:0], cols...)
	}
}

// BlocksDrawn is Blocks as a frame draws the rock's carry columns on: rock and
// shared walls ride its scroll, so cell (x, y) shows what x+carry holds now, and
// past the map's right edge the field's columns ahead
func (x *Index[E]) BlocksDrawn(px, py, carry int, mask Mask) bool {
	if carry <= 0 || px < 0 {
		return x.Blocks(px, py, mask)
	}
	px += carry
	if r := x.rock; r != nil && px >= len(r.cols) && px < len(r.cols)+len(r.ahead) {
		return py >= 0 && py < x.Height && (mask == 0 || r.Mask&mask != 0) && r.ahead[px-len(r.cols)].Rock(py)
	}
	return x.Blocks(px, py, mask)
}

// ShiftRock moves the field a column left: map column 0 leaves it and enter stands
// at its right end. The log takes the cells each column's ends crossed.
func (x *Index[E]) ShiftRock(enter Column) {
	r := x.rock
	if r == nil {
		return
	}
	x.epoch++
	w, h := x.Width, x.Height
	for col := range min(len(r.cols), w) {
		was, now := r.cols[col], enter
		if col+1 < len(r.cols) {
			now = r.cols[col+1]
		}
		// A row neither end crossed is rock in both columns or in neither
		for _, ends := range [2][2]int{{was.Ceil, now.Ceil}, {was.Floor, now.Floor}} {
			for y := max(min(ends[0], ends[1]), 0); y < min(max(ends[0], ends[1]), h); y++ {
				x.log.add(col, y, len(x.Cells))
			}
		}
	}
	copy(r.cols, r.cols[1:])
	r.cols[len(r.cols)-1] = enter
	r.Origin++
}
