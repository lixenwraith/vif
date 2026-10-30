// Package position is a map's spatial index and the geometry over it: a dense grid
// of cells holding what stands in each, blockers answering what each cell stops for
// a mask, the log of the cells whose answer changed, and the searches a placement,
// a line or a ray makes over them. It knows no game: an entity is any comparable
// value, and its class, the map's bounds and its blockers come from the caller.
package position

import (
	"github.com/lixenwraith/vif/pkg/vmath"
)

// Frame is the map an index serves, read live: the bounds every query answers
// within, which the grid may differ from for a tick, and the cell aspect the
// searches round on.
type Frame interface {
	Bounds() (w, h int)
	Aspect() vmath.Aspect
}

// Mask selects the kinds of mover a blocker stops; a query's 0 asks for any.
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

// Blocker is one source of blocked cells. Blocks and BlocksArea answer for mask, 0
// being any, over cells the index has bounded by the map; Mark sets cells[y*w+x]
// for every cell it blocks within w×h.
type Blocker interface {
	Blocks(x, y int, mask Mask) bool
	BlocksArea(x, y, width, height int, mask Mask) bool
	Mark(mask Mask, w, h int, cells []bool)
}

// Index is one map's spatial index: its grid, its frame, its blockers, the epoch
// and log of the cells whose wall answer changed, and the searches over them.
// SYNC: the caller serialises every call; the index locks nothing.
type Index[E comparable] struct {
	*Grid[E]
	frame    Frame
	blockers []Blocker
	rock     *Rock
	// epoch moves wherever a wall test's cells can change: a blocker's cell changed,
	// a rock laid or shifted.
	epoch uint64
	log   changeLog   // the cells each such change can have moved, for WallChanges
	tests []*maskGrid // WallTest's grid for each mask asked
}

// New makes an empty index over frame; the blockers answer after the rock.
func New[E comparable](frame Frame, blockers ...Blocker) *Index[E] {
	// The grid is sized by the caller to the map it indexes: a ceiling-sized one
	// costs 32 MB, which an instance per bot multiplies.
	return &Index[E]{Grid: NewGrid[E](0, 0), frame: frame, blockers: blockers}
}

// Resize sizes the grid to w×h, emptying it, and loses the log, since the cells
// moved; the caller places its entities again.
func (x *Index[E]) Resize(w, h int) {
	x.Grid.Resize(w, h)
	x.log.lose()
}

// Clear empties the grid, lifts the rock and loses the log.
func (x *Index[E]) Clear() {
	x.Grid.Clear()
	x.SetRock(0, 0, nil)
	x.log.lose()
}

// Blocks reports whether a wall stands at (x, y) for mask: the rock's, then a
// blocker's. Outside the map, or the grid, nothing does.
func (x *Index[E]) Blocks(px, py int, mask Mask) bool {
	w, h := x.frame.Bounds()
	return x.blocks(px, py, w, h, mask)
}

// blocks is Blocks with the bounds read once for a walk
func (x *Index[E]) blocks(px, py, w, h int, mask Mask) bool {
	if px < 0 || px >= w || py < 0 || py >= h || px >= x.Width || py >= x.Height {
		return false
	}
	if x.rock != nil && x.rock.blocks(px, py, mask) {
		return true
	}
	for _, b := range x.blockers {
		if b.Blocks(px, py, mask) {
			return true
		}
	}
	return false
}

// BlocksArea reports whether a wall stands in [x, x+width) × [y, y+height) for
// mask, the cells outside the map passed over.
func (x *Index[E]) BlocksArea(px, py, width, height int, mask Mask) bool {
	if x.rock != nil {
		w, h := x.wallBounds()
		if x.rock.blocksArea(px, max(py, 0), min(px+width, w), min(py+height, h), mask) {
			return true
		}
	}
	for _, b := range x.blockers {
		if b.BlocksArea(px, py, width, height, mask) {
			return true
		}
	}
	return false
}

// blockedCells counts the cells of an area that walls block for mask
func (x *Index[E]) blockedCells(px, py, width, height int, mask Mask) int {
	w, h := x.frame.Bounds()
	n := 0
	for row := py; row < py+height; row++ {
		for col := px; col < px+width; col++ {
			if x.blocks(col, row, w, h, mask) {
				n++
			}
		}
	}
	return n
}

// WallChangedAt logs (x, y) as a cell whose wall answer may have moved: a wall
// placed, moved or removed there, or its mask rewritten.
func (x *Index[E]) WallChangedAt(px, py int) {
	x.epoch++
	x.log.add(px, py, len(x.Cells))
}

// WallsChanged moves the epoch for walls rewritten wholesale, an install's, and
// loses the log, since it names no cells.
func (x *Index[E]) WallsChanged() {
	x.epoch++
	x.log.lose()
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
			g := x.testGrid(mask)
			w, h, *buf = g.w, g.h, append((*buf)[:0], g.cells...)
			built = true
		}
		if px < 0 || py < 0 || px >= w || py >= h {
			return false
		}
		return (*buf)[py*w+px]
	}
}

// maskGrid is the cells Blocks answers true for under one mask, at the bounds,
// epoch and grid generation it was built at.
type maskGrid struct {
	mask         Mask
	w, h         int
	epoch, built uint64
	cells        []bool
}

// testGrid returns mask's grid, rebuilt only when the walls or the grid changed.
func (x *Index[E]) testGrid(mask Mask) *maskGrid {
	var g *maskGrid
	for _, c := range x.tests {
		if c.mask == mask {
			g = c
		}
	}
	if g == nil {
		g = &maskGrid{mask: mask, w: -1}
		x.tests = append(x.tests, g)
	}
	w, h := x.wallBounds()
	if g.w == w && g.h == h && g.epoch == x.epoch && g.built == x.generation {
		return g
	}
	g.w, g.h, g.epoch, g.built = w, h, x.epoch, x.generation
	if cap(g.cells) < w*h {
		g.cells = make([]bool, w*h)
	}
	g.cells = g.cells[:w*h]
	clear(g.cells)
	for _, b := range x.blockers {
		b.Mark(mask, w, h, g.cells)
	}
	if r := x.rock; r != nil && (mask == 0 || r.Mask&mask != 0) {
		r.mark(w, h, g.cells)
	}
	return g
}

// wallBounds is the part of the grid a wall test covers: the map, where the grid holds it.
func (x *Index[E]) wallBounds() (w, h int) {
	w, h = x.frame.Bounds()
	w, h = min(w, x.Width), min(h, x.Height)
	return max(w, 0), max(h, 0)
}
