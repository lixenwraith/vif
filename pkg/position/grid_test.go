package position

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"
	"unsafe"
)

// entity is a test id: class in the top byte, as the game's entities are laid out
type entity uint64

func mk(c Class, id uint64) entity { return entity(uint64(c)<<56 | id) }

func (e entity) class() Class { return Class(e >> 56) }

// TestACellOfEightByteEntitiesIsSixteenBytes pins the layout the grid's cost is
// read from: four cells a cache line, and a crowd four lines.
func TestACellOfEightByteEntitiesIsSixteenBytes(t *testing.T) {
	if cell, crowd := unsafe.Sizeof(cell[entity]{}), unsafe.Sizeof(crowd[entity]{}); cell != 16 || crowd != 256 {
		t.Fatalf("a cell is %d bytes and a crowd %d, want 16 and 256", cell, crowd)
	}
}

func TestGridPartitionSurvivesRemoval(t *testing.T) {
	g := NewGrid[entity](4, 4, 1)
	shared := []entity{mk(ClassShared, 1), mk(ClassShared, 2)}
	private := []entity{mk(ClassPrivate, 1), mk(ClassPrivate, 2), mk(ClassPrivate, 3)}

	// Interleaved insertion must still yield a partitioned cell
	g.Set(private[0], ClassPrivate, Point3{X: 1, Y: 1})
	g.Set(shared[0], ClassShared, Point3{X: 1, Y: 1})
	g.Set(private[1], ClassPrivate, Point3{X: 1, Y: 1})
	g.Set(shared[1], ClassShared, Point3{X: 1, Y: 1})
	g.Set(private[2], ClassPrivate, Point3{X: 1, Y: 1})
	assertPartition(t, g, 1, 1, 2, 3)

	g.RemoveEntityAt(shared[0], Point3{X: 1, Y: 1})
	assertPartition(t, g, 1, 1, 1, 3)

	g.RemoveEntityAt(private[1], Point3{X: 1, Y: 1})
	assertPartition(t, g, 1, 1, 1, 2)

	if got := g.EntitiesAt(Point{X: 1, Y: 1}, ScopeShared)[0]; got != shared[1] {
		t.Fatalf("surviving shared entity = %d, want %d", got, shared[1])
	}
}

func TestPrivateBudgetPreservesSharedCapacity(t *testing.T) {
	g := NewGrid[entity](1, 1, 1)
	for i := range PrivateBudget {
		if g.Set(mk(ClassPrivate, uint64(i+1)), ClassPrivate, Point3{X: 0, Y: 0}) == 0 {
			t.Fatalf("private %d rejected inside the budget", i)
		}
	}
	if g.Set(mk(ClassPrivate, 9999), ClassPrivate, Point3{X: 0, Y: 0}) != 0 {
		t.Fatal("private insert exceeded the budget")
	}
	for i := range CellCapacity - PrivateBudget {
		if g.Set(mk(ClassShared, uint64(i+1)), ClassShared, Point3{X: 0, Y: 0}) == 0 {
			t.Fatalf("shared %d displaced by a saturated private run", i)
		}
	}
}

// assertPartition checks the class invariant and the expected run lengths.
func assertPartition(t *testing.T, g *Grid[entity], x, y, wantShared, wantPrivate int) {
	t.Helper()
	sharedView := g.EntitiesAt(Point{X: x, Y: y}, ScopeShared)
	privateView := g.EntitiesAt(Point{X: x, Y: y}, ScopePrivate)
	if len(sharedView) != wantShared || len(privateView) != wantPrivate {
		t.Fatalf("cell runs = (%d, %d), want (%d, %d)", len(sharedView), len(privateView), wantShared, wantPrivate)
	}
	for _, e := range sharedView {
		if e.class() != ClassShared {
			t.Fatalf("private entity %d inside the shared run", e)
		}
	}
	for _, e := range privateView {
		if e.class() != ClassPrivate {
			t.Fatalf("shared entity %d inside the private run", e)
		}
	}
}

