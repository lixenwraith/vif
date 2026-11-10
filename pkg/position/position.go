// Package position is a map's spatial index and the geometry over it: a dense grid
// of cells, a map or a volume of layers, holding what stands in each; the blocks
// among them, a bit a cell for each mask asked; the surfaces no entity holds; the
// log of block changes; and the searches over them. It knows no game: an entity is
// any comparable value, its class, its blocks and the cell aspect the caller's.
package position

import (
	"unsafe"

	"github.com/lixenwraith/vif/pkg/vmath"
)

// Point is a cell's coordinate on a flat map, vmath's, and Point3 a cell's in a
// volume, its Z the layer.
type (
	Point  = vmath.Point
	Point3 = vmath.Point3
)

// Coord is the coordinate a query takes: a Point, a cell of the first layer, or a
// Point3, a cell of any. Region is a block of cells, an Area of the first layer
// or a Box, and Body a body's cells around its header, a Footprint one layer deep
// or a Footprint3.
type (
	Coord  interface{ Point | Point3 }
	Region interface{ vmath.Area | vmath.Box }
	Body   interface {
		vmath.Footprint | vmath.Footprint3
	}
)

// A query tells the flat form of a coordinate, a region or a body from the
// volume's by its size, so the two of each kind must differ in it.
const _ = 1/(unsafe.Sizeof(Point3{})-unsafe.Sizeof(Point{})) +
	1/(unsafe.Sizeof(vmath.Box{})-unsafe.Sizeof(vmath.Area{})) +
	1/(unsafe.Sizeof(vmath.Footprint3{})-unsafe.Sizeof(vmath.Footprint{}))

// at is p's cell in the volume, a flat point's on the first layer, and whether p
// is flat: a constant in each instantiation, so a query reads a flat point as it
// would two ints and its volume half compiles away.
func at[P Coord](p P) (Point3, bool) {
	if unsafe.Sizeof(p) == unsafe.Sizeof(Point3{}) {
		return *(*Point3)(unsafe.Pointer(&p)), false
	}
	return (*Point)(unsafe.Pointer(&p)).Layer(0), true
}

// as is cell c as a P: itself, or its column and row
func as[P Coord](c Point3) (p P) {
	if unsafe.Sizeof(p) == unsafe.Sizeof(c) {
		*(*Point3)(unsafe.Pointer(&p)) = c
	} else {
		*(*Point)(unsafe.Pointer(&p)) = c.Flat()
	}
	return p
}

// box is r's columns and rows, the first of its layers and how many they are, an
// area the first layer alone, and whether r is flat. A box's six fields would
// live in memory, where four stay in registers, so it comes back in parts.
func box[R Region](r R) (a vmath.Area, z, depth int, flat bool) {
	if unsafe.Sizeof(r) == unsafe.Sizeof(vmath.Box{}) {
		b := (*vmath.Box)(unsafe.Pointer(&r))
		return vmath.Area{X: b.X, Y: b.Y, Width: b.Width, Height: b.Height}, b.Z, b.Depth, false
	}
	return *(*vmath.Area)(unsafe.Pointer(&r)), 0, 1, true
}

// body is f's columns and rows around its header, its header's layer among its
// own and how many they are, a flat footprint one layer deep, and whether f is
// flat; in parts, as box
func body[F Body](f F) (foot vmath.Footprint, headerZ, depth int, flat bool) {
	if unsafe.Sizeof(f) == unsafe.Sizeof(vmath.Footprint3{}) {
		b := (*vmath.Footprint3)(unsafe.Pointer(&f))
		return vmath.Footprint{Width: b.Width, Height: b.Height, HeaderX: b.HeaderX, HeaderY: b.HeaderY}, b.HeaderZ, b.Depth, false
	}
	return *(*vmath.Footprint)(unsafe.Pointer(&f)), 0, 1, true
}

// Frame is the map's frame, read live: the cell aspect the searches round on. The
// bounds every query answers within are the grid's.
type Frame interface {
	Aspect() vmath.Aspect
}

// Mask selects the kinds of block a query asks for, the kinds blockOf names: in a
// game the movers a block stops, and whatever else tells its blocks apart. A
// query's 0 asks for any block.
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
	solid     []solid   // the surfaces merged, one entry a mask, unless stale lays them again
	aheadFrom int       // the first column past a surface, where its heights ahead answer
	log       changeLog // the cells each block change can have moved, for BlockChanges

	// planes holds one plane per mask a query asked, and fresh names each while
	// nothing waits on it: stale is the work a probe of a plane not fresh does
	// first, so a probe tests one pointer for all of it.
	planes []plane
	fresh  [256]*plane
	stale  uint8
}

// What a probe brings current before it answers
const (
	stalePlanes   uint8 = 1 << iota // every plane, rebuilt from the cells
	staleSurfaces                   // the merged surfaces, laid again from the heights
	staleShift                      // the same, while a surface is a column ahead of its mask and layer
)

