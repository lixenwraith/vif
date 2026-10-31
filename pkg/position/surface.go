package position

import (
	"math"
	"slices"

	"github.com/lixenwraith/vif/pkg/vmath"
)

// Side is the side of a surface that is solid: Above, the rows before its height,
// or Below, the rows from it.
type Side uint8

const (
	Above Side = iota
	Below
)

// Surface is a boundary no entity holds: a height for each column from x 0 of the
// first layer, the cells on Side of it solid, blocking Mask as a block does. A
// ceiling is Above, a floor Below. Its writer lays it from a record of its own, so
// a capture carries that record and never the surface. Origin is the profile
// column at column 0, which what draws the surface anchors to.
type Surface struct {
	Mask   Mask
	Side   Side
	Origin int64
	// heights from column 0; ahead the heights past the right end the next shift
	// brings in, for a frame drawn between shifts, which block nothing
	heights []int
	ahead   []int
	pending bool // shifted since the merged bits moved
}

// Width is how many columns the surface covers, from x 0
func (s *Surface) Width() int { return len(s.heights) }

// Heights is the surface's heights from column 0, to read and not to keep
func (s *Surface) Heights() []int { return s.heights }

// Ahead is the heights past the surface's right end the next shift can bring in,
// to read and not to keep
func (s *Surface) Ahead() []int { return s.ahead }

// Height returns column x's height, and false outside the surface
func (s *Surface) Height(x int) (int, bool) {
	if x < 0 || x >= len(s.heights) {
		return 0, false
	}
	return s.heights[x], true
}

// solidAt reports whether row y is solid under height h
func (s *Surface) solidAt(h, y int) bool {
	if s.Side == Above {
		return y < h
	}
	return y >= h
}

// stops reports whether the surface blocks mask, 0 asking for any
func (s *Surface) stops(mask Mask) bool { return mask == 0 || s.Mask&mask != 0 }

// solid is the surfaces of one mask merged for the probes: for each column the
// greatest height solid above it and the least solid from it, and the cells
// those make within the grid as bits, so a cell is one bit whatever the
// surfaces. A shift of every surface of the mask moves the bits a column; a
// change of any other kind lays them again from the heights.
type solid struct {
	mask        Mask
	ceil, floor []int
	bits        Bits
	pending     int // the surfaces of the mask shifted since the bits moved
}

// stops reports whether the merged surfaces block mask, 0 asking for any
func (m *solid) stops(mask Mask) bool { return mask == 0 || m.mask&mask != 0 }

// lay sets the bits from the heights, within w×h
func (m *solid) lay(w, h int) {
	m.bits = m.bits.sized(Stride(w), h)
	for x := range min(len(m.ceil), w) {
		m.layColumn(x, h)
	}
}

// layColumn sets column x's bits from its heights
func (m *solid) layColumn(x, h int) {
	for y := range min(m.ceil[x], h) {
		m.bits.Set(x, y)
	}
	for y := max(m.floor[x], 0); y < h; y++ {
		m.bits.Set(x, y)
	}
}

// merge brings the merged surfaces to the surfaces as they stand, one entry a
// mask, in storage kept from the last merge, and lays their bits
func (x *Index[E]) merge() {
	x.unmerged = false
	n := 0
	for _, s := range x.surfaces {
		i := -1
		for j := range n {
			if x.solid[j].mask == s.Mask {
				i = j
			}
		}
		if i < 0 {
			if i = n; n == len(x.solid) {
				x.solid = append(x.solid, solid{})
			}
			x.solid[i] = solid{mask: s.Mask, ceil: x.solid[i].ceil[:0], floor: x.solid[i].floor[:0], bits: x.solid[i].bits}
			n++
		}
		s.pending = false
		m := &x.solid[i]
		for len(m.ceil) < len(s.heights) {
			m.ceil, m.floor = append(m.ceil, 0), append(m.floor, math.MaxInt)
		}
		for col, h := range s.heights {
			if s.Side == Above {
				m.ceil[col] = max(m.ceil[col], h)
			} else {
				m.floor[col] = min(m.floor[col], h)
			}
		}
	}
	x.solid = x.solid[:n]
	for i := range x.solid {
		x.solid[i].pending = 0
		x.solid[i].lay(x.Width, x.Height)
	}
}

