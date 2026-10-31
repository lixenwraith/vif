// Package position is a map's spatial index and the geometry over it: a dense grid
// of cells, a map or a volume of layers, holding what stands in each; the blocks
// among them, cells an occupant
// blocks for a mask, kept as a bit a cell for each mask asked; the surfaces no
// entity holds, a height a column with a solid side; the log of the cells whose
// block answer changed; and the searches a placement, a line or a ray makes over
// them. It knows no game: an entity is any comparable value, and its class, which
// occupant is a block and the cell aspect come from the caller.
package position

import (
	"github.com/lixenwraith/vif/pkg/vmath"
)

// Point is a cell's coordinate, vmath's: a column, a row and a layer, 0 on a map
// one layer deep. A query taking a column and a row answers for the first layer;
// one taking a Point, for any layer.
type Point = vmath.Point

// Frame is the map's frame, read live: the cell aspect the searches round on. The
// bounds every query answers within are the grid's.
type Frame interface {
	Aspect() vmath.Aspect
}

// Mask selects the kinds of mover a block stops; a query's 0 asks for any block.
type Mask uint8

// Class is one half of a cell: the shared half fills it from the front, the
// private half from the back, within PrivateBudget.
type Class uint8

const (
	ClassShared Class = iota
	ClassPrivate
)

// Scope selects the halves of a cell an enumeration visits.
type Scope uint8

const (
	ScopeShared  Scope = 1 << ClassShared
	ScopePrivate Scope = 1 << ClassPrivate
	ScopeAll           = ScopeShared | ScopePrivate
)

// Selects reports whether the scope includes class c.
func (s Scope) Selects(c Class) bool { return s&(1<<c) != 0 }

// Index is one map's spatial index: its grid, the caller's word on which shared
// occupant is a block and what it stops, a plane of bits for each mask a query
// asked, kept with every block change, the surfaces, the log of the cells whose
// block answer changed, and the searches over them.
// SYNC: the caller serialises every call; the index locks nothing.
type Index[E comparable] struct {
	*Grid[E]
	frame     Frame
	blockOf   func(e E) (mask Mask, block bool)
	surfaces  []*Surface
	solid     []solid // the surfaces merged, one entry a mask, once unmerged is false
	unmerged  bool
	aheadFrom int       // the first column past a surface, where its heights ahead answer
	log       changeLog // the cells each block change can have moved, for BlockChanges

	// planes holds one plane per mask a query asked, planeOf naming it; stale marks
	// them all for rebuilding from the cells before the next query.
	planes  []plane
	planeOf [256]int8
	stale   bool
}

// plane is the cells a block stands in for one mask
type plane struct {
	mask Mask
	Bits
}

// New makes an empty index over frame; blockOf says whether a shared occupant is
// a block and which movers it stops.
func New[E comparable](frame Frame, blockOf func(e E) (Mask, bool)) *Index[E] {
	// The grid is sized by the caller to the map it indexes: a ceiling-sized one
	// costs 32 MB, which an instance per bot multiplies.
	x := &Index[E]{Grid: NewGrid[E](0, 0, 1), frame: frame, blockOf: blockOf}
	for i := range x.planeOf {
		x.planeOf[i] = -1
	}
	return x
}

// Resize sizes the grid to w×h×d, emptying it, and loses the log, since the cells
// moved; the caller places its entities again, and the planes follow.
func (x *Index[E]) Resize(w, h, d int) {
	x.Grid.Resize(w, h, d)
	x.log.lose()
	x.stale, x.unmerged = true, true
}

// Clear empties the grid, lifts every surface and loses the log.
func (x *Index[E]) Clear() {
	x.Grid.Clear()
	x.surfaces, x.solid, x.unmerged = x.surfaces[:0], x.solid[:0], false
	x.widths()
	x.log.lose()
	x.stale = true
}

