// Package position is a map's spatial index and the geometry over it: a dense grid
// of cells holding what stands in each, the walls among them kept as a bit a cell
// for each mask asked, the rock no entity holds, the log of the cells whose wall
// answer changed, and the searches a placement, a line or a ray makes over them.
// It knows no game: an entity is any comparable value, and its class, which
// occupant is a wall and the cell aspect come from the caller.
package position

import (
	"github.com/lixenwraith/vif/pkg/vmath"
)

// Frame is the map's frame, read live: the cell aspect the searches round on. The
// bounds every query answers within are the grid's.
type Frame interface {
	Aspect() vmath.Aspect
}

// Mask selects the kinds of mover a wall stops; a query's 0 asks for any wall.
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

// Index is one map's spatial index: its grid, the rock, the caller's word on which
// shared occupant is a wall and what it stops, a plane of bits for each mask a
// query asked, kept with every wall change, the log of the cells whose wall answer
// changed, and the searches over them.
// SYNC: the caller serialises every call; the index locks nothing.
type Index[E comparable] struct {
	*Grid[E]
	frame  Frame
	wallOf func(e E) (mask Mask, wall bool)
	rock   *Rock
	log    changeLog // the cells each wall change can have moved, for WallChanges

	// planes holds one plane per mask a query asked, planeOf naming it, and rockBits
	// the rock's cells; stale marks them all for rebuilding from the cells and the
	// columns before the next query.
	planes   []plane
	planeOf  [256]int8
	rockBits Bits
	stale    bool
}

// plane is the cells a wall stands in for one mask
type plane struct {
	mask Mask
	Bits
}

// New makes an empty index over frame; wallOf says whether a shared occupant is a
// wall and which movers it stops.
func New[E comparable](frame Frame, wallOf func(e E) (Mask, bool)) *Index[E] {
	// The grid is sized by the caller to the map it indexes: a ceiling-sized one
	// costs 32 MB, which an instance per bot multiplies.
	x := &Index[E]{Grid: NewGrid[E](0, 0), frame: frame, wallOf: wallOf}
	for i := range x.planeOf {
		x.planeOf[i] = -1
	}
	return x
}

// Resize sizes the grid to w×h, emptying it, and loses the log, since the cells
// moved; the caller places its entities again, and the planes follow.
func (x *Index[E]) Resize(w, h int) {
	x.Grid.Resize(w, h)
	x.log.lose()
	x.stale = true
}

// Clear empties the grid, lifts the rock and loses the log.
func (x *Index[E]) Clear() {
	x.Grid.Clear()
	x.SetRock(0, 0, nil)
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
	x.planes = append(x.planes, plane{mask: mask, Bits: NewBits(x.Width, x.Height)})
	p := &x.planes[len(x.planes)-1]
	for _, i := range x.occupied {
		if m, wall := x.cellWall(int(i)); wall && (mask == 0 || m&mask != 0) {
			p.Set(int(i)%x.Width, int(i)/x.Width)
		}
	}
	return p
}

// rebuild sets every plane from the cells and the rock's bits from its columns
func (x *Index[E]) rebuild() {
	x.stale = false
	stride := Stride(x.Width)
	for i := range x.planes {
		x.planes[i].Bits = x.planes[i].Bits.sized(stride, x.Height)
	}
	for _, i := range x.occupied {
		x.update(int(i))
	}
	x.layRock()
}

// cellWall is what the shared occupants of cell i make of it: the masks of its
// walls, and whether one stands there.
func (x *Index[E]) cellWall(i int) (mask Mask, wall bool) {
	cell := &x.Cells[i]
	for _, e := range cell.Entities[:cell.SharedCount] {
		if m, w := x.wallOf(e); w {
			mask, wall = mask|m, true
		}
	}
	return mask, wall
}

// update brings every plane's bit for cell i to its occupants
func (x *Index[E]) update(i int) {
	if len(x.planes) == 0 {
		return
	}
	mask, wall := x.cellWall(i)
	px, py := i%x.Width, i/x.Width
	for p := range x.planes {
		x.planes[p].Put(px, py, wall && (x.planes[p].mask == 0 || mask&x.planes[p].mask != 0))
	}
}