// model is a cell as the grid's rules make it, in one slice: the shared occupants
// first, a shared insert moving the first private one to the tail, a removal
// closing its half from that half's tail.
type model struct {
	held   []entity
	shared int
}

func (m *model) set(e entity) bool {
	if len(m.held) >= CellCapacity || e.class() == ClassPrivate && len(m.held)-m.shared >= PrivateBudget {
		return false
	}
	m.held = append(m.held, e)
	if e.class() == ClassShared {
		last := len(m.held) - 1
		m.held[last], m.held[m.shared] = m.held[m.shared], e
		m.shared++
	}
	return true
}

func (m *model) remove(e entity) {
	i, last := slices.Index(m.held, e), len(m.held)-1
	if i < m.shared {
		m.shared--
		m.held[i] = m.held[m.shared]
		m.held[m.shared] = m.held[last]
	} else {
		m.held[i] = m.held[last]
	}
	m.held = m.held[:last]
}

// TestGridHoldsEachCellAsItsRulesMakeIt: whether a cell holds one occupant in
// itself or more in a crowd, they and their order are the model's, a cell that
// emptied and one that never filled read alike, and the presence bits, the
// gauges, the listing, the order and a copy loaded elsewhere are the cells'.
func TestGridHoldsEachCellAsItsRulesMakeIt(t *testing.T) {
	const w, h = 7, 5
	g := NewGrid[entity](w, h, 1)
	rng := rand.New(rand.NewPCG(3, 9))
	cells := make([]model, w*h)
	type placed struct {
		e    entity
		x, y int
	}
	var held []placed
	check := func() {
		t.Helper()
		var want Stats
		crowded := 0
		for i := range cells {
			m, x, y := &cells[i], i%w, i/w
			if got := g.Occupants(Point{X: x, Y: y}, ScopeAll); !slices.Equal(got, m.held) {
				t.Fatalf("cell %d holds %v, its rules make it %v", i, got, m.held)
			}
			if got := g.EntitiesAt(Point{X: x, Y: y}, ScopeShared); !slices.Equal(got, m.held[:m.shared]) {
				t.Fatalf("cell %d's shared half is %v of %v", i, got, m.held)
			}
			if g.Present(ClassShared).Bit(x, y) != (m.shared > 0) || g.Present(ClassPrivate).Bit(x, y) != (len(m.held) > m.shared) {
				t.Fatalf("cell %d holds %d shared of %d, the presence bits say otherwise", i, m.shared, len(m.held))
			}
			if n := len(m.held); n > 0 {
				want.CellsOccupied++
				want.EntitiesTotal += n
				want.EntitiesShared += m.shared
				want.MaxOccupancy = max(want.MaxOccupancy, n)
			}
			if len(m.held) >= 2 {
				crowded++
			}
		}
		if got := g.ComputeStats(); got != want {
			t.Fatalf("the gauges read %+v, the cells %+v", got, want)
		}
		listed, last := 0, int32(-1)
		g.EachOccupied(func(index int32, occupants []entity, shared int) {
			if m := &cells[index]; !slices.Equal(occupants, m.held) || shared != m.shared || len(occupants) == 0 || index <= last {
				t.Fatalf("cell %d is listed after %d as %v, %d shared; it holds %v, %d shared", index, last, occupants, shared, m.held, m.shared)
			}
			listed, last = listed+1, index
		})
		order := g.Order()
		if listed != want.CellsOccupied || len(order) != crowded || !slices.IsSortedFunc(order, func(a, b CellOrder[entity]) int { return a.Index - b.Index }) {
			t.Fatalf("%d cells listed and %d in order, of %d holding something and %d crowded", listed, len(order), want.CellsOccupied, crowded)
		}
	}
	for step := range 6000 {
		switch {
		case len(held) > 0 && rng.IntN(5) < 2:
			i := rng.IntN(len(held))
			g.RemoveEntityAt(held[i].e, Point3{X: held[i].x, Y: held[i].y})
			cells[held[i].y*w+held[i].x].remove(held[i].e)
			held[i] = held[len(held)-1]
			held = held[:len(held)-1]
		case step%997 == 996:
			g.Clear()
			held, cells = nil, make([]model, w*h)
		case step%499 == 498: // a copy loaded into another grid is this one
			o := NewGrid[entity](w, h, 1)
			g.EachOccupied(func(index int32, occupants []entity, shared int) { o.Load(index, occupants, shared) })
			g = o
		default:
			c := ClassShared
			if rng.IntN(3) == 0 {
				c = ClassPrivate
			}
			e := mk(c, uint64(step+1))
			x, y := rng.IntN(w), rng.IntN(h)
			if rng.IntN(2) == 0 { // a few cells take half the entities, to the capacity
				x, y = rng.IntN(2), 0
			}
			took := g.Set(e, c, Point3{X: x, Y: y}) > 0
			if took != cells[y*w+x].set(e) {
				t.Fatalf("step %d: the grid took %v at (%d, %d): %t, its rules say otherwise", step, e, x, y, took)
			}
			if took {
				held = append(held, placed{e, x, y})
			}
		}
		check()
	}
}