// planeFor returns mask's plane, built from the cells on its first use.
func (x *Index[E]) planeFor(mask Mask) *plane {
	if x.stale {
		x.rebuild()
	}
	if i := x.planeOf[mask]; i >= 0 {
		return &x.planes[i]
	}
	x.planeOf[mask] = int8(len(x.planes))
	x.planes = append(x.planes, plane{mask: mask, Bits: NewBits(x.Width, x.Height*x.Depth)})
	p := &x.planes[len(x.planes)-1]
	for _, i := range x.occupied {
		if m, block := x.cellBlock(int(i)); block && (mask == 0 || m&mask != 0) {
			p.Set(int(i)%x.Width, int(i)/x.Width)
		}
	}
	return p
}

// rebuild sets every plane from the cells
func (x *Index[E]) rebuild() {
	x.stale = false
	stride := Stride(x.Width)
	for i := range x.planes {
		x.planes[i].Bits = x.planes[i].Bits.sized(stride, x.Height*x.Depth)
	}
	for _, i := range x.occupied {
		x.update(int(i))
	}
}

// cellBlock is what the shared occupants of cell i make of it: the masks of its
// blocks, and whether one stands there.
func (x *Index[E]) cellBlock(i int) (mask Mask, block bool) {
	cell := &x.Cells[i]
	for _, e := range cell.Entities[:cell.SharedCount] {
		if m, b := x.blockOf(e); b {
			mask, block = mask|m, true
		}
	}
	return mask, block
}

// update brings every plane's bit for cell i to its occupants
func (x *Index[E]) update(i int) {
	if len(x.planes) == 0 {
		return
	}
	mask, block := x.cellBlock(i)
	px, row := i%x.Width, i/x.Width
	for p := range x.planes {
		x.planes[p].Put(px, row, block && (x.planes[p].mask == 0 || mask&x.planes[p].mask != 0))
	}
}

// Blocks reports whether (x, y) of the first layer is blocked for mask: a
// surface's solid side, or a shared occupant blockOf names. Outside the map
// nothing is.
func (x *Index[E]) Blocks(px, py int, mask Mask) bool {
	if px < 0 || px >= x.Width || py < 0 || py >= x.Height {
		return false
	}
	if x.unmerged {
		x.merge()
	}
	for i := range x.solid {
		if m := &x.solid[i]; (mask == 0 || m.mask&mask != 0) && m.bits.Bit(px, py) {
			return true
		}
	}
	return x.planeFor(mask).Bit(px, py)
}

// BlocksAt is Blocks for a cell of any layer; the surfaces lie on the first.
func (x *Index[E]) BlocksAt(p Point, mask Mask) bool {
	if p.Z == 0 {
		return x.Blocks(p.X, p.Y, mask)
	}
	if !x.InBounds(p) {
		return false
	}
	return x.planeFor(mask).Bit(p.X, x.row(p))
}

// BlocksArea reports whether a cell of [x, x+width) × [y, y+height) of the first
// layer is blocked for mask, the cells outside the map passed over.
func (x *Index[E]) BlocksArea(px, py, width, height int, mask Mask) bool {
	x0, y0 := max(px, 0), max(py, 0)
	x1, y1 := min(px+width, x.Width), min(py+height, x.Height)
	if x0 >= x1 || y0 >= y1 {
		return false
	}
	if x.unmerged {
		x.merge()
	}
	for i := range x.solid {
		if m := &x.solid[i]; m.stops(mask) && m.bits.Any(x0, x1, y0, y1) {
			return true
		}
	}
	return x.planeFor(mask).Any(x0, x1, y0, y1)
}

