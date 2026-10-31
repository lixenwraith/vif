package position

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/lixenwraith/vif/pkg/vmath"
)

type frame struct{}

func (frame) Aspect() vmath.Aspect { return vmath.RefAspect }

// TestAreaAnswersAsItsCells: an area query over the planes, whatever words and
// word boundaries it spans, is its cells' point queries joined, with blocks of
// every mask placed, moved, remasked and removed, surfaces laid and shifted, and
// the grid resized under them; so is the grid of block bits.
func TestAreaAnswersAsItsCells(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 11))
	walls := map[entity]Mask{}
	x := New[entity](frame{}, func(e entity) (Mask, bool) { m, ok := walls[e]; return m, ok })
	var w, h int
	var ceiling, floor *Surface
	lay := func(mask Mask, w int) {
		top, bottom := make([]int, w), make([]int, w)
		for i := range w {
			top[i], bottom[i] = rng.IntN(h+2)-1, rng.IntN(h+2)-1
		}
		if ceiling == nil {
			ceiling, floor = x.AddSurface(mask, Above, 0, top), x.AddSurface(mask, Below, 0, bottom)
			return
		}
		ceiling.Mask, floor.Mask = mask, mask
		x.LaySurface(ceiling, 0, top)
		x.LaySurface(floor, 0, bottom)
	}
	lift := func() {
		if ceiling != nil {
			x.RemoveSurface(ceiling)
			x.RemoveSurface(floor)
			ceiling, floor = nil, nil
		}
	}
	masks := []Mask{0, 1, 2, 1 | 8, 0xFF}
	w, h = 70, 9
	x.Resize(w, h, 1)
	place := func(e entity, px, py int) {
		if x.Set(e, e.class(), Point3{X: px, Y: py}) {
			x.BlockChangedAt(Point3{X: px, Y: py})
		}
	}
	var held []struct {
		e    entity
		x, y int
	}
	var buf []uint64
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
			x.RemoveEntityAt(held[i].e, Point3{X: held[i].x, Y: held[i].y})
			x.BlockChangedAt(Point3{X: held[i].x, Y: held[i].y})
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
				x.BlockChangedAt(Point3{X: held[i].x, Y: held[i].y})
			}
		case 7: // surfaces narrower or wider than the map, or none
			if rng.IntN(5) == 0 {
				lift()
			} else {
				lay(masks[1+rng.IntN(len(masks)-1)], rng.IntN(w+2))
			}
		case 8:
			if ceiling != nil {
				x.ShiftSurface(ceiling, rng.IntN(h))
				x.ShiftSurface(floor, rng.IntN(h))
			}
		case 9:
			x.BlocksChanged()
		case 10:
			if step%300 == 10 { // the planes follow a resize once the grid is filled again
				w, h = 40+rng.IntN(100), 3+rng.IntN(10)
				x.Resize(w, h, 1)
				lift()
				held = held[:0]
				for e := range walls {
					px, py := rng.IntN(w), rng.IntN(h)
					if x.Set(e, e.class(), Point3{X: px, Y: py}) {
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
			bits := x.BlockBits(mask, buf)
			buf = bits.Words
			for py := range h {
				for px := range w {
					if got, want := bits.Bit(px, py), x.Blocks(px, py, mask); got != want {
						t.Fatalf("step %d mask %d at (%d,%d): BlockBits %t, Blocks %t", step, mask, px, py, got, want)
					}
				}
			}
		}
	}
}

