package position

// Column is one map column of a rock field: its ceiling fills rows [0, Ceil) and
// its floor rows [Floor, map height)
type Column struct {
	Ceil, Floor int
}

// Rock reports whether row y of the column is rock
func (c Column) Rock(y int) bool { return y < c.Ceil || y >= c.Floor }

// Rock is rock no entity holds, a column for each map column from x 0, blocking
// Mask as a wall does; every wall query reads it before the cells. Its writer
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

// Rock returns the rock field, or nil when none stands; it changes only through
// SetRock, SetRockAhead and ShiftRock.
func (x *Index[E]) Rock() *Rock { return x.rock }

// SetRock lays cols as the rock field from map column 0, origin its first profile
// column; no columns lift the field
func (x *Index[E]) SetRock(mask Mask, origin int64, cols []Column) {
	x.log.lose()
	if len(cols) == 0 {
		x.rock = nil
	} else {
		if x.rock == nil {
			x.rock = &Rock{}
		}
		x.rock.Mask, x.rock.Origin = mask, origin
		x.rock.cols = append(x.rock.cols[:0], cols...)
		x.rock.ahead = x.rock.ahead[:0]
	}
	if !x.stale {
		x.layRock()
	}
}

// layRock sets the rock's bits from its columns, within the grid
func (x *Index[E]) layRock() {
	x.rockBits = x.rockBits.sized(Stride(x.Width), x.Height)
	if x.rock == nil {
		return
	}
	for px, c := range x.rock.cols[:min(len(x.rock.cols), x.Width)] {
		x.layColumn(px, c)
	}
}

// layColumn sets map column px's bits for c
func (x *Index[E]) layColumn(px int, c Column) {
	for py := range min(c.Ceil, x.Height) {
		x.rockBits.Set(px, py)
	}
	for py := max(c.Floor, 0); py < x.Height; py++ {
		x.rockBits.Set(px, py)
	}
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
// at its right end. The log takes the cells each column's ends crossed, and the
// rock's bits move a column with it.
func (x *Index[E]) ShiftRock(enter Column) {
	r := x.rock
	if r == nil {
		return
	}
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
	if x.stale {
		return
	}
	// Every bit a column left, then the last column the map shows laid afresh,
	// since its bits came from past the map's edge or past the field's
	for py := range h {
		row := x.rockBits.Row(py)
		ShiftRow(row, row, 1)
	}
	if last := min(len(r.cols), w) - 1; last >= 0 {
		x.layColumn(last, r.cols[last])
	}
}