// BlocksBox reports whether a cell of the box from p, width×height×depth cells, is
// blocked for mask, the cells outside the volume passed over.
func (x *Index[E]) BlocksBox(p Point, width, height, depth int, mask Mask) bool {
	x0, y0, z0 := max(p.X, 0), max(p.Y, 0), max(p.Z, 0)
	x1, y1, z1 := min(p.X+width, x.Width), min(p.Y+height, x.Height), min(p.Z+depth, x.Depth)
	if x0 >= x1 || y0 >= y1 || z0 >= z1 {
		return false
	}
	if z0 == 0 && x.BlocksArea(x0, y0, x1-x0, y1-y0, mask) {
		return true
	}
	plane := x.planeFor(mask)
	for z := max(z0, 1); z < z1; z++ {
		if plane.Any(x0, x1, z*x.Height+y0, z*x.Height+y1) {
			return true
		}
	}
	return false
}

// blockedCells counts the cells of an area blocked for mask
func (x *Index[E]) blockedCells(px, py, width, height int, mask Mask) int {
	n := 0
	for row := py; row < py+height; row++ {
		for col := px; col < px+width; col++ {
			if x.Blocks(col, row, mask) {
				n++
			}
		}
	}
	return n
}

// BlockBits returns Blocks for mask as a grid of bits in buf's storage: the
// plane's cells and the surfaces' solid sides. It is exact while blocks cannot
// change.
func (x *Index[E]) BlockBits(mask Mask, buf []uint64) Bits {
	p := x.planeFor(mask)
	b := Bits{Words: append(buf[:0], p.Words...), Stride: p.Stride}
	if x.unmerged {
		x.merge()
	}
	for i := range x.solid {
		if m := &x.solid[i]; m.stops(mask) {
			for j, w := range m.bits.Words {
				b.Words[j] |= w
			}
		}
	}
	return b
}

// BlockChangedAt logs p as a cell whose block answer may have moved, a block
// placed, moved or removed there or its mask rewritten, and brings the planes to
// what the cell holds now.
func (x *Index[E]) BlockChangedAt(p Point) {
	x.log.add(p, len(x.Cells))
	if !x.stale && x.InBounds(p) {
		x.update(x.row(p)*x.Width + p.X)
	}
}

// BlocksChanged loses the log for blocks rewritten wholesale, an install's, since
// it names no cells, and marks the planes for rebuilding.
func (x *Index[E]) BlocksChanged() {
	x.log.lose()
	x.stale = true
}

// BlockChanges returns every cell whose Blocks may answer otherwise than it did at
// sequence since, and the sequence it stands at now, for a reader that keeps its
// own copy of the answers to re-test only those cells. A cell is named once per
// change, and may be off the map. ok is false when the log no longer reaches back
// to since, or since is zero: the reader then re-reads every cell. The cells are
// the log's, to read before blocks change again; entries before since are
// dropped, so a second reader of one index re-reads more often.
func (x *Index[E]) BlockChanges(since uint64) (cells []vmath.Point, now uint64, ok bool) {
	l := &x.log
	if l.base == 0 {
		l.base = 1
	}
	now = l.base + uint64(len(l.cells))
	if since < l.base || since > now {
		return nil, now, false
	}
	if read := int(since - l.base); read > 0 {
		l.cells = l.cells[:copy(l.cells, l.cells[read:])]
		l.base = since
	}
	return l.cells, now, true
}

// changeLog is the cells whose block answer may have moved since the sequence a
// reader last read at: each cell a block left or entered, a block rewritten in
// place, a surface's height crossed. A change naming no cells loses it, as do
// more cells than re-testing would beat re-reading the grid; a reader from before
// then re-reads.
type changeLog struct {
	cells []vmath.Point
	base  uint64 // the sequence before cells[0]; zero is no sequence read
}

// add logs one cell, losing the log past an eighth of the grid's cells
func (l *changeLog) add(p vmath.Point, cells int) {
	if l.base == 0 {
		l.base = 1
	}
	if len(l.cells) > cells/8+64 {
		l.lose()
		return
	}
	l.cells = append(l.cells, p)
}

// lose forgets every cell, leaving each reader before now to re-read
func (l *changeLog) lose() {
	l.base += uint64(len(l.cells)) + 1
	l.cells = l.cells[:0]
}
