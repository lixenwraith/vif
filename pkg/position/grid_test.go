package position

import (
	"math"
	"math/rand/v2"
	"testing"
	"unsafe"
)

// entity is a test id: class in the top byte, as the game's entities are laid out
type entity uint64

func mk(c Class, id uint64) entity { return entity(uint64(c)<<56 | id) }

func (e entity) class() Class { return Class(e >> 56) }

// TestCellFitsFourCacheLines pins the layout the partition byte was taken from.
func TestCellFitsFourCacheLines(t *testing.T) {
	if got := unsafe.Sizeof(Cell[entity]{}); got != 256 {
		t.Fatalf("unsafe.Sizeof(Cell{}) = %d, want 256", got)
	}
}

func TestGridPartitionSurvivesRemoval(t *testing.T) {
	g := NewGrid[entity](4, 4, 1)
	shared := []entity{mk(ClassShared, 1), mk(ClassShared, 2)}
	private := []entity{mk(ClassPrivate, 1), mk(ClassPrivate, 2), mk(ClassPrivate, 3)}

	// Interleaved insertion must still yield a partitioned cell
	g.Set(private[0], ClassPrivate, Point{X: 1, Y: 1})
	g.Set(shared[0], ClassShared, Point{X: 1, Y: 1})
	g.Set(private[1], ClassPrivate, Point{X: 1, Y: 1})
	g.Set(shared[1], ClassShared, Point{X: 1, Y: 1})
	g.Set(private[2], ClassPrivate, Point{X: 1, Y: 1})
	assertPartition(t, g, 1, 1, 2, 3)

	g.RemoveEntityAt(shared[0], Point{X: 1, Y: 1})
	assertPartition(t, g, 1, 1, 1, 3)

	g.RemoveEntityAt(private[1], Point{X: 1, Y: 1})
	assertPartition(t, g, 1, 1, 1, 2)

	if got := g.EntitiesAt(1, 1, ScopeShared)[0]; got != shared[1] {
		t.Fatalf("surviving shared entity = %d, want %d", got, shared[1])
	}
}

func TestPrivateBudgetPreservesSharedCapacity(t *testing.T) {
	g := NewGrid[entity](1, 1, 1)
	for i := range PrivateBudget {
		if !g.Set(mk(ClassPrivate, uint64(i+1)), ClassPrivate, Point{X: 0, Y: 0}) {
			t.Fatalf("private %d rejected inside the budget", i)
		}
	}
	if g.Set(mk(ClassPrivate, 9999), ClassPrivate, Point{X: 0, Y: 0}) {
		t.Fatal("private insert exceeded the budget")
	}
	for i := range CellCapacity - PrivateBudget {
		if !g.Set(mk(ClassShared, uint64(i+1)), ClassShared, Point{X: 0, Y: 0}) {
			t.Fatalf("shared %d displaced by a saturated private run", i)
		}
	}
}

// assertPartition checks the class invariant and the expected run lengths.
func assertPartition(t *testing.T, g *Grid[entity], x, y, wantShared, wantPrivate int) {
	t.Helper()
	sharedView := g.EntitiesAt(x, y, ScopeShared)
	privateView := g.EntitiesAt(x, y, ScopePrivate)
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

// TestGridOccupiedListIsTheCellsHoldingEntities holds the index a copy and the gauges
// read in place of the map: every listed cell holds something, every cell holding
// something is listed once at its slot, and the stats read off the list are the
// full scan's.
func TestGridOccupiedListIsTheCellsHoldingEntities(t *testing.T) {
	g := NewGrid[entity](7, 5, 1)
	rng := rand.New(rand.NewPCG(3, 9))
	type placed struct {
		e    entity
		x, y int
	}
	var held []placed
	check := func() {
		t.Helper()
		seen := map[int32]bool{}
		for slot, idx := range g.Occupied() {
			c := &g.Cells[idx]
			if c.Count == 0 || int(c.slot) != slot || seen[idx] {
				t.Fatalf("occupied[%d] = cell %d: count %d, slot %d, seen %v", slot, idx, c.Count, c.slot, seen[idx])
			}
			seen[idx] = true
		}
		var want Stats
		for i := range g.Cells {
			c := &g.Cells[i]
			if x, y := i%7, i/7; g.Present(ClassShared).Bit(x, y) != (c.SharedCount > 0) || g.Present(ClassPrivate).Bit(x, y) != (c.Count > c.SharedCount) {
				t.Fatalf("cell %d holds %d shared of %d, the presence bits say otherwise", i, c.SharedCount, c.Count)
			}
			if n := int(c.Count); n > 0 {
				if !seen[int32(i)] {
					t.Fatalf("cell %d holds %d and is not listed", i, n)
				}
				want.CellsOccupied++
				want.EntitiesTotal += n
				want.EntitiesShared += int(g.Cells[i].SharedCount)
				want.MaxOccupancy = max(want.MaxOccupancy, n)
			}
		}
		if got := g.ComputeStats(); got != want {
			t.Fatalf("stats off the list %+v, off the cells %+v", got, want)
		}
	}
	for step := range 2000 {
		switch {
		case len(held) > 0 && rng.IntN(3) == 0:
			i := rng.IntN(len(held))
			g.RemoveEntityAt(held[i].e, Point{X: held[i].x, Y: held[i].y})
			held[i] = held[len(held)-1]
			held = held[:len(held)-1]
		case step%97 == 96:
			g.Clear()
			held = nil
		default:
			c := ClassShared
			if rng.IntN(4) == 0 {
				c = ClassPrivate
			}
			e := mk(c, uint64(step+1))
			x, y := rng.IntN(7), rng.IntN(5)
			if g.Set(e, c, Point{X: x, Y: y}) {
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
		if len(g.Cells) > MaxCells {
			t.Fatalf("resize to %dx%d allocated %d cells", tt[0], tt[1], len(g.Cells))
		}
		if g.Width*g.Height != len(g.Cells) {
			t.Fatalf("resize to %dx%d left %dx%d describing %d cells",
				tt[0], tt[1], g.Width, g.Height, len(g.Cells))
		}
	}
}
