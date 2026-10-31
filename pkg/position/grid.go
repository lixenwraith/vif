package position

import (
	"errors"
	"slices"
)

// CellCapacity keeps a Cell of eight-byte entities at exactly 256 bytes, four cache
// lines: 31 entities, two counts and six bytes of padding.
const CellCapacity = 31

// PrivateBudget caps the private half of a cell so a pile of one instance's own
// effects can never consume the slots a shared entity needs.
const PrivateBudget = 12

// Map bounds, a clamp rather than a rejection: a replicated map setup must reach
// the same bounds on every instance, and the product reaches make(). A grid is
// sized to its map at 256 bytes a cell, so the ceiling costs 32 MB.
const (
	// MaxWidth is the widest map, in cells.
	MaxWidth = 2000

	// MaxHeight is the tallest map, in cells.
	MaxHeight = 2000

	// MaxCells is the most cells a map or a volume may hold, whatever its shape.
	MaxCells = 500 * 250
)

// Cell is one grid cell: its occupants, the shared ones in Entities[:SharedCount]
// and the private ones in Entities[SharedCount:Count]. slot is the cell's place in
// the grid's occupied list while it holds anything.
type Cell[E comparable] struct {
	Count       uint8
	SharedCount uint8
	_           [2]byte
	slot        int32
	Entities    [CellCapacity]E
}

