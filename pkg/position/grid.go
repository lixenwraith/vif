package position

import (
	"errors"
	"math/bits"
	"slices"
)

// CellCapacity is the most occupants a cell holds, which keeps a crowd of
// eight-byte entities at 256 bytes, four cache lines.
const CellCapacity = 31

// PrivateBudget caps the private half of a cell so a pile of one instance's own
// effects can never consume the slots a shared entity needs.
const PrivateBudget = 12

// Map bounds, a clamp rather than a rejection: a replicated map setup must reach
// the same bounds on every instance, and the product reaches make(). A grid is
// sized to its map at 16 bytes a cell, so the ceiling costs 2 MB.
const (
	// MaxWidth is the widest map, in cells.
	MaxWidth = 2000

	// MaxHeight is the tallest map, in cells.
	MaxHeight = 2000

	// MaxCells is the most cells a map or a volume may hold, whatever its shape.
	MaxCells = 500 * 250
)

// cell is one grid cell: what it holds, and its occupant while that is one, as in
// play all but a few cells in a hundred holding anything do.
type cell[E comparable] struct {
	tag int32
	one [1]E
}

// What a cell's tag says it holds; from holdsCrowd up, its place among the crowds.
const (
	holdsNone    int32 = iota
	holdsShared        // one shared occupant, in one
	holdsPrivate       // one private occupant, in one
	holdsCrowd         // two or more, in crowds[tag-holdsCrowd]
)

// crowd is the occupants of cell at while it holds two or more: the shared ones in
// held[:shared], the private ones in held[shared:count].
type crowd[E comparable] struct {
	count, shared uint8
	_             [2]byte
	at            int32
	held          [CellCapacity]E
}

// view returns the occupants a scope selects; the partition makes every scope one
// contiguous range.
func (k *crowd[E]) view(scope Scope) []E {
	switch scope & ScopeAll {
	case ScopeShared:
		return k.held[:k.shared]
	case ScopePrivate:
		return k.held[k.shared:k.count]
	case ScopeAll:
		return k.held[:k.count]
	}
	return nil
}

// Clamp reduces a requested map to the largest legal one, so an oversized request
// is played rather than refused, and alike on every instance a replicated setup
// reaches. The clamp is per axis first and then on the product, so an extreme
// aspect is cut on the axis that is wrong rather than reshaped. Zero or negative
// dimensions return unchanged: they are a caller's "the current bounds" sentinel.
func Clamp(width, height int) (int, int) {
	if width <= 0 || height <= 0 {
		return width, height
	}
	width = min(width, MaxWidth)
	height = min(height, MaxHeight)
	// Division rather than multiplication: width is already bounded, so the
	// quotient cannot overflow where the product could.
	if width*height > MaxCells {
		height = max(MaxCells/width, 1)
	}
	return width, height
}

// ClampVolume is Clamp with a depth: the layers the cells left under MaxCells
// allow, at least one, so a map at the ceiling is one layer deep.
func ClampVolume(width, height, depth int) (int, int, int) {
	width, height = Clamp(width, height)
	depth = max(depth, 1)
	if cells := max(width, 0) * max(height, 0); cells > 0 {
		depth = min(depth, max(MaxCells/cells, 1))
	}
	return width, height, depth
}

// Grid is a dense grid for spatial queries without allocation, every cell of the
// map a cell: a volume of Depth layers, one on a flat map, each Width×Height.
type Grid[E comparable] struct {
	Width  int
	Height int
	Depth  int

	// cells is every cell, index (z*Height+y)*Width+x, and crowds the occupants of
	// the ones holding two or more, spare the places a cell that thinned out left.
	cells  []cell[E]
	crowds []crowd[E]
	spare  []int32

	// present marks the cells holding an entity of each class, a bit a cell, the
	// layers' rows in turn, so a probe passes an empty cell, a scan empty cells a
	// word at a time, and a copy, a clear and the planes read what the map holds.
	present [2]Bits

	// byCount is how many cells hold each count of occupants, [0] unread, and shared
	// the shared occupants of them all: the gauges and the crowded cells, unscanned.
	byCount [CellCapacity + 1]int32
	shared  int
}

// NewGrid makes a grid of width×height×depth cells, clamped to the ceiling.
func NewGrid[E comparable](width, height, depth int) *Grid[E] {
	g := &Grid[E]{}
	g.Resize(width, height, depth)
	return g
}

// Present is the cells holding an entity of class c, a bit a cell, the layers'
// rows in turn. Read-only.
func (g *Grid[E]) Present(c Class) Bits { return g.present[c] }

