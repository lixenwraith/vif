// Package position is a map's spatial index and the geometry over it: a dense grid
// of cells holding what stands in each, the walls among them kept as a bit a cell
// for each mask asked, the rock no entity holds, the log of the cells whose wall
// answer changed, and the searches a placement, a line or a ray makes over them.
// It knows no game: an entity is any comparable value, and its class, which
// occupant is a wall and the cell aspect come from the caller.
package position

import (
	"math/bits"

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

	// planes holds one plane per mask a query asked, planeOf naming it; stale
	// marks them all for rebuilding from the cells before the next query.
	planes  []plane
	planeOf [256]int8
	stride  int // words a row
	stale   bool
}

// plane is the cells a wall stands in for one mask, a bit a cell, each row
// starting on a word so an area is a few words a row.
type plane struct {
	mask Mask
	bits []uint64
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
	x.stride = (x.Width + 63) / 64
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
	x.planes = append(x.planes, plane{mask: mask, bits: make([]uint64, x.stride*x.Height)})
	p := &x.planes[len(x.planes)-1]
	for _, i := range x.occupied {
		if m, wall := x.cellWall(int(i)); wall {
			word, bit := x.cellWord(int(i))
			p.set(word, bit, m)
		}
	}
	return p
}

// rebuild sets every plane from the cells
func (x *Index[E]) rebuild() {
	x.stale = false
	for i := range x.planes {
		p := &x.planes[i]
		if cap(p.bits) < x.stride*x.Height {
			p.bits = make([]uint64, x.stride*x.Height)
		}
		p.bits = p.bits[:x.stride*x.Height]
		clear(p.bits)
	}
	for _, i := range x.occupied {
		x.update(int(i))
	}
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

// cellWord is cell i's word in a plane and the bit in it
func (x *Index[E]) cellWord(i int) (word int, bit uint) {
	px, py := i%x.Width, i/x.Width
	return py*x.stride + px>>6, uint(px & 63)
}

// update brings every plane's bit for cell i to its occupants
func (x *Index[E]) update(i int) {
	if len(x.planes) == 0 {
		return
	}
	mask, wall := x.cellWall(i)
	word, bit := x.cellWord(i)
	for p := range x.planes {
		x.planes[p].put(word, bit, wall && (x.planes[p].mask == 0 || mask&x.planes[p].mask != 0))
	}
}

// set sets the bit of cell (word, bit) where mask names the plane's movers
func (p *plane) set(word int, bit uint, mask Mask) {
	if p.mask == 0 || mask&p.mask != 0 {
		p.bits[word] |= 1 << bit
	}
}

// put writes the bit of cell (word, bit)
func (p *plane) put(word int, bit uint, on bool) {
	if on {
		p.bits[word] |= 1 << bit
	} else {
		p.bits[word] &^= 1 << bit
	}
}

// any reports whether a bit is set in [x0, x1) × [y0, y1), which the caller has
// bounded by the grid
func (p *plane) any(stride, x0, x1, y0, y1 int) bool {
	w0, w1 := x0>>6, (x1-1)>>6
	first := ^uint64(0) << (uint(x0) & 63)
	last := ^uint64(0) >> (63 - uint(x1-1)&63)
	for y := y0; y < y1; y++ {
		row := p.bits[y*stride:]
		if w0 == w1 {
			if row[w0]&first&last != 0 {
				return true
			}
			continue
		}
		if row[w0]&first != 0 || row[w1]&last != 0 {
			return true
		}
		for w := w0 + 1; w < w1; w++ {
			if row[w] != 0 {
				return true
			}
		}
	}
	return false
}

// Blocks reports whether a wall stands at (x, y) for mask: the rock's, or a
// shared occupant wallOf names. Outside the map nothing does.
func (x *Index[E]) Blocks(px, py int, mask Mask) bool {
	if px < 0 || px >= x.Width || py < 0 || py >= x.Height {
		return false
	}
	if x.rock != nil && x.rock.blocks(px, py, mask) {
		return true
	}
	return x.planeFor(mask).bits[py*x.stride+px>>6]>>(uint(px)&63)&1 != 0
}

// BlocksArea reports whether a wall stands in [x, x+width) × [y, y+height) for
// mask, the cells outside the map passed over.
func (x *Index[E]) BlocksArea(px, py, width, height int, mask Mask) bool {
	x0, y0 := max(px, 0), max(py, 0)
	x1, y1 := min(px+width, x.Width), min(py+height, x.Height)
	if x0 >= x1 || y0 >= y1 {
		return false
	}
	if x.rock != nil && x.rock.blocksArea(x0, y0, x1, y1, mask) {
		return true
	}
	return x.planeFor(mask).any(x.stride, x0, x1, y0, y1)
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

// WallTest returns Blocks for mask as a grid copied into buf on its first call,
// reused across calls. A derivation that probes every cell several times pays one
// copy instead; it is exact while walls cannot change.
func (x *Index[E]) WallTest(mask Mask, buf *[]bool) func(px, py int) bool {
	var w, h int
	built := false
	return func(px, py int) bool {
		if !built {
			w, h = x.Width, x.Height
			*buf = x.walls(mask, (*buf)[:0])
			built = true
		}
		if px < 0 || py < 0 || px >= w || py >= h {
			return false
		}
		return (*buf)[py*w+px]
	}
}

// walls appends mask's grid of Blocks to buf: the plane's bits, then the rock
func (x *Index[E]) walls(mask Mask, buf []bool) []bool {
	w, h := x.Width, x.Height
	if cap(buf) < w*h {
		buf = make([]bool, w*h)
	}
	buf = buf[:w*h]
	clear(buf)
	p := x.planeFor(mask)
	for py := range h {
		row := buf[py*w : py*w+w]
		for wi, word := range p.bits[py*x.stride : py*x.stride+x.stride] {
			for word != 0 {
				row[wi*64+bits.TrailingZeros64(word)] = true
				word &= word - 1
			}
		}
	}
	if r := x.rock; r != nil && (mask == 0 || r.Mask&mask != 0) {
		r.mark(w, h, buf)
	}
	return buf
}
