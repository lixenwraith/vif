package navigation

import (
	"math/bits"
	"slices"

	"github.com/lixenwraith/vif/pkg/position"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// Footprint is a body's cells around the header it navigates by: Width×Height
// cells, the header HeaderX, HeaderY from the top-left
type Footprint struct {
	Width, Height, HeaderX, HeaderY int
}

// Point is the footprint of a body one cell across
var Point = Footprint{Width: 1, Height: 1}

// Covers reports whether f's cells around a header hold o's around the same
// header, so every header f fits on o fits on too
func (f Footprint) Covers(o Footprint) bool {
	return f.HeaderX >= o.HeaderX && f.HeaderY >= o.HeaderY &&
		f.Width-f.HeaderX >= o.Width-o.HeaderX && f.Height-f.HeaderY >= o.Height-o.HeaderY
}

// CompositePassability is where a footprint's header may stand: a bit a header,
// set where the footprint lies off the grid or over a wall, derived from the
// walls a row of words at a time.
type CompositePassability struct {
	Width, Height          int
	FootprintW, FootprintH int
	HeaderOffX, HeaderOffY int

	blocked position.Bits
	edge    []uint64 // the headers whose footprint runs off the grid's columns, a row
	rowOr   []uint64 // deriveRow's scratch: the walls under a header row's footprint rows
	win     []uint64
	turned  []uint64 // the bits deriveRow changed, for the headers to report
	// Moved's scratch: the header rows a change reaches, each once by its mark, and
	// the words of each it reaches
	rows   []int32
	lo, hi []int32
	marks  []uint32
	mark   uint32
}

// NewCompositePassability creates the passability grid of a footprint on a map
func NewCompositePassability(mapW, mapH int, foot Footprint) *CompositePassability {
	p := &CompositePassability{
		FootprintW: foot.Width,
		FootprintH: foot.Height,
		HeaderOffX: foot.HeaderX,
		HeaderOffY: foot.HeaderY,
	}
	p.Resize(mapW, mapH)
	return p
}

// Clone returns a grid sharing no storage with p.
func (p *CompositePassability) Clone() *CompositePassability {
	n := *p
	n.blocked.Words, n.edge = slices.Clone(p.blocked.Words), slices.Clone(p.edge)
	n.rowOr, n.win, n.turned, n.rows, n.lo, n.hi, n.marks, n.mark = nil, nil, nil, nil, nil, nil, nil, 0
	return &n
}

// Resize adjusts dimensions, invalidates all cells
func (p *CompositePassability) Resize(width, height int) {
	p.Width, p.Height = width, height
	stride := position.Stride(width)
	p.blocked = position.NewBits(width, height)
	p.edge = grow(p.edge, stride)
	clear(p.edge)
	for x := range width {
		if !p.fitsX(x) {
			p.edge[x>>6] |= 1 << (uint(x) & 63)
		}
	}
	for x := width; x < stride*64; x++ { // past the width no header stands
		p.edge[x>>6] |= 1 << (uint(x) & 63)
	}
}

// Bits is the grid: a bit set where no header may stand. Read-only.
func (p *CompositePassability) Bits() position.Bits { return p.blocked }

// fitsX reports whether the footprint at a header in column x lies within the
// grid's columns, fitsY the same for row y
func (p *CompositePassability) fitsX(x int) bool {
	x0 := x - p.HeaderOffX
	return x0 >= 0 && x0+p.FootprintW <= p.Width
}

func (p *CompositePassability) fitsY(y int) bool {
	y0 := y - p.HeaderOffY
	return y0 >= 0 && y0+p.FootprintH <= p.Height
}

// Compute derives every header from walls, a grid of the map's blocked cells, and
// reports whether any changed.
func (p *CompositePassability) Compute(walls position.Bits) bool {
	p.scratch()
	changed := false
	for y := range p.Height {
		changed = p.deriveRow(walls, y, 0, p.blocked.Stride-1) || changed
	}
	return changed
}

// scratch sizes deriveRow's rows
func (p *CompositePassability) scratch() {
	stride := p.blocked.Stride
	p.rowOr, p.win, p.turned = grow(p.rowOr, stride), grow(p.win, stride), grow(p.turned, stride)
}

// deriveRow derives words [w0, w1] of header row y: the OR of the walls' words
// under the footprint's rows, each brought to the header's column, and the edge.
// It leaves the bits that changed in turned and reports whether any did.
func (p *CompositePassability) deriveRow(walls position.Bits, y, w0, w1 int) bool {
	row := p.blocked.Row(y)
	win := p.win[w0 : w1+1]
	if !p.fitsY(y) {
		for i := range win {
			win[i] = ^uint64(0)
		}
	} else {
		// The walls' words the shifts read: the range and the word either side
		r0, r1 := max(w0-1, 0), min(w1+1, p.blocked.Stride-1)
		rowOr := p.rowOr[r0 : r1+1]
		copy(rowOr, walls.Row(y - p.HeaderOffY)[r0:r1+1])
		for r := y - p.HeaderOffY + 1; r < y-p.HeaderOffY+p.FootprintH; r++ {
			for i, w := range walls.Row(r)[r0 : r1+1] {
				rowOr[i] |= w
			}
		}
		// The footprint's columns, each brought to the header's: cell x+d for d in
		// [-HeaderOffX, FootprintW-HeaderOffX)
		copy(win, p.edge[w0:w1+1])
		for d := -p.HeaderOffX; d < p.FootprintW-p.HeaderOffX; d++ {
			p.orShifted(p.win, p.rowOr, d, w0, w1)
		}
	}
	changed := false
	for i, w := range win {
		if p.turned[w0+i] = row[w0+i] ^ w; p.turned[w0+i] != 0 {
			row[w0+i], changed = w, true
		}
	}
	return changed
}

// orShifted ORs src's row moved d cells left into words [w0, w1] of dst, d in
// (-64, 64), reading src a word either side of them
func (p *CompositePassability) orShifted(dst, src []uint64, d, w0, w1 int) {
	switch {
	case d == 0:
		for i := w0; i <= w1; i++ {
			dst[i] |= src[i]
		}
	case d > 0:
		for i := w0; i <= w1; i++ {
			dst[i] |= src[i] >> uint(d)
			if i+1 < len(src) {
				dst[i] |= src[i+1] << (64 - uint(d))
			}
		}
	default:
		e := uint(-d)
		for i := w0; i <= w1; i++ {
			dst[i] |= src[i] << e
			if i > 0 {
				dst[i] |= src[i-1] >> (64 - e)
			}
		}
	}
}

// Moved brings passability up to cells that closed and opened since it was
// computed, reading the walls as they stand: the header rows and words a changed
// cell's footprints reach are derived again, and flip is called once for every
// header whose fit changed, with its new validity.
func (p *CompositePassability) Moved(walls position.Bits, closed, opened []vmath.Point, flip func(x, y int, valid bool)) {
	p.scratch()
	stride := p.blocked.Stride
	p.lo, p.hi, p.marks = grow(p.lo, p.Height), grow(p.hi, p.Height), grow(p.marks, p.Height)
	if p.mark++; p.mark == 0 {
		clear(p.marks[:cap(p.marks)])
		p.mark = 1
	}
	p.rows = p.rows[:0]
	reach := func(c vmath.Point) {
		if c.X < 0 || c.Y < 0 || c.X >= p.Width || c.Y >= p.Height {
			return
		}
		x0 := max(c.X+p.HeaderOffX-p.FootprintW+1, 0) >> 6
		x1 := min(c.X+p.HeaderOffX, p.Width-1) >> 6
		for hy := max(c.Y+p.HeaderOffY-p.FootprintH+1, 0); hy <= min(c.Y+p.HeaderOffY, p.Height-1); hy++ {
			if p.marks[hy] != p.mark {
				p.marks[hy], p.lo[hy], p.hi[hy] = p.mark, int32(stride), -1
				p.rows = append(p.rows, int32(hy))
			}
			p.lo[hy], p.hi[hy] = min(p.lo[hy], int32(x0)), max(p.hi[hy], int32(x1))
		}
	}
	for _, c := range closed {
		reach(c)
	}
	for _, c := range opened {
		reach(c)
	}
	for _, hy := range p.rows {
		y, w0, w1 := int(hy), int(p.lo[hy]), int(p.hi[hy])
		if !p.deriveRow(walls, y, w0, w1) {
			continue
		}
		row := p.blocked.Row(y)
		for i := w0; i <= w1; i++ {
			for turned := p.turned[i]; turned != 0; turned &= turned - 1 {
				x := i*64 + bits.TrailingZeros64(turned)
				if x < p.Width && flip != nil {
					flip(x, y, row[i]>>(uint(x)&63)&1 == 0)
				}
			}
		}
	}
}

// IsBlocked returns true if composite cannot occupy header position (x,y)
// Used as WallChecker for composite flow field computation
func (p *CompositePassability) IsBlocked(x, y int) bool {
	if x < 0 || y < 0 || x >= p.Width || y >= p.Height {
		return true
	}
	return p.blocked.Bit(x, y)
}

// IsValid returns true if composite can occupy header position (x,y)
func (p *CompositePassability) IsValid(x, y int) bool {
	return !p.IsBlocked(x, y)
}

// --- DEBUG ---

// GetDimensions returns grid dimensions
func (p *CompositePassability) GetDimensions() (width, height int) {
	return p.Width, p.Height
}

// Footprint is the footprint the grid fits
func (p *CompositePassability) Footprint() Footprint {
	return Footprint{Width: p.FootprintW, Height: p.FootprintH, HeaderX: p.HeaderOffX, HeaderY: p.HeaderOffY}
}