// TestClampBoundsEveryShape covers the three ways a requested map can be illegal:
// too wide, too tall, and legal on both axes but too large as a product.
func TestClampBoundsEveryShape(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		w, h int
	}{
		{"ordinary", 160, 50},
		{"at the cell ceiling", 500, MaxCells / 500},
		{"too wide", MaxWidth * 4, 10},
		{"too tall", 10, MaxHeight * 4},
		{"legal axes, illegal product", MaxWidth, MaxHeight},
		{"overflows int on multiply", math.MaxInt32, math.MaxInt32},
		{"maximum int", math.MaxInt, math.MaxInt},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w, h := Clamp(tt.w, tt.h)
			if w < 1 || h < 1 {
				t.Fatalf("clamped %dx%d to %dx%d, which is not a map", tt.w, tt.h, w, h)
			}
			if w > MaxWidth || h > MaxHeight {
				t.Fatalf("clamped %dx%d to %dx%d, outside %dx%d", tt.w, tt.h, w, h, MaxWidth, MaxHeight)
			}
			if w*h > MaxCells {
				t.Fatalf("clamped %dx%d to %d cells, ceiling is %d", tt.w, tt.h, w*h, MaxCells)
			}
		})
	}
}

// TestClampLeavesALegalMapAlone is the non-vacuous half: a clamp that reshaped an
// ordinary map would be a bug rather than a defence.
func TestClampLeavesALegalMapAlone(t *testing.T) {
	t.Parallel()
	for _, tt := range [][2]int{{80, 24}, {160, 50}, {200, 60}, {1000, 100}} {
		if w, h := Clamp(tt[0], tt[1]); w != tt[0] || h != tt[1] {
			t.Fatalf("clamped a legal %dx%d to %dx%d", tt[0], tt[1], w, h)
		}
	}
}

// TestTheGridSurvivesAHostileResize: the dimensions reach make() from a replicated
// map setup, so before the clamp a product overflowing int panicked the allocator,
// and under a crash handler that is the process.
func TestTheGridSurvivesAHostileResize(t *testing.T) {
	t.Parallel()
	g := NewGrid[entity](80, 24, 1)
	for _, tt := range [][2]int{
		{math.MaxInt32, math.MaxInt32},
		{math.MaxInt, math.MaxInt},
		{1 << 31, 1 << 31},
		{-1, -1},
		{0, 0},
		{MaxWidth * 100, MaxHeight * 100},
	} {
		g.Resize(tt[0], tt[1], 1) // must not panic
		if len(g.cells) > MaxCells {
			t.Fatalf("resize to %dx%d allocated %d cells", tt[0], tt[1], len(g.cells))
		}
		if g.Width*g.Height != len(g.cells) {
			t.Fatalf("resize to %dx%d left %dx%d describing %d cells",
				tt[0], tt[1], g.Width, g.Height, len(g.cells))
		}
	}
}