// TestAPointAnswersAsTheCellsBlocks: a point query is what the cell's shared
// occupants' masks say, a private occupant nothing, a block of mask 0 a block for
// any-block queries alone.
func TestAPointAnswersAsTheCellsBlocks(t *testing.T) {
	walls := map[entity]Mask{mk(ClassShared, 1): 2, mk(ClassShared, 2): 0, mk(ClassPrivate, 3): 0xFF}
	x := New[entity](frame{}, func(e entity) (Mask, bool) { m, ok := walls[e]; return m, ok })
	x.Resize(130, 4, 1)
	for e := range walls {
		x.Set(e, e.class(), Point3{X: 100, Y: 2})
		x.BlockChangedAt(Point3{X: 100, Y: 2})
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
	x.RemoveEntityAt(mk(ClassShared, 1), Point3{X: 100, Y: 2})
	x.BlockChangedAt(Point3{X: 100, Y: 2})
	if x.Blocks(100, 2, 2) || !x.Blocks(100, 2, 0) {
		t.Fatal("a block gone still blocks, or the block of mask 0 left with it")
	}
}

// TestAVolumeAnswersLayerByLayer: a point, a box and a line of sight through a
// volume answer from the layer each cell lies in, the flat queries answering for
// the first layer as the point forms do there and the surfaces lying on it alone,
// and a depth the ceiling cannot hold is clamped.
func TestAVolumeAnswersLayerByLayer(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 9))
	walls := map[entity]Mask{}
	x := New[entity](frame{}, func(e entity) (Mask, bool) { m, ok := walls[e]; return m, ok })
	const w, h, d = 9, 6, 4
	x.Resize(w, h, d)
	if x.Depth != d || len(x.Cells) != w*h*d {
		t.Fatalf("a %d×%d×%d volume has depth %d and %d cells", w, h, d, x.Depth, len(x.Cells))
	}
	masks := []Mask{0, 1, 2, 4, 3}
	cells := map[Point3][]entity{}
	for i := range 80 {
		if i == 40 { // the planes stand from here, so the rest reach them cell by cell
			for _, m := range masks {
				x.BlocksAt(Point3{Z: 1}, m)
			}
		}
		e := mk(ClassShared, uint64(i+1))
		if rng.IntN(4) == 0 {
			e = mk(ClassPrivate, uint64(i+1))
		}
		if rng.IntN(2) == 0 {
			walls[e] = Mask(1 << rng.IntN(3))
		}
		p := Point3{X: rng.IntN(w), Y: rng.IntN(h), Z: rng.IntN(d)}
		if x.Set(e, e.class(), p) {
			x.BlockChangedAt(p)
			cells[p] = append(cells[p], e)
		}
	}
	heights := make([]int, w)
	for i := range heights {
		heights[i] = rng.IntN(h + 1)
	}
	x.AddSurface(1, Above, 0, heights)
	blocked := func(p Point3, mask Mask) bool {
		if p.Z == 0 && p.Y < heights[p.X] && (mask == 0 || mask&1 != 0) {
			return true
		}
		for _, e := range cells[p] {
			if m, ok := walls[e]; ok && e.class() == ClassShared && (mask == 0 || m&mask != 0) {
				return true
			}
		}
		return false
	}
	for z := range d {
		for y := range h {
			for px := range w {
				p := Point3{X: px, Y: y, Z: z}
				for _, mask := range masks {
					if got, want := x.BlocksAt(p, mask), blocked(p, mask); got != want {
						t.Fatalf("mask %d at %v: BlocksAt %t, the cell says %t", mask, p, got, want)
					}
					if z == 0 && x.Blocks(px, y, mask) != x.BlocksAt(p, mask) {
						t.Fatalf("mask %d at %v: the flat query and the point form differ", mask, p)
					}
				}
				got, want := slices.Clone(x.EntitiesIn(p, ScopeAll)), slices.Clone(cells[p])
				slices.Sort(got)
				slices.Sort(want)
				if !slices.Equal(got, want) || x.HasAnyEntityIn(p, ScopeAll) != (len(want) > 0) {
					t.Fatalf("at %v: holds %v, placed %v", p, got, want)
				}
				if z == 0 && len(x.EntitiesAt(px, y, ScopeAll)) != len(got) {
					t.Fatalf("at %v: the flat query and the point form differ", p)
				}
			}
		}
	}
	inside := func() Point3 { return Point3{X: rng.IntN(w), Y: rng.IntN(h), Z: rng.IntN(d)} }
	for range 500 {
		mask := masks[rng.IntN(len(masks))]
		p := Point3{X: rng.IntN(w+4) - 2, Y: rng.IntN(h+4) - 2, Z: rng.IntN(d+4) - 2}
		bw, bh, bd := 1+rng.IntN(4), 1+rng.IntN(4), 1+rng.IntN(3)
		want := false
		for z := p.Z; z < p.Z+bd; z++ {
			for y := p.Y; y < p.Y+bh; y++ {
				for px := p.X; px < p.X+bw; px++ {
					want = want || x.BlocksAt(Point3{X: px, Y: y, Z: z}, mask)
				}
			}
		}
		if got := x.BlocksBox(p, bw, bh, bd, mask); got != want {
			t.Fatalf("mask %d box %v %d×%d×%d: BlocksBox %t, its cells say %t", mask, p, bw, bh, bd, got, want)
		}
		a, b := inside(), inside()
		want = true
		for l := vmath.NewLine3(a, b).Next(); !l.AtEnd(); l = l.Next() {
			want = want && !x.BlocksAt(l.Point(), mask)
		}
		if got := x.HasLineOfSightBetween(a, b, mask); got != want {
			t.Fatalf("mask %d %v→%v: HasLineOfSightBetween %t, the line's cells say %t", mask, a, b, got, want)
		}
	}
	if x.Resize(500, 250, 3); x.Depth != 1 {
		t.Fatalf("a map at the ceiling took depth %d, want 1", x.Depth)
	}
	if x.Resize(100, 50, 30); x.Depth != 25 {
		t.Fatalf("100×50×30 took depth %d, want the 25 layers the ceiling allows", x.Depth)
	}
}

