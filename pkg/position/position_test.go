package position

import (
	"fmt"
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
		x.LaySurface(ceiling, top)
		x.LaySurface(floor, bottom)
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
		if x.Set(e, e.class(), Point3{X: px, Y: py}) > 0 {
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
					if x.Set(e, e.class(), Point3{X: px, Y: py}) > 0 {
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
					want = want || x.Blocks(Point{X: cx, Y: cy}, mask)
				}
			}
			if got := x.BlocksArea(vmath.Area{X: px, Y: py, Width: aw, Height: ah}, mask); got != want {
				t.Fatalf("step %d mask %d area (%d,%d) %dx%d: %t, cells say %t", step, mask, px, py, aw, ah, got, want)
			}
		}
		if step%50 == 0 {
			mask := masks[rng.IntN(len(masks))]
			bits := x.BlockBits(mask, buf)
			buf = bits.Words
			for py := range h {
				for px := range w {
					if got, want := bits.Bit(px, py), x.Blocks(Point{X: px, Y: py}, mask); got != want {
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
		if got := x.Blocks(Point{X: 100, Y: 2}, tt.mask); got != tt.want {
			t.Fatalf("mask %d: %t, want %t", tt.mask, got, tt.want)
		}
		if got := x.BlocksArea(vmath.Area{X: 99, Y: 1, Width: 3, Height: 3}, tt.mask); got != tt.want {
			t.Fatalf("mask %d around the cell: %t, want %t", tt.mask, got, tt.want)
		}
	}
	x.RemoveEntityAt(mk(ClassShared, 1), Point3{X: 100, Y: 2})
	x.BlockChangedAt(Point3{X: 100, Y: 2})
	if x.Blocks(Point{X: 100, Y: 2}, 2) || !x.Blocks(Point{X: 100, Y: 2}, 0) {
		t.Fatal("a block gone still blocks, or the block of mask 0 left with it")
	}
}

// TestASurfaceIsSolidOnItsSideOfItsHeightsOnItsLayer: surfaces of any mask, side,
// width and layer, laid, laid again, lifted and shifted in any order between
// probes, block the cells their own heights say and no other; a frame carried
// into the scroll sees the heights ahead; and a reader of the log re-testing only
// the cells it names keeps every answer.
func TestASurfaceIsSolidOnItsSideOfItsHeightsOnItsLayer(t *testing.T) {
	rng := rand.New(rand.NewPCG(8, 3))
	walls, standing := map[entity]Mask{}, map[Point3]entity{}
	x := New[entity](frame{}, func(e entity) (Mask, bool) { m, ok := walls[e]; return m, ok })
	var w, h, d int
	heights := func(n int) []int { // a profile's, a step a column, or scattered
		out, v, step := make([]int, n), rng.IntN(h+3)-1, 3
		if rng.IntN(5) == 0 {
			step = 2*h + 1
		}
		for i := range out {
			v = min(max(v+rng.IntN(step)-step/2, -1), h+1)
			out[i] = v
		}
		return out
	}
	stops := func(of, mask Mask) bool { return mask == 0 || of&mask != 0 }
	solid := func(c Point3, mask Mask, of func(*Surface, int) (int, bool)) bool {
		for _, s := range x.Surfaces() {
			if v, ok := of(s, c.X); ok && s.Layer == c.Z && stops(s.Mask, mask) && (c.Y < v) == (s.Side == Above) {
				return true
			}
		}
		return false
	}
	blocked := func(c Point3, mask Mask) bool {
		if c.X < 0 || c.Y < 0 || c.Z < 0 || c.X >= w || c.Y >= h || c.Z >= d {
			return false
		}
		e, wall := standing[c]
		return wall && stops(walls[e], mask) || solid(c, mask, (*Surface).Height)
	}
	ahead := func(s *Surface, col int) (int, bool) {
		if i := col - s.Width(); i >= 0 && i < len(s.Ahead()) {
			return s.Ahead()[i], true
		}
		return 0, false
	}
	masks := []Mask{0, 1, 2, 4, 3, 0xFF}
	var reader []bool
	var since uint64
	for round := range 2500 {
		if round%250 == 0 {
			w, h, d = 3+rng.IntN(140), 2+rng.IntN(8), 1+rng.IntN(3)
			x.Resize(w, h, d)
			clear(standing)
		}
		for range 1 + rng.IntN(4) {
			surfaces := x.Surfaces()
			pick := func() *Surface { return surfaces[rng.IntN(len(surfaces))] }
			switch op := rng.IntN(12); {
			case op >= 10:
				c := Point3{X: rng.IntN(w), Y: rng.IntN(h), Z: rng.IntN(d)}
				if e, ok := standing[c]; ok {
					x.RemoveEntityAt(e, c)
					delete(standing, c)
				} else {
					e = mk(ClassShared, uint64(len(walls)+1))
					walls[e], standing[c] = masks[rng.IntN(5)], e
					x.Set(e, ClassShared, c)
				}
				x.BlockChangedAt(c)
			case op == 0 && len(surfaces) < 7:
				mask, layer := masks[rng.IntN(5)], rng.IntN(d+2)-1
				if len(surfaces) > 0 && rng.IntN(2) == 0 { // beside another, as a floor under a ceiling
					beside := pick()
					mask, layer = beside.Mask, beside.Layer
				}
				x.AddSurface(mask, Side(rng.IntN(2)), layer, heights(rng.IntN(w+3)))
			case len(surfaces) == 0:
			case op == 1:
				s := pick()
				s.Mask, s.Side, s.Layer = masks[rng.IntN(5)], Side(rng.IntN(2)), rng.IntN(d+2)-1
				x.LaySurface(s, heights(rng.IntN(w+3)))
			case op == 2:
				x.RemoveSurface(pick())
			case op == 3:
				x.SetSurfaceAhead(pick(), heights(rng.IntN(5)))
			case op < 7: // a scroll: each a column, in any order, which leaves a probe no work
				settled := x.stale == 0
				for _, i := range rng.Perm(len(surfaces)) {
					x.ShiftSurface(surfaces[i], rng.IntN(h+3)-1)
				}
				if settled && x.stale != 0 {
					t.Fatalf("round %d: a scroll of every surface left the probes work %b", round, x.stale)
				}
			default: // some ahead of the rest, or twice
				for range 1 + rng.IntN(3) {
					x.ShiftSurface(pick(), rng.IntN(h+3)-1)
				}
			}
		}
		mask, carry := masks[rng.IntN(len(masks))], rng.IntN(4)
		cells, now, logged := x.BlockChanges(since)
		since = now
		if !logged || len(reader) != w*h*d {
			reader, cells = make([]bool, w*h*d), nil
			for i := range reader {
				reader[i] = x.Blocks(Point3{X: i % w, Y: i / w % h, Z: i / (w * h)}, 0)
			}
		}
		for _, c := range cells {
			if !x.InBounds(c) {
				t.Fatalf("round %d: the log names %v, outside the volume", round, c)
			}
			reader[(c.Z*h+c.Y)*w+c.X] = x.Blocks(c, 0)
		}
		for z := -1; z <= d; z++ {
			for y := -1; y <= h; y++ {
				for px := -1; px <= w; px++ {
					c := Point3{X: px, Y: y, Z: z}
					if got, want := x.Blocks(c, mask), blocked(c, mask); got != want {
						t.Fatalf("round %d mask %d at %v: Blocks %t, the heights say %t", round, mask, c, got, want)
					}
					on := c
					if carry > 0 && px >= 0 {
						on.X += carry
					}
					want := blocked(on, mask) || on != c && y >= 0 && y < h && z >= 0 && z < d && solid(on, mask, ahead)
					if got := x.BlocksCarried(c, carry, mask); got != want {
						t.Fatalf("round %d mask %d at %v carried %d: BlocksCarried %t, the heights say %t", round, mask, c, carry, got, want)
					}
					if x.InBounds(c) && reader[(z*h+y)*w+px] != blocked(c, 0) {
						t.Fatalf("round %d at %v: the log's reader holds %t", round, c, reader[(z*h+y)*w+px])
					}
				}
			}
		}
		if x.stale != 0 {
			t.Fatalf("round %d: the probes left themselves work %b", round, x.stale)
		}
	}
}

// layered builds a volume and, for each of its layers, a flat map holding what
// that layer holds: entities of both halves placed and lifted, blocks of every
// mask, and a surface on each layer with heights ahead of it. Half the changes
// come once the planes stand, so they reach them cell by cell; a flat point
// changes and logs the first layer, which the log read at the half shows.
func layered(t *testing.T, rng *rand.Rand, w, h, d, n int, masks []Mask) (*Index[entity], []*Index[entity]) {
	t.Helper()
	walls := map[entity]Mask{}
	blockOf := func(e entity) (Mask, bool) { m, ok := walls[e]; return m, ok }
	vol := New[entity](frame{}, blockOf)
	vol.Resize(w, h, d)
	all := []*Index[entity]{vol}
	for range d {
		all = append(all, New[entity](frame{}, blockOf))
		all[len(all)-1].Resize(w, h, 1)
	}
	maps := all[1:]
	_, since, _ := vol.BlockChanges(0)
	var changed []Point3
	var held []entity
	for i := range n {
		if i == n/2 {
			if cells, _, ok := vol.BlockChanges(since); !ok || !slices.Equal(cells, changed) {
				t.Fatalf("the volume's log names %v (%t), its blocks changed at %v", cells, ok, changed)
			}
			for _, x := range all {
				for _, m := range masks {
					x.Blocks(Point{}, m)
				}
			}
		}
		e, p := mk(Class(rng.IntN(2)), uint64(i+1)), Point3{X: rng.IntN(w), Y: rng.IntN(h), Z: rng.IntN(d)}
		if rng.IntN(4) == 0 && len(held) > 0 { // one that stands leaves, or one that left returns
			e = held[rng.IntN(len(held))]
			p = changed[slices.Index(held, e)]
		} else if rng.IntN(3) > 0 {
			walls[e] = Mask(1 << rng.IntN(3))
		}
		if p.Z == 0 && i%2 == 0 {
			toggle(vol, e, p.Flat())
		} else {
			toggle(vol, e, p)
		}
		toggle(maps[p.Z], e, p.Flat())
		held, changed = append(held, e), append(changed, p)
	}
	for z, flat := range maps { // a ceiling or a floor a layer, narrower than the map
		side, heights, ahead := Side(z%2), make([]int, w-9+z), make([]int, 6)
		for i := range heights {
			heights[i] = int(side)*(h-3) + rng.IntN(4)
		}
		for i := range ahead {
			ahead[i] = rng.IntN(h)
		}
		vol.SetSurfaceAhead(vol.AddSurface(Mask(1<<(z%3)), side, z, heights), ahead)
		flat.SetSurfaceAhead(flat.AddSurface(Mask(1<<(z%3)), side, 0, heights), ahead)
	}
	return vol, maps
}

// toggle places e on p's cell or, standing there, lifts it, and logs the cell
func toggle[P Coord](x *Index[entity], e entity, p P) {
	if x.Holds(e, e.class(), p) {
		x.RemoveEntityAt(e, p)
	} else {
		x.Set(e, e.class(), p)
	}
	x.BlockChangedAt(p)
}

// TestALayerOfAVolumeAnswersAsAMapHoldingIt: every query a point, a region or a
// body takes answers on a layer of a volume as on a flat map holding what that
// layer holds, its cells named in the layer, a body one layer deep as the flat
// one; the flat forms answer on a volume as its first layer; and a layer the
// volume does not hold is outside it.
func TestALayerOfAVolumeAnswersAsAMapHoldingIt(t *testing.T) {
	rng := rand.New(rand.NewPCG(21, 4))
	const w, h, d = 70, 12, 3
	masks := []Mask{0, 1, 2, 4, 3}
	vol, maps := layered(t, rng, w, h, d, 420, masks)
	same := func(c Point) Point { return c }
	for i := range 3000 {
		z, mask, scope := rng.IntN(d), masks[rng.IntN(len(masks))], Scope(1+rng.IntN(3))
		p := Point{X: rng.IntN(w+6) - 3, Y: rng.IntN(h+6) - 3}
		q := Point{X: rng.IntN(w+6) - 3, Y: rng.IntN(h+6) - 3}
		area := vmath.Area{X: p.X, Y: p.Y, Width: 1 + rng.IntN(5), Height: 1 + rng.IntN(4)}
		foot := vmath.Footprint{Width: 1 + rng.IntN(4), Height: 1 + rng.IntN(3)}
		foot.HeaderX, foot.HeaderY = rng.IntN(foot.Width), rng.IntN(foot.Height)
		inLayer := func(c Point3) Point {
			if c != (Point3{}) && c.Z != z {
				t.Fatalf("query %d on layer %d names %v", i, z, c)
			}
			return c.Flat()
		}
		want := fmt.Sprint(answers(t, maps[z], p, q, area, foot, mask, scope, i, same))
		if got := fmt.Sprint(answers(t, vol, p.Layer(z), q.Layer(z), area.Layer(z), foot, mask, scope, i, inLayer)); got != want {
			t.Fatalf("query %d from %v to %v on layer %d, area %+v, body %+v, mask %d, scope %d:\nthe volume %s\nthe map    %s",
				i, p, q, z, area, foot, mask, scope, got, want)
		}
		if got := fmt.Sprint(answers(t, vol, p.Layer(z), q.Layer(z), area.Layer(z), foot.Deep(1), mask, scope, i, inLayer)); got != want {
			t.Fatalf("query %d on layer %d, body %+v one layer deep:\nthe volume %s\nthe map    %s", i, z, foot, got, want)
		}
		if z != 0 {
			continue
		}
		if got := fmt.Sprint(answers(t, vol, p, q, area, foot, mask, scope, i, same)); got != want {
			t.Fatalf("query %d from %v to %v, area %+v, body %+v, mask %d, scope %d:\nthe volume's flat forms %s\nthe map                %s",
				i, p, q, area, foot, mask, scope, got, want)
		}
	}
	for _, z := range []int{-1, d} {
		p, e := Point3{X: rng.IntN(w), Y: rng.IntN(h), Z: z}, mk(ClassShared, 9000)
		if vol.InBounds(p) || vol.Blocks(p, 0) || !vol.IsBlocked(p, 0) || vol.Set(e, e.class(), p) != 0 ||
			vol.HasAnyEntityAt(p, ScopeAll) || vol.EntitiesAt(p, ScopeAll) != nil || vol.Occupants(p, ScopeAll) != nil {
			t.Fatalf("%v, on a layer a volume of %d does not hold, lies in it", p, d)
		}
		_, _, scanned := vol.FindClosestEntityInDirection(p, 1, 0, Rect{MaxX: w, MaxY: h}, ScopeAll, nil)
		_, landed := vol.FindFreeAreaSpiral(p, vmath.CellFootprint, 0, 0)
		_, placed := vol.FindFreeFromPattern(p, 1, 1, PatternCardinalFirst, 1, 4, false, 0, nil)
		if scanned || landed || placed {
			t.Fatalf("a search from %v, on a layer a volume of %d does not hold, found a cell (%t, %t, %t)", p, d, scanned, landed, placed)
		}
	}
}

// answers is what every query of x says of p, q, a region and a body, the cells
// it names through flat and n varying what else a query takes; a search that
// finds nothing must name the zero point
func answers[P Coord, R Region, F Body](t *testing.T, x *Index[entity], p, q P, r R, f F, mask Mask, scope Scope, n int, flat func(P) Point) []any {
	t.Helper()
	out := []any{
		x.InBounds(p), x.Blocks(p, mask), x.IsBlocked(p, mask), x.BlocksCarried(p, n%5, mask),
		slices.Clone(x.EntitiesAt(p, scope)), slices.Clone(x.Occupants(p, scope)), x.HasAnyEntityAt(p, scope),
		x.BlocksArea(r, mask), x.IsAreaFree(r, mask), x.HasLineOfSight(p, q, mask), x.HasAreaLineOfSight(p, q, f, mask),
	}
	for _, e := range x.EntitiesAt(p, ScopeAll) {
		out = append(out, x.Holds(e, e.class(), p), x.Holds(e, e.class(), q))
	}
	last, free := x.FindLastFreeOnRay(p, q, mask)
	out = append(out, flat(last), free)
	search := func(c P, found bool) {
		var none P
		if !found && c != none {
			t.Fatalf("search %d found nothing and names %v", n, c)
		}
		out = append(out, flat(c), found)
	}
	search(x.FindFreeAreaSpiral(p, f, mask, n%7))
	search(x.FindFreeAreaInside(p, f, mask, n%7-1))
	var walked []Point
	search(x.SpiralSearch(p, 1+n%3, func(c P) bool {
		walked = append(walked, flat(c))
		return len(walked) > 4+n%20
	}))
	out = append(out, walked)
	refuse := func(topLeft P, w, h int) bool { c := flat(topLeft); return (c.X+c.Y+w+h)%3 != 0 }
	search(x.FindFreeFromPattern(p, 1+n%3, 1+n%2, Pattern(n%2), 1, 6, n%4 < 2, mask, nil))
	search(x.FindFreeFromPattern(p, 1+n%3, 1+n%2, Pattern(n%2), 1, 6, n%4 < 2, mask, refuse))
	dx, dy, placed := x.FindPlacementAroundExclusion(p, 1+n%4, 1+n%2, Exclusion{Left: n % 3, Right: 1, Top: n % 2, Bottom: 2}, n%2, Pattern(n%2), mask, refuse)
	out = append(out, dx, dy, placed)
	dir := [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}[n%4]
	c, _ := at(q)
	e, cell, found := x.FindClosestEntityInDirection(p, dir[0], dir[1], Rect{MinX: c.X - 40, MaxX: c.X + 9, MinY: c.Y - 3, MaxY: c.Y + 2}, scope,
		func(e entity) bool { return e&1 != 0 })
	search(cell, found)
	return append(out, e)
}

// TestABodyWithDepthStandsWhereEachLayerOfItDoes: a box is free, and a body with
// depth fits, lands and flies a line, where its columns and rows do on a map
// holding each layer it spans, and nowhere a layer of it lies outside the
// volume; a box blocks where a layer of it the volume holds does; and a flat
// point carries such a body from the first layer.
func TestABodyWithDepthStandsWhereEachLayerOfItDoes(t *testing.T) {
	rng := rand.New(rand.NewPCG(13, 8))
	const w, h, d = 40, 10, 4
	masks := []Mask{0, 1, 2, 4, 3}
	vol, maps := layered(t, rng, w, h, d, 260, masks)
	for i := range 3000 {
		mask, radius := masks[rng.IntN(len(masks))], i%6
		f := vmath.Footprint{Width: 1 + rng.IntN(4), Height: 1 + rng.IntN(3)}.Deep(1 + rng.IntN(3))
		f.HeaderX, f.HeaderY, f.HeaderZ = rng.IntN(f.Width), rng.IntN(f.Height), rng.IntN(f.Depth)
		stands := func(c Point3) bool {
			for l := c.Z - f.HeaderZ; l < c.Z-f.HeaderZ+f.Depth; l++ {
				if l < 0 || l >= d || !maps[l].IsAreaFree(f.Flat().At(c.X, c.Y), mask) {
					return false
				}
			}
			return true
		}
		c := Point3{X: rng.IntN(w+4) - 2, Y: rng.IntN(h+4) - 2, Z: rng.IntN(d+2) - 1}
		blocks := false
		for l := max(c.Z-f.HeaderZ, 0); l < min(c.Z-f.HeaderZ+f.Depth, d); l++ {
			blocks = blocks || maps[l].BlocksArea(f.Flat().At(c.X, c.Y), mask)
		}
		if free, blocked := vol.IsAreaFree(f.At(c), mask), vol.BlocksArea(f.At(c), mask); free != stands(c) || blocked != blocks {
			t.Fatalf("body %+v at %v, mask %d: free %t and blocked %t, its layers say %t and %t", f, c, mask, free, blocked, stands(c), blocks)
		}
		corner := func(header Point3, found bool) (Point3, bool) {
			if !found {
				return Point3{}, false
			}
			return header.Sub(Point3{X: f.HeaderX, Y: f.HeaderY, Z: f.HeaderZ}), true
		}
		want, found := corner(vol.SpiralSearch(c, radius, stands))
		if got, ok := vol.FindFreeAreaSpiral(c, f, mask, radius); got != want || ok != found {
			t.Fatalf("body %+v from %v, mask %d, radius %d: lands at %v (%t), the walk's first cell it stands on gives %v (%t)",
				f, c, mask, radius, got, ok, want, found)
		}
		in := Point3{
			X: max(f.HeaderX, min(c.X, w-f.Width+f.HeaderX)),
			Y: max(f.HeaderY, min(c.Y, h-f.Height+f.HeaderY)),
			Z: max(f.HeaderZ, min(c.Z, d-f.Depth+f.HeaderZ)),
		}
		want, found = corner(vol.SpiralSearch(in, radius, stands))
		if got, ok := vol.FindFreeAreaInside(c, f, mask, radius); got != want || ok != found {
			t.Fatalf("body %+v from %v pulled in to %v, mask %d, radius %d: lands at %v (%t), want %v (%t)",
				f, c, in, mask, radius, got, ok, want, found)
		}
		a := Point3{X: rng.IntN(w+2) - 1, Y: rng.IntN(h+2) - 1, Z: rng.IntN(d)}
		b := Point3{X: rng.IntN(w+2) - 1, Y: rng.IntN(h+2) - 1, Z: rng.IntN(d)}
		sight, flies := true, true
		for l := vmath.NewLine3(a, b).Next(); !l.AtEnd(); l = l.Next() {
			cell := l.Point()
			sight = sight && !maps[cell.Z].Blocks(cell.Flat(), mask)
			flies = flies && stands(cell)
		}
		if f.Width == 1 && f.Height == 1 && f.Depth == 1 { // a body of one cell is a point's line of sight
			flies = sight
		}
		if got := vol.HasLineOfSight(a, b, mask); got != sight {
			t.Fatalf("mask %d %v to %v: HasLineOfSight %t, the line's cells say %t", mask, a, b, got, sight)
		}
		if got := vol.HasAreaLineOfSight(a, b, f, mask); got != flies {
			t.Fatalf("body %+v, mask %d, %v to %v: HasAreaLineOfSight %t, the line's cells say %t", f, mask, a, b, got, flies)
		}
		a.Z, b.Z, c.Z = 0, 0, 0
		got, ok := vol.FindFreeAreaSpiral(c.Flat(), f, mask, radius)
		if want, found := vol.FindFreeAreaSpiral(c, f, mask, radius); got != want.Flat() || ok != found ||
			vol.HasAreaLineOfSight(a.Flat(), b.Flat(), f, mask) != vol.HasAreaLineOfSight(a, b, f, mask) {
			t.Fatalf("body %+v, mask %d: from the flat points %v, %v and %v it answers otherwise than from their cells of the first layer", f, mask, c, a, b)
		}
	}
}

// TestARayStopsBeforeTheFirstCellThatBlocks: a ray from a start, taken as free,
// toward an end names the last cell of the line before the first that blocks,
// the end when none does, and reports whether none did
func TestARayStopsBeforeTheFirstCellThatBlocks(t *testing.T) {
	rng := rand.New(rand.NewPCG(6, 17))
	const w, h = 40, 12
	_, maps := layered(t, rng, w, h, 1, 60, []Mask{0, 1, 2})
	for range 2000 {
		a := Point{X: rng.IntN(w+4) - 2, Y: rng.IntN(h+4) - 2}
		b := Point{X: rng.IntN(w+4) - 2, Y: rng.IntN(h+4) - 2}
		mask := Mask(rng.IntN(3))
		want, free := a, true
		for line := vmath.NewLine(a.X, a.Y, b.X, b.Y); free && !line.AtEnd(); {
			line = line.Next()
			if cell := (Point{X: line.X(), Y: line.Y()}); maps[0].Blocks(cell, mask) {
				free = false
			} else {
				want = cell
			}
		}
		if got, ok := maps[0].FindLastFreeOnRay(a, b, mask); got != want || ok != free {
			t.Fatalf("mask %d from %v toward %v: the ray ends at %v (%t), the line's cells say %v (%t)", mask, a, b, got, ok, want, free)
		}
	}
}

// TestAPlacementAroundAnExclusionTakesItsPassesInOrder: the first of the 8 slots, in
// the pattern's order turning away from the map's centre column, that lies inside
// the map, the caller's check takes and no wall stands in; then at twice the
// distance; then without the check; then clamped to the map; then, found false,
// the slot over the fewest blocked cells, the first among equals; on its layer
func TestAPlacementAroundAnExclusionTakesItsPassesInOrder(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 13))
	orders := map[Pattern][2][8]int{
		PatternCardinalFirst: {anchorRelativeCardinalFirstCW, anchorRelativeCardinalFirstCCW},
		PatternDiagonalFirst: {anchorRelativeDiagonalFirstCW, anchorRelativeDiagonalFirstCCW},
	}
	for round := range 300 {
		w, h, layer := 3+rng.IntN(28), 2+rng.IntN(10), round%3
		walls := map[entity]Mask{}
		blockOf := func(e entity) (Mask, bool) { m, ok := walls[e]; return m, ok }
		x, vol := New[entity](frame{}, blockOf), New[entity](frame{}, blockOf)
		x.Resize(w, h, 1)
		vol.Resize(w, h, 3)
		for i, density := 0, 5+rng.IntN(90); i < 3*w*h; i++ {
			if e := mk(ClassShared, uint64(i+1)); rng.IntN(100) < density {
				walls[e] = Mask(1 << rng.IntN(2))
				c := Point3{X: i % w, Y: i / w % h, Z: i / (w * h)}
				if toggle(vol, e, c); c.Z == layer {
					toggle(x, e, c.Flat())
				}
			}
		}
		for range 60 {
			anchor := Point{X: rng.IntN(w+8) - 4, Y: rng.IntN(h+6) - 3}
			ow, oh := 1+rng.IntN(w+3), 1+rng.IntN(h+2) // a body larger than the map among them
			exclusion := Exclusion{Left: rng.IntN(5), Right: rng.IntN(5), Top: rng.IntN(3), Bottom: rng.IntN(3)}
			padding, pattern, mask := rng.IntN(3), Pattern(rng.IntN(2)), Mask(rng.IntN(4))
			var asked []Point
			check := []func(Point, int, int) bool{
				nil,
				func(p Point, _, _ int) bool { asked = append(asked, p); return (p.X+p.Y)&1 == 0 },
				func(p Point, _, _ int) bool { asked = append(asked, p); return false },
			}[rng.IntN(3)]
			dx, dy, found := x.FindPlacementAroundExclusion(anchor, ow, oh, exclusion, padding, pattern, mask, check)
			got := asked
			asked = nil
			order := orders[pattern][searchDirection(anchor.X, w/2)]
			wx, wy, placed := placedAround(x, anchor, ow, oh, exclusionSlots(ow, oh, exclusion, padding), order, mask, check)
			if dx != wx || dy != wy || found != placed || !slices.Equal(got, asked) {
				t.Fatalf("%d×%d around %v on %d×%d, %+v padded %d, pattern %d, mask %d: (%d, %d) %t having asked %v, cell by cell (%d, %d) %t asking %v",
					ow, oh, anchor, w, h, exclusion, padding, pattern, mask, dx, dy, found, got, wx, wy, placed, asked)
			}
			var deep func(Point3, int, int) bool
			if asked = nil; check != nil {
				deep = func(p Point3, pw, ph int) bool { return p.Z == layer && check(p.Flat(), pw, ph) }
			}
			vx, vy, there := vol.FindPlacementAroundExclusion(anchor.Layer(layer), ow, oh, exclusion, padding, pattern, mask, deep)
			if vx != dx || vy != dy || there != found || !slices.Equal(got, asked) {
				t.Fatalf("%d×%d around %v on layer %d of a volume: (%d, %d) %t having asked %v, a map holding the layer (%d, %d) %t asking %v",
					ow, oh, anchor, layer, vx, vy, there, asked, dx, dy, found, got)
			}
		}
	}
}