// view returns the sub-slice a scope selects; the partition makes every scope one
// contiguous range.
func (c *Cell[E]) view(scope Scope) []E {
	switch scope & ScopeAll {
	case ScopeShared:
		return c.Entities[:c.SharedCount]
	case ScopePrivate:
		return c.Entities[c.SharedCount:c.Count]
	case ScopeAll:
		return c.Entities[:c.Count]
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
// map a Cell: a volume of Depth layers, one on a flat map, each Width×Height.
type Grid[E comparable] struct {
	Cells  []Cell[E] // 1D array: index = (z*Height+y)*Width + x
	Width  int
	Height int
	Depth  int

	// occupied lists the cells holding an entity, in the order they filled, so a
	// copy, a clear and the gauges read what the map holds rather than the map: the
	// ceiling is 125,000 cells, 32 MB, which a scan touches whole.
	occupied []int32

	// present marks the cells holding an entity of each class, a bit a cell, the
	// layers' rows in turn, so a probe passes an empty cell on a word that stays in
	// cache rather than a cell of 256 bytes that does not, and a scan passes empty
	// cells a word at a time.
	present [2]Bits
}

// NewGrid makes a grid of width×height×depth cells, clamped to the ceiling.
func NewGrid[E comparable](width, height, depth int) *Grid[E] {
	width, height, depth = ClampVolume(width, height, depth)
	width, height = max(width, 0), max(height, 0)
	return &Grid[E]{
		Cells:   make([]Cell[E], width*height*depth),
		Width:   width,
		Height:  height,
		Depth:   depth,
		present: [2]Bits{NewBits(width, height*depth), NewBits(width, height*depth)},
	}
}

// Present is the cells holding an entity of class c, a bit a cell, the layers'
// rows in turn. Read-only.
func (g *Grid[E]) Present(c Class) Bits { return g.present[c] }

// InBounds reports whether the volume holds a cell at p.
func (g *Grid[E]) InBounds(p Point) bool {
	return p.X >= 0 && p.X < g.Width && p.Y >= 0 && p.Y < g.Height && p.Z >= 0 && p.Z < g.Depth
}

// row is the row of the presence bits and the planes that p's cell lies in
func (g *Grid[E]) row(p Point) int { return p.Z*g.Height + p.Y }

// CellAt returns p's cell, or nil outside the volume. Read-only.
func (g *Grid[E]) CellAt(p Point) *Cell[E] {
	if !g.InBounds(p) {
		return nil
	}
	return &g.Cells[g.row(p)*g.Width+p.X]
}

// anyIn reports whether a cell of [x0, x1) × [y0, y1), which the caller has
// bounded and found non-empty, holds anything scope selects, by the bits
func (g *Grid[E]) anyIn(x0, x1, y0, y1 int, scope Scope) bool {
	return scope&ScopeShared != 0 && g.present[ClassShared].Any(x0, x1, y0, y1) ||
		scope&ScopePrivate != 0 && g.present[ClassPrivate].Any(x0, x1, y0, y1)
}

// holds reports whether a cell scope selects holds anything, by the bits
func (g *Grid[E]) holds(x, y int, scope Scope) bool {
	return scope&ScopeShared != 0 && g.present[ClassShared].Bit(x, y) ||
		scope&ScopePrivate != 0 && g.present[ClassPrivate].Bit(x, y)
}

// Set inserts e into its class's half of p's cell. O(1); false when out of bounds,
// the cell is full, or the private budget is spent (soft clip).
func (g *Grid[E]) Set(e E, class Class, p Point) bool {
	if !g.InBounds(p) {
		return false
	}

	x, y := p.X, g.row(p)
	idx := int32(y*g.Width + x)
	cell := &g.Cells[idx]
	if cell.Count >= CellCapacity {
		return false
	}

	// A private insert appends inside its own budget and never touches the shared run
	if class == ClassPrivate {
		if cell.Count-cell.SharedCount >= PrivateBudget {
			return false
		}
		g.fill(idx, cell)
		if cell.Count == cell.SharedCount {
			g.present[ClassPrivate].Set(x, y)
		}
		cell.Entities[cell.Count] = e
		cell.Count++
		return true
	}

	// A shared insert splits the run: the first private entity moves to the tail
	g.fill(idx, cell)
	if cell.SharedCount == 0 {
		g.present[ClassShared].Set(x, y)
	}
	cell.Entities[cell.Count] = cell.Entities[cell.SharedCount]
	cell.Entities[cell.SharedCount] = e
	cell.SharedCount++
	cell.Count++
	return true
}

// fill notes a cell about to take its first entity.
func (g *Grid[E]) fill(idx int32, cell *Cell[E]) {
	if cell.Count == 0 {
		cell.slot = int32(len(g.occupied))
		g.occupied = append(g.occupied, idx)
	}
}

// vacate takes a cell that emptied off the occupied list, the last one into its place.
func (g *Grid[E]) vacate(cell *Cell[E]) {
	last := g.occupied[len(g.occupied)-1]
	g.occupied[cell.slot] = last
	g.Cells[last].slot = cell.slot
	g.occupied = g.occupied[:len(g.occupied)-1]
}

// Load fills cell idx from a copy: its counts and its occupants in the copy's order.
func (g *Grid[E]) Load(idx int32, count, shared uint8, occupants []E) {
	cell := &g.Cells[idx]
	g.fill(idx, cell)
	cell.Count, cell.SharedCount = count, shared
	copy(cell.Entities[:count], occupants)
	x, y := int(idx)%g.Width, int(idx)/g.Width
	g.present[ClassShared].Put(x, y, shared > 0)
	g.present[ClassPrivate].Put(x, y, count > shared)
}

// Occupied is every cell holding an entity, in the order they filled. Read-only.
func (g *Grid[E]) Occupied() []int32 { return g.occupied }

// RemoveEntityAt deletes e from p's cell. O(k), k the cell's occupants:
// swap-removes within e's half, refilling the shared tail from the private run.
func (g *Grid[E]) RemoveEntityAt(e E, p Point) {
	if !g.InBounds(p) {
		return
	}

	x, y := p.X, g.row(p)
	cell := &g.Cells[y*g.Width+x]
	for i := uint8(0); i < cell.Count; i++ {
		if cell.Entities[i] != e {
			continue
		}

		if i < cell.SharedCount {
			// Close the shared run, then pull the last private entity into the vacated shared tail
			lastShared := cell.SharedCount - 1
			cell.Entities[i] = cell.Entities[lastShared]
			cell.Count--
			cell.SharedCount--
			if lastShared != cell.Count {
				cell.Entities[lastShared] = cell.Entities[cell.Count]
			}
			if cell.SharedCount == 0 {
				g.present[ClassShared].Clear(x, y)
			}
		} else {
			cell.Count--
			if i != cell.Count {
				cell.Entities[i] = cell.Entities[cell.Count]
			}
			if cell.Count == cell.SharedCount {
				g.present[ClassPrivate].Clear(x, y)
			}
		}

		var zero E
		cell.Entities[cell.Count] = zero
		if cell.Count == 0 {
			g.vacate(cell)
		}
		return
	}
}

// EntitiesAt returns a view of the entities scope selects at (x, y) of the first
// layer, nil if empty or out of bounds: the cell's own storage, to copy or to read
// before it changes.
func (g *Grid[E]) EntitiesAt(x, y int, scope Scope) []E {
	if x < 0 || x >= g.Width || y < 0 || y >= g.Height || !g.holds(x, y, scope) {
		return nil
	}
	return g.Cells[y*g.Width+x].view(scope)
}

// EntitiesAtPoint is EntitiesAt for a cell of any layer.
func (g *Grid[E]) EntitiesAtPoint(p Point, scope Scope) []E {
	if !g.InBounds(p) || !g.holds(p.X, g.row(p), scope) {
		return nil
	}
	return g.Cells[g.row(p)*g.Width+p.X].view(scope)
}

// Occupants is EntitiesAt without the presence test, for a caller that knows the
// cell holds something, as one asking about an entity's own cell does.
func (g *Grid[E]) Occupants(x, y int, scope Scope) []E {
	if x < 0 || x >= g.Width || y < 0 || y >= g.Height {
		return nil
	}
	return g.Cells[y*g.Width+x].view(scope)
}

// HasAnyEntityAt reports whether scope selects at least one entity at (x, y) of
// the first layer. O(1)
func (g *Grid[E]) HasAnyEntityAt(x, y int, scope Scope) bool {
	return x >= 0 && x < g.Width && y >= 0 && y < g.Height && g.holds(x, y, scope)
}

// HasAnyEntityAtPoint is HasAnyEntityAt for a cell of any layer.
func (g *Grid[E]) HasAnyEntityAtPoint(p Point, scope Scope) bool {
	return g.InBounds(p) && g.holds(p.X, g.row(p), scope)
}

// Holds reports whether the grid holds e in its class's half of p's cell, which
// tells an idempotent write from a soft-clipped entity that needs another insertion.
func (g *Grid[E]) Holds(e E, class Class, p Point) bool {
	for _, occupant := range g.EntitiesAtPoint(p, 1<<class) {
		if occupant == e {
			return true
		}
	}
	return false
}

// Clear removes every entity from every cell.
func (g *Grid[E]) Clear() {
	for _, i := range g.occupied {
		g.Cells[i].Count = 0
		g.Cells[i].SharedCount = 0
	}
	g.occupied = g.occupied[:0]
	clear(g.present[ClassShared].Words)
	clear(g.present[ClassPrivate].Words)
}

// Resize sets the grid's dimensions, a volume of newDepth layers, emptying it.
// Backing storage is grow-only: shrinking keeps capacity, so a map that oscillates
// never reallocates; growth allocates once and holds. It clamps rather than
// trusting its caller: every path into it carries dimensions from a replicated
// payload or a terminal report, and an unbounded product here is a panic in the
// allocator rather than a rejected map.
func (g *Grid[E]) Resize(newWidth, newHeight, newDepth int) {
	newWidth, newHeight, newDepth = ClampVolume(newWidth, newHeight, newDepth)
	// Floored before the dimensions are stored, not just before they are used: a
	// grid recording bounds that disagree with the cells it holds answers Set and
	// every enumeration from two different maps.
	newWidth, newHeight = max(newWidth, 0), max(newHeight, 0)
	g.occupied = g.occupied[:0]
	need := newWidth * newHeight * newDepth
	if need <= cap(g.Cells) {
		g.Cells = g.Cells[:need]
		// Every cell, as a re-grown one may hold stale counts from a larger layout
		for i := range g.Cells {
			g.Cells[i].Count = 0
			g.Cells[i].SharedCount = 0
		}
	} else {
		g.Cells = make([]Cell[E], need)
	}
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

// ComputeStats reads the occupied cells.
func (g *Grid[E]) ComputeStats() Stats {
	var stats Stats
	for _, i := range g.occupied {
		count := int(g.Cells[i].Count)
		stats.CellsOccupied++
		stats.EntitiesTotal += count
		stats.EntitiesShared += int(g.Cells[i].SharedCount)
		if count > stats.MaxOccupancy {
			stats.MaxOccupancy = count
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

// Order reads the occupants of every cell holding two or more.
func (g *Grid[E]) Order() []CellOrder[E] {
	var out []CellOrder[E]
	for i := range g.Cells {
		if c := &g.Cells[i]; c.Count >= 2 {
			out = append(out, CellOrder[E]{Index: i, Entities: slices.Clone(c.Entities[:c.Count])})
		}
	}
	return out
}

// Reorder puts each listed cell's occupants in order, class naming each one's
// half, refusing a list that names other occupants, breaks the shared-first
// partition or leaves a cell of two or more out.
func (g *Grid[E]) Reorder(cells []CellOrder[E], class func(E) Class) error {
	listed := make(map[int]struct{}, len(cells))
	for _, co := range cells {
		if _, twice := listed[co.Index]; twice || co.Index < 0 || co.Index >= len(g.Cells) {
			return ErrOrderMismatch
		}
		listed[co.Index] = struct{}{}
		c := &g.Cells[co.Index]
		held := c.Entities[:c.Count]
		if c.Count < 2 || len(co.Entities) != len(held) {
			return ErrOrderMismatch
		}
		for k, e := range co.Entities {
			shared := k < int(c.SharedCount)
			if shared != (class(e) == ClassShared) || !slices.Contains(held, e) || slices.Contains(co.Entities[:k], e) {
				return ErrOrderMismatch
			}
		}
		copy(held, co.Entities)
	}
	crowded := 0
	for i := range g.Cells {
		if g.Cells[i].Count >= 2 {
			crowded++
		}
	}
	if crowded != len(cells) {
		return ErrOrderMismatch
	}
	return nil
}