// spoil leaves work for the next probe of each plane
func (x *Index[E]) spoil(work uint8) {
	x.stale |= work
	x.fresh = [256]*plane{}
}

// plane is the cells a block stands in for one mask, and the bits of the merged
// surfaces that stop it, which a probe reads with them
type plane struct {
	mask Mask
	Bits
	solid [][]uint64
}

// word is word w of the plane's bits with the merged surfaces' over them
func (p *plane) word(w int) uint64 {
	word := p.Words[w]
	for _, s := range p.solid {
		word |= s[w]
	}
	return word
}

// blocked reports cell px of row, which the caller has bounded: a block's bit, or a
// merged surface's
func (p *plane) blocked(px, row int) bool {
	return p.word(row*p.Stride+px>>6)>>(uint(px)&63)&1 != 0
}

// any reports whether a cell of [x0, x1) × the rows [r0, r1), bounded by the caller
// and non-empty, is blocked
func (p *plane) any(x0, x1, r0, r1 int) bool {
	w0, w1 := x0>>6, (x1-1)>>6
	first, last := ^uint64(0)<<(uint(x0)&63), ^uint64(0)>>(63-uint(x1-1)&63)
	for at := r0 * p.Stride; at < r1*p.Stride; at += p.Stride {
		for w := at + w0; w <= at+w1; w++ {
			word := p.word(w)
			if w == at+w0 {
				word &= first
			}
			if w == at+w1 {
				word &= last
			}
			if word != 0 {
				return true
			}
		}
	}
	return false
}

// deep is any on each of n layers, pitch rows apart
func (p *plane) deep(x0, x1, r0, r1, n, pitch int) bool {
	for ; n > 0; n, r0, r1 = n-1, r0+pitch, r1+pitch {
		if p.any(x0, x1, r0, r1) {
			return true
		}
	}
	return false
}

// New makes an empty index over frame; blockOf says whether a shared occupant is
// a block and which movers it stops.
func New[E comparable](frame Frame, blockOf func(e E) (Mask, bool)) *Index[E] {
	return &Index[E]{Grid: NewGrid[E](0, 0, 1), frame: frame, blockOf: blockOf}
}

// Resize sizes the grid to w×h×d, emptying it, and loses the log, since the cells
// moved; the caller places its entities again, and the planes follow.
func (x *Index[E]) Resize(w, h, d int) {
	x.Grid.Resize(w, h, d)
	x.log.lose()
	x.spoil(stalePlanes | staleSurfaces)
}

// Clear empties the grid, lifts every surface and loses the log.
func (x *Index[E]) Clear() {
	x.Grid.Clear()
	x.surfaces = x.surfaces[:0]
	x.widths()
	x.log.lose()
	x.spoil(stalePlanes | staleSurfaces)
}

// probe returns mask's plane, current with the cells and the surfaces
func (x *Index[E]) probe(mask Mask) *plane {
	if p := x.fresh[mask]; p != nil {
		return p
	}
	return x.planeFor(mask)
}

// planeFor does what stale names and returns mask's plane, built from the cells
// on its first use
func (x *Index[E]) planeFor(mask Mask) *plane {
	if x.stale&(staleSurfaces|staleShift) != 0 {
		x.merge()
	}
	if x.stale&stalePlanes != 0 {
		x.rebuild()
	}
	for i := range x.planes {
		if p := &x.planes[i]; p.mask == mask {
			x.fresh[mask] = p
			return p
		}
	}
	x.fresh = [256]*plane{} // the planes may move as they grow
	x.planes = append(x.planes, plane{mask: mask, Bits: NewBits(x.Width, x.Height*x.Depth)})
	p := &x.planes[len(x.planes)-1]
	x.bind(p)
	x.each(ScopeShared, func(px, row int) {
		if m, block := x.cellBlock(row*x.Width + px); block && (mask == 0 || m&mask != 0) {
			p.Set(px, row)
		}
	})
	x.fresh[mask] = p
	return p
}

// bind gives p the bits of the merged surfaces that stop its mask
func (x *Index[E]) bind(p *plane) {
	p.solid = p.solid[:0]
	for i := range x.solid {
		if m := &x.solid[i]; m.stops(p.mask) {
			p.solid = append(p.solid, m.bits.Words)
		}
	}
}

// rebuild sets every plane from the cells
func (x *Index[E]) rebuild() {
	x.stale &^= stalePlanes
	stride := Stride(x.Width)
	for i := range x.planes {
		x.planes[i].Bits = x.planes[i].Bits.sized(stride, x.Height*x.Depth)
	}
	if len(x.planes) > 0 {
		x.each(ScopeShared, x.update)
	}
}