// placedAround is FindPlacementAroundExclusion read a cell at a time
func placedAround(x *Index[entity], anchor Point, ow, oh int, slots [8][2]int, order [8]int, mask Mask, check func(Point, int, int) bool) (int, int, bool) {
	slot := func(i, scale int) Point {
		return Point{X: anchor.X + slots[i][0]*scale, Y: anchor.Y + slots[i][1]*scale}
	}
	clamped := func(i, scale int) Point {
		p := slot(i, scale)
		return Point{X: max(0, min(p.X, x.Width-ow)), Y: max(0, min(p.Y, x.Height-oh))}
	}
	walls := func(p Point) (n int) {
		for cy := p.Y; cy < p.Y+oh; cy++ {
			for cx := p.X; cx < p.X+ow; cx++ {
				if x.Blocks(Point{X: cx, Y: cy}, mask) {
					n++
				}
			}
		}
		return n
	}
	for _, pass := range [3]struct {
		scale   int
		checked bool
	}{{1, true}, {2, true}, {1, false}} {
		for _, i := range order {
			p := slot(i, pass.scale)
			if p.X < 0 || p.Y < 0 || p.X+ow > x.Width || p.Y+oh > x.Height {
				continue
			}
			if (!pass.checked || check == nil || check(p, ow, oh)) && walls(p) == 0 {
				return p.X - anchor.X, p.Y - anchor.Y, true
			}
		}
	}
	for _, i := range order {
		if p := clamped(i, 1); walls(p) == 0 {
			return p.X - anchor.X, p.Y - anchor.Y, true
		}
	}
	best, least := Point{}, -1
	for _, scale := range [2]int{1, 2} {
		for _, i := range order {
			if p := clamped(i, scale); least < 0 || walls(p) < least {
				best, least = p, walls(p)
			}
		}
	}
	return best.X - anchor.X, best.Y - anchor.Y, false
}