// rockStops reports whether the rock standing stops mask
func (x *Index[E]) rockStops(mask Mask) bool {
	return x.rock != nil && (mask == 0 || x.rock.Mask&mask != 0)
}

// Blocks reports whether a wall stands at (x, y) for mask: the rock's, or a
// shared occupant wallOf names. Outside the map nothing does.
func (x *Index[E]) Blocks(px, py int, mask Mask) bool {
	if px < 0 || px >= x.Width || py < 0 || py >= x.Height {
		return false
	}
	if x.stale {
		x.rebuild()
	}
	if x.rockStops(mask) && x.rockBits.Bit(px, py) {
		return true
	}
	return x.planeFor(mask).Bit(px, py)
}

// BlocksArea reports whether a wall stands in [x, x+width) × [y, y+height) for
// mask, the cells outside the map passed over.
func (x *Index[E]) BlocksArea(px, py, width, height int, mask Mask) bool {
	x0, y0 := max(px, 0), max(py, 0)
	x1, y1 := min(px+width, x.Width), min(py+height, x.Height)
	if x0 >= x1 || y0 >= y1 {
		return false
	}
	if x.stale {
		x.rebuild()
	}
	if x.rockStops(mask) && x.rockBits.Any(x0, x1, y0, y1) {
		return true
	}
	return x.planeFor(mask).Any(x0, x1, y0, y1)
}

// blockedCells counts the cells of an area that walls block for mask
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

// WallBits returns Blocks for mask as a grid of bits in buf's storage: the plane's
// cells and the rock's. It is exact while walls cannot change.
func (x *Index[E]) WallBits(mask Mask, buf []uint64) Bits {
	p := x.planeFor(mask)
	b := Bits{Words: append(buf[:0], p.Words...), Stride: p.Stride}
	if x.rockStops(mask) {
		for i, w := range x.rockBits.Words {
			b.Words[i] |= w
		}
	}
	return b
}

// WallChangedAt logs (x, y) as a cell whose wall answer may have moved, a wall
// placed, moved or removed there or its mask rewritten, and brings the planes to
// what the cell holds now.
func (x *Index[E]) WallChangedAt(px, py int) {
	x.log.add(px, py, len(x.Cells))
	if !x.stale && px >= 0 && px < x.Width && py >= 0 && py < x.Height {
		x.update(py*x.Width + px)
	}
}

// WallsChanged loses the log for walls rewritten wholesale, an install's, since it
// names no cells, and marks the planes for rebuilding.
func (x *Index[E]) WallsChanged() {
	x.log.lose()
	x.stale = true
}

// WallChanges returns every cell whose Blocks may answer otherwise than it did at
// sequence since, and the sequence it stands at now, for a reader that keeps its
// own copy of the answers to re-test only those cells. A cell is named once per
// change, and may be off the map. ok is false when the log no longer reaches back
// to since, or since is zero: the reader then re-reads every cell. The cells are
// the log's, to read before walls change again; entries before since are dropped,
// so a second reader of one index re-reads more often.
func (x *Index[E]) WallChanges(since uint64) (cells []vmath.Point, now uint64, ok bool) {
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

// changeLog is the cells whose wall answer may have moved since the sequence a
// reader last read at: each cell a wall left or entered, a wall rewritten in
// place, a rock column turned. A change naming no cells loses it, as do more
// cells than re-testing would beat re-reading the grid; a reader from before then
// re-reads.
type changeLog struct {
	cells []vmath.Point
	base  uint64 // the sequence before cells[0]; zero is no sequence read
}

// add logs one cell, losing the log past an eighth of the grid's cells
func (l *changeLog) add(x, y, cells int) {
	if l.base == 0 {
		l.base = 1
	}
	if len(l.cells) > cells/8+64 {
		l.lose()
		return
	}
	l.cells = append(l.cells, vmath.Point{X: x, Y: y})
}

// lose forgets every cell, leaving each reader before now to re-read
func (l *changeLog) lose() {
	l.base += uint64(len(l.cells)) + 1
	l.cells = l.cells[:0]
}