// InBounds reports whether the volume holds a cell at p.
func (g *Grid[E]) InBounds(p Point3) bool {
	return uint(p.X) < uint(g.Width) && uint(p.Y) < uint(g.Height) && uint(p.Z) < uint(g.Depth)
}

// row is the row of the presence bits and the planes that p's cell lies in
func (g *Grid[E]) row(p Point3) int { return p.Z*g.Height + p.Y }

// occupied is word w of row with a bit set for each cell holding anything scope
// selects
func (g *Grid[E]) occupied(row, w int, scope Scope) uint64 {
	var word uint64
	if scope&ScopeShared != 0 {
		word = g.present[ClassShared].Words[row*g.present[ClassShared].Stride+w]
	}
	if scope&ScopePrivate != 0 {
		word |= g.present[ClassPrivate].Words[row*g.present[ClassPrivate].Stride+w]
	}
	return word
}

// each calls fn with the column and row of every cell holding anything scope
// selects, by cell index
func (g *Grid[E]) each(scope Scope, fn func(px, row int)) {
	for row := range g.Height * g.Depth {
		for w := range g.present[ClassShared].Stride {
			for word := g.occupied(row, w, scope); word != 0; word &= word - 1 {
				fn(w<<6+bits.TrailingZeros64(word), row)
			}
		}
	}
}

// holds reports whether a cell scope selects holds anything, by the bits
func (g *Grid[E]) holds(x, y int, scope Scope) bool {
	return scope&ScopeShared != 0 && g.present[ClassShared].Bit(x, y) ||
		scope&ScopePrivate != 0 && g.present[ClassPrivate].Bit(x, y)
}

// view returns the occupants of c that scope selects, its own storage
func (g *Grid[E]) view(c *cell[E], scope Scope) []E {
	if c.tag >= holdsCrowd {
		return g.crowds[c.tag-holdsCrowd].view(scope)
	}
	if c.tag != holdsNone && scope&(1<<(c.tag-holdsShared)) != 0 {
		return c.one[:]
	}
	return nil
}

// Set inserts e into its class's half of p's cell and returns how many the cell
// then holds. O(1); 0 when out of bounds, the cell is full, or the private budget
// is spent (soft clip).
func (g *Grid[E]) Set(e E, class Class, p Point3) int {
	if !g.InBounds(p) {
		return 0
	}
	x, y := p.X, g.row(p)
	c := &g.cells[y*g.Width+x]
	if c.tag == holdsNone {
		c.tag, c.one[0] = holdsShared+int32(class), e
		g.present[class].Set(x, y)
		g.shared += int(ClassPrivate - class)
		g.byCount[1]++
		return 1
	}
	if c.tag < holdsCrowd {
		g.gather(c, y*g.Width+x)
	}
	k := &g.crowds[c.tag-holdsCrowd]
	n := k.count
	if n >= CellCapacity || class == ClassPrivate && n-k.shared >= PrivateBudget {
		return 0
	}
	if class == ClassPrivate {
		// A private insert appends inside its own budget and never touches the shared run
		if n == k.shared {
			g.present[ClassPrivate].Set(x, y)
		}
		k.held[n] = e
	} else {
		// A shared insert splits the run: the first private entity moves to the tail
		if k.shared == 0 {
			g.present[ClassShared].Set(x, y)
		}
		k.held[n] = k.held[k.shared]
		k.held[k.shared] = e
		k.shared++
		g.shared++
	}
	k.count = n + 1
	g.byCount[n]--
	g.byCount[n+1]++
	return int(n) + 1
}

// crowdFor gives cell idx a place among the crowds, a spare one or a new
func (g *Grid[E]) crowdFor(idx int) *crowd[E] {
	var place int32
	if n := len(g.spare); n > 0 {
		place, g.spare = g.spare[n-1], g.spare[:n-1]
	} else {
		place = int32(len(g.crowds))
		g.crowds = append(g.crowds, crowd[E]{})
	}
	g.cells[idx].tag = holdsCrowd + place
	k := &g.crowds[place]
	k.at = int32(idx)
	return k
}

// gather moves the one occupant of c, cell idx, into a crowd, since the cell is
// about to hold a second
func (g *Grid[E]) gather(c *cell[E], idx int) {
	shared, e := uint8(holdsPrivate-c.tag), c.one[0]
	k := g.crowdFor(idx)
	k.count, k.shared, k.held[0] = 1, shared, e
	var zero E
	c.one[0] = zero
}