// TestAVolumeKeepsTheLayersTheCellCeilingAllows: a volume keeps the columns and
// rows it was asked and as many of the layers as leave it under the ceiling
func TestAVolumeKeepsTheLayersTheCellCeilingAllows(t *testing.T) {
	x := New[entity](frame{}, func(entity) (Mask, bool) { return 0, false })
	for _, tt := range []struct{ w, h, d, want int }{{9, 6, 4, 4}, {500, 250, 3, 1}, {100, 50, 30, 25}} {
		if x.Resize(tt.w, tt.h, tt.d); x.Depth != tt.want || len(x.cells) != tt.w*tt.h*tt.want {
			t.Fatalf("%d×%d×%d took depth %d and %d cells, want %d layers", tt.w, tt.h, tt.d, x.Depth, len(x.cells), tt.want)
		}
	}
}

// TestADirectionalScanAnswersAsItsCells: the scan over the presence words finds
// what a walk of every cell finds, row by row or column by column from the start:
// the first holding an occupant the scope and the filter take, and of those the
// one nearest the start on the cross axis, the first met winning a tie.
func TestADirectionalScanAnswersAsItsCells(t *testing.T) {
	rng := rand.New(rand.NewPCG(4, 6))
	x := New[entity](frame{}, func(entity) (Mask, bool) { return 0, false })
	const w, h = 150, 9
	x.Resize(w, h, 1)
	for i := range 160 { // sparse, with a few cells of several
		e := mk(Class(rng.IntN(2)), uint64(i+1))
		x.Set(e, e.class(), Point3{X: rng.IntN(w) / (1 + rng.IntN(3)), Y: rng.IntN(h)})
	}
	walk := func(sx, sy, dx, dy int, b Rect, scope Scope, filter func(entity) bool) (entity, int, int, bool) {
		along, across, end, lo, hi := sx+dx, sy, w, max(b.MinY, 0), min(b.MaxY+1, h)
		if dy != 0 {
			along, across, end, lo, hi = sy+dy, sx, h, max(b.MinX, 0), min(b.MaxX+1, w)
		}
		for ; along >= 0 && along < end; along += dx + dy {
			var best entity
			at, nearest := -1, 0
			for c := lo; c < hi; c++ {
				px, py := along, c
				if dy != 0 {
					px, py = c, along
				}
				for _, e := range x.EntitiesAt(Point{X: px, Y: py}, scope) {
					if d := vmath.IntAbs(c - across); (filter == nil || filter(e)) && (at < 0 || d < nearest) {
						best, at, nearest = e, c, d
					}
				}
			}
			if at >= 0 && dy != 0 {
				return best, at, along, true
			} else if at >= 0 {
				return best, along, at, true
			}
		}
		return 0, -1, -1, false
	}
	odd := func(e entity) bool { return e&1 != 0 }
	for i := range 20000 {
		sx, sy := rng.IntN(w+6)-3, rng.IntN(h+6)-3
		dir := [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}[rng.IntN(4)]
		b := Rect{MinX: rng.IntN(w+8) - 4, MinY: rng.IntN(h+4) - 2}
		b.MaxX, b.MaxY = b.MinX+rng.IntN(140)-2, b.MinY+rng.IntN(8)-2
		scope := Scope(1 + rng.IntN(3))
		filter := odd
		if i%3 == 0 {
			filter = nil
		}
		ge, at, gok := x.FindClosestEntityInDirection(Point{X: sx, Y: sy}, dir[0], dir[1], b, scope, filter)
		we, wx, wy, wok := walk(sx, sy, dir[0], dir[1], b, scope, filter)
		if ge != we || gok != wok || gok && (at != Point{X: wx, Y: wy}) {
			t.Fatalf("from (%d,%d) by %v within %+v, scope %d: found %v at %v %t, the cells say %v at (%d,%d) %t",
				sx, sy, dir, b, scope, ge, at, gok, we, wx, wy, wok)
		}
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
		if x.Set(e, e.class(), p) > 0 {
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
					if (bw > 1 || bh > 1) && (cx < 0 || cy < 0 || cx >= w || cy >= h) || x.Blocks(Point{X: cx, Y: cy}, mask) {
						want = false
					}
				}
			}
		}
		if got := x.HasAreaLineOfSight(Point{X: x0, Y: y0}, Point{X: x1, Y: y1}, vmath.Footprint{Width: bw, Height: bh, HeaderX: ax, HeaderY: ay}, mask); got != want {
			t.Fatalf("%d×%d body anchored %d,%d from (%d,%d) to (%d,%d) mask %d: %t, its cells say %t",
				bw, bh, ax, ay, x0, y0, x1, y1, mask, got, want)
		}
	}
}