// TestAnAreaLineOfSightFliesTheBodyFromItsAnchor: a body clears the line between
// two anchor cells when, with its anchor on each cell strictly between them, every
// cell of it lies on the map and none is blocked; a body of one cell is a point's
// line of sight.
func TestAnAreaLineOfSightFliesTheBodyFromItsAnchor(t *testing.T) {
	rng := rand.New(rand.NewPCG(8, 2))
	walls := map[entity]Mask{}
	x := New[entity](frame{}, func(e entity) (Mask, bool) { m, ok := walls[e]; return m, ok })
	const w, h = 24, 12
	x.Resize(w, h, 1)
	for i := range 40 {
		e := mk(ClassShared, uint64(i+1))
		walls[e] = Mask(1 << rng.IntN(2))
		p := Point3{X: rng.IntN(w), Y: rng.IntN(h)}
		if x.Set(e, e.class(), p) {
			x.BlockChangedAt(p)
		}
	}
	for range 2000 {
		bw, bh := 1+rng.IntN(5), 1+rng.IntN(4)
		ax, ay := rng.IntN(bw), rng.IntN(bh)
		x0, y0, x1, y1 := rng.IntN(w+2)-1, rng.IntN(h+2)-1, rng.IntN(w+2)-1, rng.IntN(h+2)-1
		mask := Mask(rng.IntN(3))
		want := true
		for line := vmath.NewLine(x0, y0, x1, y1).Next(); !line.AtEnd(); line = line.Next() {
			for by := range bh {
				for bx := range bw {
					cx, cy := line.X()-ax+bx, line.Y()-ay+by
					if (bw > 1 || bh > 1) && (cx < 0 || cy < 0 || cx >= w || cy >= h) || x.Blocks(cx, cy, mask) {
						want = false
					}
				}
			}
		}
		if got := x.HasAreaLineOfSight(x0, y0, x1, y1, bw, bh, ax, ay, mask); got != want {
			t.Fatalf("%d×%d body anchored %d,%d from (%d,%d) to (%d,%d) mask %d: %t, its cells say %t",
				bw, bh, ax, ay, x0, y0, x1, y1, mask, got, want)
		}
	}
}