// Load fills cell idx, which holds nothing, from a copy: its occupants in the
// copy's order, the first shared of them the shared ones.
func (g *Grid[E]) Load(idx int32, occupants []E, shared int) {
	switch len(occupants) {
	case 0:
		return
	case 1:
		g.cells[idx] = cell[E]{tag: holdsPrivate - int32(shared), one: [1]E{occupants[0]}}
	default:
		k := g.crowdFor(int(idx))
		k.count, k.shared = uint8(copy(k.held[:], occupants)), uint8(shared)
	}
	g.byCount[min(len(occupants), CellCapacity)]++
	g.shared += shared
	x, y := int(idx)%g.Width, int(idx)/g.Width
	g.present[ClassShared].Put(x, y, shared > 0)
	g.present[ClassPrivate].Put(x, y, len(occupants) > shared)
}

// EachOccupied calls fn with every cell holding an entity, by cell index: the
// index, its occupants, the shared ones first, and how many those are. The
// occupants are the cell's own storage, to read before it changes.
func (g *Grid[E]) EachOccupied(fn func(index int32, occupants []E, shared int)) {
	g.each(ScopeAll, func(px, row int) {
		idx := row*g.Width + px
		if c := &g.cells[idx]; c.tag < holdsCrowd {
			fn(int32(idx), c.one[:], int(holdsPrivate-c.tag))
		} else if k := &g.crowds[c.tag-holdsCrowd]; true {
			fn(int32(idx), k.held[:k.count], int(k.shared))
		}
	})
}

// RemoveEntityAt deletes e from p's cell. O(k), k the cell's occupants:
// swap-removes within e's half, refilling the shared tail from the private run.
func (g *Grid[E]) RemoveEntityAt(e E, p Point3) {
	if !g.InBounds(p) {
		return
	}
	x, y := p.X, g.row(p)
	c := &g.cells[y*g.Width+x]
	if c.tag < holdsCrowd {
		if c.tag != holdsNone && c.one[0] == e {
			g.present[c.tag-holdsShared].Clear(x, y)
			g.shared -= int(holdsPrivate - c.tag)
			g.byCount[1]--
			*c = cell[E]{}
		}
		return
	}
	k := &g.crowds[c.tag-holdsCrowd]
	n := k.count
	for i := uint8(0); i < n; i++ {
		if k.held[i] != e {
			continue
		}
		n--
		if i < k.shared {
			// Close the shared run, then pull the last private entity into the vacated shared tail
			k.shared--
			k.held[i] = k.held[k.shared]
			k.held[k.shared] = k.held[n]
			if k.shared == 0 {
				g.present[ClassShared].Clear(x, y)
			}
			g.shared--
		} else {
			k.held[i] = k.held[n]
			if n == k.shared {
				g.present[ClassPrivate].Clear(x, y)
			}
		}
		var zero E
		k.held[n] = zero
		k.count = n
		g.byCount[n+1]--
		g.byCount[n]++
		if n == 1 { // a cell of one again, its place among the crowds spare
			g.spare = append(g.spare, c.tag-holdsCrowd)
			c.tag, c.one[0] = holdsPrivate-int32(k.shared), k.held[0]
			k.count, k.held[0] = 0, zero
		}
		return
	}
}

// EntitiesAt returns a view of the entities scope selects at (x, y) of the first
// layer, nil if empty or out of bounds: the cell's own storage, to copy or to read
// before it changes.
func (g *Grid[E]) EntitiesAt(x, y int, scope Scope) []E {
	if uint(x) >= uint(g.Width) || uint(y) >= uint(g.Height) || !g.holds(x, y, scope) {
		return nil
	}
	return g.view(&g.cells[y*g.Width+x], scope)
}

// EntitiesIn is EntitiesAt for a cell of any layer.
func (g *Grid[E]) EntitiesIn(p Point3, scope Scope) []E {
	if !g.InBounds(p) || !g.holds(p.X, g.row(p), scope) {
		return nil
	}
	return g.view(&g.cells[g.row(p)*g.Width+p.X], scope)
}

// Occupants is EntitiesAt without the presence test, for a caller that knows the
// cell holds something, as one asking about an entity's own cell does.
func (g *Grid[E]) Occupants(x, y int, scope Scope) []E {
	if uint(x) >= uint(g.Width) || uint(y) >= uint(g.Height) {
		return nil
	}
	return g.view(&g.cells[y*g.Width+x], scope)
}

// HasAnyEntityAt reports whether scope selects at least one entity at (x, y) of
// the first layer. O(1)
func (g *Grid[E]) HasAnyEntityAt(x, y int, scope Scope) bool {
	return uint(x) < uint(g.Width) && uint(y) < uint(g.Height) && g.holds(x, y, scope)
}