// cellBlock is what the shared occupants of cell i make of it: the masks of its
// blocks, and whether one stands there.
func (x *Index[E]) cellBlock(i int) (mask Mask, block bool) {
	for _, e := range x.view(&x.cells[i], ScopeShared) {
		if m, b := x.blockOf(e); b {
			mask, block = mask|m, true
		}
	}
	return mask, block
}

// update brings every plane's bit for cell px of row to its occupants
func (x *Index[E]) update(px, row int) {
	if len(x.planes) == 0 {
		return
	}
	mask, block := x.cellBlock(row*x.Width + px)
	w, bit := row*x.planes[0].Stride+px>>6, uint64(1)<<(uint(px)&63)
	for i := range x.planes {
		if p := &x.planes[i]; block && (p.mask == 0 || mask&p.mask != 0) {
			p.Words[w] |= bit
		} else {
			p.Words[w] &^= bit
		}
	}
}

// Blocks reports whether p's cell is blocked for mask: a surface's solid side, or
// a shared occupant blockOf names. Outside the map nothing is.
func (x *Index[E]) Blocks[P Coord](p P, mask Mask) bool {
	c, flat := at(p)
	if !x.in(c, flat) {
		return false
	}
	return x.probe(mask).blocked(c.X, x.row(c, flat))
}

// BlocksArea reports whether a cell of r is blocked for mask, the cells outside
// the map passed over.
func (x *Index[E]) BlocksArea[R Region](r R, mask Mask) bool {
	a, z, depth, flat := box(r)
	x0, y0 := max(a.X, 0), max(a.Y, 0)
	x1, y1 := min(a.X+a.Width, x.Width), min(a.Y+a.Height, x.Height)
	if x0 >= x1 || y0 >= y1 {
		return false
	}
	plane := x.probe(mask)
	if flat {
		return plane.any(x0, x1, y0, y1)
	}
	z0, z1 := max(z, 0), min(z+depth, x.Depth)
	return plane.deep(x0, x1, z0*x.Height+y0, z0*x.Height+y1, z1-z0, x.Height)
}

// clear reports whether the cells of a on depth layers from z lie whole inside
// the volume and p blocks none of them; one layer, a flat map's every body, is
// one probe
func (x *Index[E]) clear(p *plane, a vmath.Area, z, depth int) bool {
	if a.X < 0 || a.Y < 0 || z < 0 || a.X+a.Width > x.Width || a.Y+a.Height > x.Height || z+depth > x.Depth {
		return false
	}
	if a.Width <= 0 || a.Height <= 0 {
		return true
	}
	row := z*x.Height + a.Y
	if depth == 1 {
		return !p.any(a.X, a.X+a.Width, row, row+a.Height)
	}
	return !p.deep(a.X, a.X+a.Width, row, row+a.Height, depth, x.Height)
}

// blockedCells counts the cells of a on z, a layer of the volume, blocked for
// mask, a cell outside the map being none
func (x *Index[E]) blockedCells(a vmath.Area, z int, mask Mask) int {
	p, n := x.probe(mask), 0
	for row := max(a.Y, 0); row < min(a.Y+a.Height, x.Height); row++ {
		for col := max(a.X, 0); col < min(a.X+a.Width, x.Width); col++ {
			if p.blocked(col, z*x.Height+row) {
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
	p := x.probe(mask)
	b := Bits{Words: append(buf[:0], p.Words...), Stride: p.Stride}
	for _, s := range p.solid {
		for j, w := range s {
			b.Words[j] |= w
		}
	}
	return b
}

// BlockChangedAt logs p as a cell whose block answer may have moved, a block
// placed, moved or removed there or its mask rewritten, and brings the planes to
// what the cell holds now.
func (x *Index[E]) BlockChangedAt[P Coord](p P) {
	c, flat := at(p)
	x.log.add(c, len(x.cells))
	if x.stale&stalePlanes == 0 && x.in(c, flat) {
		x.update(c.X, x.row(c, flat))
	}
}

// BlocksChanged loses the log for blocks rewritten wholesale, an install's, since
// it names no cells, and marks the planes for rebuilding.
func (x *Index[E]) BlocksChanged() {
	x.log.lose()
	x.spoil(stalePlanes)
}

// BlockChanges returns the cells whose Blocks may answer otherwise than at sequence
// since, each named once a change and maybe off the map, and the sequence now, for
// a reader re-testing only those. ok is false when the log no longer reaches since
// or since is zero: the reader re-reads every cell. The cells are the log's, read
// before blocks change; entries before since go, so a second reader re-reads more.
func (x *Index[E]) BlockChanges(since uint64) (cells []vmath.Point3, now uint64, ok bool) {
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
	cells []vmath.Point3
	base  uint64 // the sequence before cells[0]; zero is no sequence read
}

// add logs one cell, losing the log past an eighth of the grid's cells
func (l *changeLog) add(p vmath.Point3, cells int) {
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