// shifted notes that s moved a column: once every surface of its mask has, the
// merged heights and bits move a column with them and the last column is laid
// from the surfaces' last heights; a surface moving twice before the rest, or
// one moving alone before a probe, leaves the mask to a merge.
func (x *Index[E]) shifted(s *Surface) {
	if x.unmerged {
		return
	}
	i := -1
	for j := range x.solid {
		if x.solid[j].mask == s.Mask {
			i = j
		}
	}
	if i < 0 || s.pending {
		x.unmerged = true
		return
	}
	s.pending = true
	m := &x.solid[i]
	m.pending++
	n := 0
	for _, o := range x.surfaces {
		if o.Mask == s.Mask {
			n++
		}
	}
	if m.pending < n {
		return
	}
	w := len(m.ceil)
	if w > 0 {
		copy(m.ceil, m.ceil[1:])
		copy(m.floor, m.floor[1:])
		m.ceil[w-1], m.floor[w-1] = 0, math.MaxInt
	}
	for _, o := range x.surfaces {
		o.pending = false
		if o.Mask != s.Mask || len(o.heights) == 0 {
			continue
		}
		if last := len(o.heights) - 1; o.Side == Above {
			m.ceil[last] = max(m.ceil[last], o.heights[last])
		} else {
			m.floor[last] = min(m.floor[last], o.heights[last])
		}
	}
	m.pending = 0
	for y := range x.Height {
		row := m.bits.Row(y)
		ShiftRow(row, row, 1)
	}
	if last := min(w, x.Width) - 1; last >= 0 {
		m.layColumn(last, x.Height)
	}
}

// Surfaces is every surface laid, in the order laid. Read-only.
func (x *Index[E]) Surfaces() []*Surface { return x.surfaces }

// AddSurface lays a surface of heights from column 0, solid on side, blocking
// mask, origin its first profile column, and returns it for its writer to keep.
func (x *Index[E]) AddSurface(mask Mask, side Side, origin int64, heights []int) *Surface {
	s := &Surface{Mask: mask, Side: side, Origin: origin, heights: slices.Clone(heights)}
	x.surfaces = append(x.surfaces, s)
	x.log.lose()
	x.unmerged = true
	x.widths()
	return s
}

// LaySurface lays s again, from column 0 with origin its first profile column,
// its heights ahead forgotten.
func (x *Index[E]) LaySurface(s *Surface, origin int64, heights []int) {
	s.Origin, s.heights, s.ahead = origin, append(s.heights[:0], heights...), s.ahead[:0]
	x.log.lose()
	x.unmerged = true
	x.widths()
}

// RemoveSurface lifts s.
func (x *Index[E]) RemoveSurface(s *Surface) {
	if i := slices.Index(x.surfaces, s); i >= 0 {
		x.surfaces = slices.Delete(x.surfaces, i, i+1)
		x.log.lose()
		x.unmerged = true
		x.widths()
	}
}

// SetSurfaceAhead keeps heights as the columns past s's right end, in entry order
func (x *Index[E]) SetSurfaceAhead(s *Surface, heights []int) {
	s.ahead = append(s.ahead[:0], heights...)
}

// ShiftSurface moves s a column left: column 0 leaves it and enter stands at its
// right end. The log takes the cells its height crossed.
func (x *Index[E]) ShiftSurface(s *Surface, enter int) {
	w, h := x.Width, x.Height
	for col := range min(len(s.heights), w) {
		was, now := s.heights[col], enter
		if col+1 < len(s.heights) {
			now = s.heights[col+1]
		}
		// A row the height did not cross is solid in both columns or in neither
		for y := max(min(was, now), 0); y < min(max(was, now), h); y++ {
			x.log.add(vmath.Point{X: col, Y: y}, len(x.Cells))
		}
	}
	if len(s.heights) > 0 {
		copy(s.heights, s.heights[1:])
		s.heights[len(s.heights)-1] = enter
	}
	s.Origin++
	x.shifted(s)
}

// BlocksCarried is Blocks as a frame drawn carry columns into a scroll sees it:
// the surfaces and the blocks ride it, so cell (x, y) shows what x+carry holds
// now, and past a surface's right end the heights ahead of it
func (x *Index[E]) BlocksCarried(px, py, carry int, mask Mask) bool {
	if carry > 0 && px >= 0 {
		if px += carry; px >= x.aheadFrom {
			return x.blocksAhead(px, py, mask)
		}
	}
	return x.Blocks(px, py, mask)
}

// blocksAhead is BlocksCarried past the first surface's right end
func (x *Index[E]) blocksAhead(px, py int, mask Mask) bool {
	for _, s := range x.surfaces {
		if i := px - len(s.heights); i >= 0 && i < len(s.ahead) && s.stops(mask) &&
			py >= 0 && py < x.Height && s.solidAt(s.ahead[i], py) {
			return true
		}
	}
	return x.Blocks(px, py, mask)
}

// widths notes the surfaces' widths: the first column past one is where its
// heights ahead answer
func (x *Index[E]) widths() {
	x.aheadFrom = math.MaxInt
	for _, s := range x.surfaces {
		x.aheadFrom = min(x.aheadFrom, len(s.heights))
	}
}