// HasAnyEntityIn is HasAnyEntityAt for a cell of any layer.
func (g *Grid[E]) HasAnyEntityIn(p Point3, scope Scope) bool {
	return g.InBounds(p) && g.holds(p.X, g.row(p), scope)
}

// Holds reports whether the grid holds e in its class's half of p's cell, which
// tells an idempotent write from a soft-clipped entity that needs another insertion.
func (g *Grid[E]) Holds(e E, class Class, p Point3) bool {
	return slices.Contains(g.EntitiesIn(p, 1<<class), e)
}

// Clear removes every entity from every cell.
func (g *Grid[E]) Clear() { g.Resize(g.Width, g.Height, g.Depth) }

// Resize sets the grid's dimensions, a volume of newDepth layers, emptying it.
// Backing storage is grow-only: shrinking keeps capacity, so a map that oscillates
// never reallocates; growth allocates once and holds. It clamps rather than
// trusting its caller: every path into it carries dimensions from a replicated
// payload or a terminal report, and an unbounded product here panics the allocator.
func (g *Grid[E]) Resize(newWidth, newHeight, newDepth int) {
	newWidth, newHeight, newDepth = ClampVolume(newWidth, newHeight, newDepth)
	// Floored before the dimensions are stored, not just before they are used: a
	// grid recording bounds that disagree with the cells it holds answers Set and
	// every enumeration from two different maps.
	newWidth, newHeight = max(newWidth, 0), max(newHeight, 0)
	if need := newWidth * newHeight * newDepth; need <= cap(g.cells) {
		g.cells = g.cells[:need]
		clear(g.cells)
	} else {
		g.cells = make([]cell[E], need)
	}
	clear(g.crowds)
	g.crowds, g.spare = g.crowds[:0], g.spare[:0]
	g.byCount, g.shared = [CellCapacity + 1]int32{}, 0
	g.Width, g.Height, g.Depth = newWidth, newHeight, newDepth
	for c := range g.present {
		g.present[c] = g.present[c].sized(Stride(newWidth), newHeight*newDepth)
	}
}

// Stats is what the occupied cells hold.
type Stats struct {
	CellsOccupied  int
	EntitiesTotal  int
	EntitiesShared int
	MaxOccupancy   int
}

// ComputeStats reads the gauges off the counts the grid keeps.
func (g *Grid[E]) ComputeStats() Stats {
	stats := Stats{EntitiesShared: g.shared}
	for n := 1; n <= CellCapacity; n++ {
		if cells := int(g.byCount[n]); cells > 0 {
			stats.CellsOccupied += cells
			stats.EntitiesTotal += cells * n
			stats.MaxOccupancy = n
		}
	}
	return stats
}

// CellOrder is one grid cell's occupants in order, the cell named by its grid index.
type CellOrder[E comparable] struct {
	Index    int `json:"i"`
	Entities []E `json:"e"`
}

// ErrOrderMismatch refuses an order naming other occupants than the grid holds.
var ErrOrderMismatch = errors.New("the order names other entities than the world holds")

// Order reads the occupants of every cell holding two or more, by cell index.
func (g *Grid[E]) Order() []CellOrder[E] {
	var out []CellOrder[E]
	for i := range g.crowds {
		if k := &g.crowds[i]; k.count >= 2 {
			out = append(out, CellOrder[E]{Index: int(k.at), Entities: slices.Clone(k.held[:k.count])})
		}
	}
	slices.SortFunc(out, func(a, b CellOrder[E]) int { return a.Index - b.Index })
	return out
}

// Reorder puts each listed cell's occupants in order, class naming each one's
// half, refusing a list that names other occupants, breaks the shared-first
// partition or leaves a cell of two or more out.
func (g *Grid[E]) Reorder(cells []CellOrder[E], class func(E) Class) error {
	listed := make(map[int]struct{}, len(cells))
	for _, co := range cells {
		if _, twice := listed[co.Index]; twice || co.Index < 0 || co.Index >= len(g.cells) {
			return ErrOrderMismatch
		}
		listed[co.Index] = struct{}{}
		c := &g.cells[co.Index]
		if c.tag < holdsCrowd {
			return ErrOrderMismatch
		}
		k := &g.crowds[c.tag-holdsCrowd]
		held := k.held[:k.count]
		if len(co.Entities) != len(held) {
			return ErrOrderMismatch
		}
		for i, e := range co.Entities {
			if (i < int(k.shared)) != (class(e) == ClassShared) || !slices.Contains(held, e) || slices.Contains(co.Entities[:i], e) {
				return ErrOrderMismatch
			}
		}
		copy(held, co.Entities)
	}
	if len(g.crowds)-len(g.spare) != len(cells) {
		return ErrOrderMismatch
	}
	return nil
}
