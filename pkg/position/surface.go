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

// Surface is a boundary no entity holds: a height for each column from x 0 of
// Layer, the cells on Side of it solid, blocking Mask as a block does. A ceiling
// is Above, a floor Below. Its writer lays it from a record of its own, so a
// capture carries that record and never the surface, and sets Mask, Side and
// Layer only before laying it again.
type Surface struct {
	Mask  Mask
	Side  Side
	Layer int
	// heights from column 0; ahead the heights past the right end the next shift
	// brings in, for a frame drawn between shifts, which block nothing
	heights []int
	ahead   []int
	pending bool // shifted since the merged bits of its mask and layer moved
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

// solid is the cells the surfaces of one mask make solid, as bits sized as a
// plane's, so a cell is one bit whatever the surfaces. A shift of every surface
// of the mask on a layer moves that layer's bits a column; a change of any other
// kind lays them again from the heights.
type solid struct {
	mask Mask
	bits Bits
}

// stops reports whether the merged surfaces block mask, 0 asking for any
func (m *solid) stops(mask Mask) bool { return mask == 0 || m.mask&mask != 0 }

// lay sets the solid cells of s's column col, which with s's layer lies inside a
// volume of h rows a layer
func (m *solid) lay(s *Surface, col, h int) {
	from, to := 0, min(s.heights[col], h)
	if s.Side == Below {
		from, to = max(s.heights[col], 0), h
	}
	for y := from; y < to; y++ {
		m.bits.Set(col, s.Layer*h+y)
	}
}

// merged returns mask's entry among solids, or nil
func merged(solids []solid, mask Mask) *solid {
	for i := range solids {
		if solids[i].mask == mask {
			return &solids[i]
		}
	}
	return nil
}

// merge lays the merged bits again from the surfaces as they stand, one entry a
// mask, in storage kept from the last merge; a layer or a column outside the
// volume lays nothing
func (x *Index[E]) merge() {
	x.stale &^= staleSurfaces | staleShift
	n := 0
	for _, s := range x.surfaces {
		s.pending = false
		if merged(x.solid[:n], s.Mask) != nil {
			continue
		}
		if n == len(x.solid) {
			x.solid = append(x.solid, solid{})
		}
		x.solid[n] = solid{mask: s.Mask, bits: x.solid[n].bits.sized(Stride(x.Width), x.Height*x.Depth)}
		n++
	}
	x.solid = x.solid[:n]
	for _, s := range x.surfaces {
		if uint(s.Layer) >= uint(x.Depth) {
			continue
		}
		m := merged(x.solid, s.Mask)
		for col := range min(len(s.heights), x.Width) {
			m.lay(s, col, x.Height)
		}
	}
	for i := range x.planes {
		x.bind(&x.planes[i])
	}
}

// shifted notes that s moved a column: once every surface of its mask on its
// layer has, that layer's merged bits move a column with them and each lays the
// column it brought in. Until then a probe lays the surfaces again, as it does
// for a surface moving twice before the rest.
func (x *Index[E]) shifted(s *Surface) {
	if x.stale&staleSurfaces != 0 {
		return
	}
	m := merged(x.solid, s.Mask)
	if m == nil || s.pending {
		x.spoil(staleSurfaces)
		return
	}
	s.pending = true
	whole, others := true, false
	for _, o := range x.surfaces {
		if o.Mask == s.Mask && o.Layer == s.Layer {
			whole = whole && o.pending
		} else {
			others = others || o.pending
		}
	}
	if !whole {
		x.spoil(staleShift)
		return
	}
	if !others {
		x.stale &^= staleShift
	}
	inside := uint(s.Layer) < uint(x.Depth)
	for y := s.Layer * x.Height; inside && y < (s.Layer+1)*x.Height; y++ {
		row := m.bits.Row(y)
		ShiftRow(row, row, 1)
	}
	for _, o := range x.surfaces {
		if o.Mask != s.Mask || o.Layer != s.Layer {
			continue
		}
		o.pending = false
		if col := min(len(o.heights), x.Width) - 1; inside && col >= 0 {
			m.lay(o, col, x.Height)
		}
	}
}

// Surfaces is every surface laid, in the order laid. Read-only.
func (x *Index[E]) Surfaces() []*Surface { return x.surfaces }

// AddSurface lays a surface of heights from column 0 of layer, solid on side,
// blocking mask, and returns it for its writer to keep.
func (x *Index[E]) AddSurface(mask Mask, side Side, layer int, heights []int) *Surface {
	s := &Surface{Mask: mask, Side: side, Layer: layer, heights: slices.Clone(heights)}
	x.surfaces = append(x.surfaces, s)
	x.log.lose()
	x.spoil(staleSurfaces)
	x.widths()
	return s
}

// LaySurface lays s again from column 0 as its fields now stand, its heights
// ahead forgotten.
func (x *Index[E]) LaySurface(s *Surface, heights []int) {
	s.heights, s.ahead = append(s.heights[:0], heights...), s.ahead[:0]
	x.log.lose()
	x.spoil(staleSurfaces)
	x.widths()
}

// RemoveSurface lifts s.
func (x *Index[E]) RemoveSurface(s *Surface) {
	if i := slices.Index(x.surfaces, s); i >= 0 {
		x.surfaces = slices.Delete(x.surfaces, i, i+1)
		x.log.lose()
		x.spoil(staleSurfaces)
		x.widths()
	}
}

// SetSurfaceAhead keeps heights as the columns past s's right end, in entry order
func (x *Index[E]) SetSurfaceAhead(s *Surface, heights []int) {
	s.ahead = append(s.ahead[:0], heights...)
}

// ShiftSurface moves s a column left: column 0 leaves it and enter stands at its
// right end. The log takes the cells of the volume its height crossed.
func (x *Index[E]) ShiftSurface(s *Surface, enter int) {
	w, h := x.Width, x.Height
	if uint(s.Layer) >= uint(x.Depth) {
		w = 0
	}
	for col := range min(len(s.heights), w) {
		was, now := s.heights[col], enter
		if col+1 < len(s.heights) {
			now = s.heights[col+1]
		}
		// A row the height did not cross is solid in both columns or in neither
		for y := max(min(was, now), 0); y < min(max(was, now), h); y++ {
			x.log.add(vmath.Point3{X: col, Y: y, Z: s.Layer}, len(x.cells))
		}
	}
	if len(s.heights) > 0 {
		copy(s.heights, s.heights[1:])
		s.heights[len(s.heights)-1] = enter
	}
	x.shifted(s)
}

// BlocksCarried is Blocks as a frame drawn carry columns into a scroll sees it:
// the surfaces and the blocks ride it, so p's cell shows what the one carry
// columns on holds now, and past a surface's right end the heights ahead of it
func (x *Index[E]) BlocksCarried[P Coord](p P, carry int, mask Mask) bool {
	c, flat := at(p)
	if carry > 0 && c.X >= 0 {
		if c.X += carry; c.X >= x.aheadFrom && x.solidAhead(c, mask) {
			return true
		}
	}
	return x.in(c, flat) && x.probe(mask).blocked(c.X, x.row(c, flat))
}

// solidAhead reports whether c, past a surface's right end on its layer, is
// solid by the heights ahead of that surface
func (x *Index[E]) solidAhead(c Point3, mask Mask) bool {
	if c.Y < 0 || c.Y >= x.Height || uint(c.Z) >= uint(x.Depth) {
		return false
	}
	for _, s := range x.surfaces {
		if i := c.X - len(s.heights); i >= 0 && i < len(s.ahead) && s.Layer == c.Z && s.stops(mask) &&
			s.solidAt(s.ahead[i], c.Y) {
			return true
		}
	}
	return false
}

// widths notes the surfaces' widths: the first column past one is where its
// heights ahead answer
func (x *Index[E]) widths() {
	x.aheadFrom = math.MaxInt
	for _, s := range x.surfaces {
		x.aheadFrom = min(x.aheadFrom, len(s.heights))
	}
}
