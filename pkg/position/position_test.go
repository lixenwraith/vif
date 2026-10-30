package position

import (
	"math/rand/v2"
	"testing"

	"github.com/lixenwraith/vif/pkg/vmath"
)

type frame struct{}

func (frame) Aspect() vmath.Aspect { return vmath.RefAspect }

// TestAreaAnswersAsItsCells: an area query over the planes, whatever words and
// word boundaries it spans, is its cells' point queries joined, with walls of every
// mask placed, moved, remasked and removed, a rock laid and shifted, and the grid
// resized under them; so is a wall test's grid.
func TestAreaAnswersAsItsCells(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 11))
	walls := map[entity]Mask{}
	x := New[entity](frame{}, func(e entity) (Mask, bool) { m, ok := walls[e]; return m, ok })
	masks := []Mask{0, 1, 2, 1 | 8, 0xFF}
	w, h := 70, 9
	x.Resize(w, h)
	place := func(e entity, px, py int) {
		if x.Set(e, e.class(), px, py) {
			x.WallChangedAt(px, py)
		}
	}
	var held []struct {
		e    entity
		x, y int
	}
	var buf []bool
	for step := range 3000 {
		switch rng.IntN(12) {
		case 0, 1, 2, 3:
			e := mk(ClassShared, uint64(step+1))
			if rng.IntN(6) == 0 {
				e = mk(ClassPrivate, uint64(step+1)) // a private wall blocks nothing
			}
			walls[e] = masks[rng.IntN(len(masks))]
			px, py := rng.IntN(w), rng.IntN(h)
			place(e, px, py)
			held = append(held, struct {
				e    entity
				x, y int
			}{e, px, py})
		case 4, 5:
			if len(held) == 0 {
				break
			}
			i := rng.IntN(len(held))
			x.RemoveEntityAt(held[i].e, held[i].x, held[i].y)
			x.WallChangedAt(held[i].x, held[i].y)
			if rng.IntN(2) == 0 { // gone for good, or moved
				delete(walls, held[i].e)
				held[i] = held[len(held)-1]
				held = held[:len(held)-1]
			} else {
				held[i].x, held[i].y = rng.IntN(w), rng.IntN(h)
				place(held[i].e, held[i].x, held[i].y)
			}
		case 6:
			if len(held) > 0 {
				i := rng.IntN(len(held))
				walls[held[i].e] = masks[rng.IntN(len(masks))]
				x.WallChangedAt(held[i].x, held[i].y)
			}
		case 7:
			cols := make([]Column, rng.IntN(w+2))
			for i := range cols {
				cols[i] = Column{Ceil: rng.IntN(h+2) - 1, Floor: rng.IntN(h+2) - 1}
			}
			x.SetRock(masks[1+rng.IntN(len(masks)-1)], 0, cols)
		case 8:
			x.ShiftRock(Column{Ceil: rng.IntN(h), Floor: rng.IntN(h)})
		case 9:
			x.WallsChanged()
		case 10:
			if step%300 == 10 { // the planes follow a resize once the grid is filled again
				w, h = 40+rng.IntN(100), 3+rng.IntN(10)
				x.Resize(w, h)
				x.SetRock(0, 0, nil)
				held = held[:0]
				for e := range walls {
					px, py := rng.IntN(w), rng.IntN(h)
					if x.Set(e, e.class(), px, py) {
						held = append(held, struct {
							e    entity
							x, y int
						}{e, px, py})
					}
				}
			}
		}
		for range 4 {
			mask := masks[rng.IntN(len(masks))]
			px, py := rng.IntN(w+4)-2, rng.IntN(h+4)-2
			aw, ah := rng.IntN(70), rng.IntN(5)
			want := false
			for cy := py; cy < py+ah; cy++ {
				for cx := px; cx < px+aw; cx++ {
					want = want || x.Blocks(cx, cy, mask)
				}
			}
			if got := x.BlocksArea(px, py, aw, ah, mask); got != want {
				t.Fatalf("step %d mask %d area (%d,%d) %dx%d: %t, cells say %t", step, mask, px, py, aw, ah, got, want)
			}
		}
		if step%50 == 0 {
			mask := masks[rng.IntN(len(masks))]
			test := x.WallTest(mask, &buf)
			for py := range h {
				for px := range w {
					if got, want := test(px, py), x.Blocks(px, py, mask); got != want {
						t.Fatalf("step %d mask %d at (%d,%d): WallTest %t, Blocks %t", step, mask, px, py, got, want)
					}
				}
			}
		}
	}
}

// TestAPointAnswersAsTheCellsWalls: a point query is what the cell's shared
// occupants' masks say, a private occupant nothing, a wall of mask 0 a wall for
// any-wall queries alone.
func TestAPointAnswersAsTheCellsWalls(t *testing.T) {
	walls := map[entity]Mask{mk(ClassShared, 1): 2, mk(ClassShared, 2): 0, mk(ClassPrivate, 3): 0xFF}
	x := New[entity](frame{}, func(e entity) (Mask, bool) { m, ok := walls[e]; return m, ok })
	x.Resize(130, 4)
	for e := range walls {
		x.Set(e, e.class(), 100, 2)
		x.WallChangedAt(100, 2)
	}
	for _, tt := range []struct {
		mask Mask
		want bool
	}{{0, true}, {2, true}, {1, false}, {0xFF, true}, {4, false}} {
		if got := x.Blocks(100, 2, tt.mask); got != tt.want {
			t.Fatalf("mask %d: %t, want %t", tt.mask, got, tt.want)
		}
		if got := x.BlocksArea(99, 1, 3, 3, tt.mask); got != tt.want {
			t.Fatalf("mask %d around the cell: %t, want %t", tt.mask, got, tt.want)
		}
	}
	x.RemoveEntityAt(mk(ClassShared, 1), 100, 2)
	x.WallChangedAt(100, 2)
	if x.Blocks(100, 2, 2) || !x.Blocks(100, 2, 0) {
		t.Fatal("a wall gone still blocks, or the wall of mask 0 left with it")
	}
}
