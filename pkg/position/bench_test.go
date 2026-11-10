package position

import (
	"math/rand/v2"
	"testing"

	"github.com/lixenwraith/vif/pkg/vmath"
)

// benchShapes are the maps the costs are read on: a scrolling level's, a sixth of
// its cells held, and the ceiling's, two fifths of them a maze's walls.
var benchShapes = []struct {
	name          string
	w, h          int
	walls, glyphs int
}{
	{"200x60", 200, 60, 600, 1800},
	{"500x250", 500, 250, 52000, 2000},
}

const (
	benchWall   entity = 1 << 47 // a shared occupant that is a block, its mask the byte above
	benchMask   Mask   = 2
	benchPoints        = 4096
)

var benchBody = vmath.Footprint{Width: 5, Height: 3, HeaderX: 2, HeaderY: 1}

type placed struct {
	e entity
	p Point
}

// benchIndex fills a map of shape s with its walls, one a cell, and its private
// glyphs, and returns it with the glyphs and points to probe.
func benchIndex(s int) (*Index[entity], []placed, []Point) {
	shape := benchShapes[s]
	rng := rand.New(rand.NewPCG(7, uint64(s)))
	x := New[entity](frame{}, func(e entity) (Mask, bool) { return Mask(e >> 48), e&benchWall != 0 })
	x.Resize(shape.w, shape.h, 1)
	for id := 1; id <= shape.walls; {
		p := Point3{X: rng.IntN(shape.w), Y: rng.IntN(shape.h)}
		if x.HasAnyEntityAt(p.X, p.Y, ScopeShared) {
			continue
		}
		x.Set(mk(ClassShared, uint64(id))|benchWall|entity(benchMask)<<48, ClassShared, p)
		x.BlockChangedAt(p)
		id++
	}
	glyphs := make([]placed, shape.glyphs)
	for i := range glyphs {
		glyphs[i] = placed{mk(ClassPrivate, uint64(i+1)), Point{X: rng.IntN(shape.w), Y: rng.IntN(shape.h)}}
		x.Set(glyphs[i].e, ClassPrivate, glyphs[i].p.Layer(0))
	}
	points := make([]Point, benchPoints)
	for i := range points {
		points[i] = Point{X: rng.IntN(shape.w), Y: rng.IntN(shape.h)}
	}
	return x, glyphs, points
}

// benchEach runs fn on every shape
func benchEach(b *testing.B, fn func(b *testing.B, x *Index[entity], glyphs []placed, points []Point)) {
	for s, shape := range benchShapes {
		b.Run(shape.name, func(b *testing.B) {
			x, glyphs, points := benchIndex(s)
			b.ReportAllocs()
			fn(b, x, glyphs, points)
		})
	}
}

// BenchmarkBlocks is one cell's probe, the renderers' and the movers' question.
func BenchmarkBlocks(b *testing.B) {
	benchEach(b, func(b *testing.B, x *Index[entity], _ []placed, points []Point) {
		n, i := 0, 0
		for b.Loop() {
			if p := points[i&(benchPoints-1)]; x.Blocks(p.X, p.Y, benchMask) {
				n++
			}
			i++
		}
		_ = n
	})
}

// BenchmarkBlocksUnderSurfaces is the probe on a map a ceiling and a floor lie on.
func BenchmarkBlocksUnderSurfaces(b *testing.B) {
	benchEach(b, func(b *testing.B, x *Index[entity], _ []placed, points []Point) {
		top, bottom := make([]int, x.Width), make([]int, x.Width)
		for i := range top {
			top[i], bottom[i] = x.Height/8+i%5, x.Height-x.Height/8-i%7
		}
		x.AddSurface(benchMask, Above, 0, top)
		x.AddSurface(benchMask, Below, 0, bottom)
		n, i := 0, 0
		for b.Loop() {
			if p := points[i&(benchPoints-1)]; x.Blocks(p.X, p.Y, benchMask) {
				n++
			}
			i++
		}
		_ = n
	})
}

// BenchmarkBlocksArea is a body's probe: its cells, a few words a row.
func BenchmarkBlocksArea(b *testing.B) {
	benchEach(b, func(b *testing.B, x *Index[entity], _ []placed, points []Point) {
		n, i := 0, 0
		for b.Loop() {
			if p := points[i&(benchPoints-1)]; x.BlocksArea(p.X-benchBody.HeaderX, p.Y-benchBody.HeaderY, benchBody.Width, benchBody.Height, benchMask) {
				n++
			}
			i++
		}
		_ = n
	})
}

// BenchmarkBodyLineOfSight flies a body between two cells some thirty apart.
func BenchmarkBodyLineOfSight(b *testing.B) {
	benchEach(b, func(b *testing.B, x *Index[entity], _ []placed, points []Point) {
		n, i := 0, 0
		for b.Loop() {
			p := points[i&(benchPoints-1)]
			q := points[(i+1)&(benchPoints-1)]
			q = Point{X: p.X + (q.X-p.X)%30, Y: p.Y + (q.Y-p.Y)%12}
			if x.HasAreaLineOfSight(p.X, p.Y, q.X, q.Y, benchBody, benchMask) {
				n++
			}
			i++
		}
		_ = n
	})
}

// BenchmarkOccupants reads the occupants of a cell, most of them empty.
func BenchmarkOccupants(b *testing.B) {
	benchEach(b, func(b *testing.B, x *Index[entity], _ []placed, points []Point) {
		n, i := 0, 0
		for b.Loop() {
			p := points[i&(benchPoints-1)]
			n += len(x.EntitiesAt(p.X, p.Y, ScopeAll))
			i++
		}
		_ = n
	})
}

// BenchmarkMove takes one glyph out of its cell and into the next, as a mover's
// write does.
func BenchmarkMove(b *testing.B) {
	benchEach(b, func(b *testing.B, x *Index[entity], glyphs []placed, _ []Point) {
		i := 0
		for b.Loop() {
			g := &glyphs[i%len(glyphs)]
			x.RemoveEntityAt(g.e, g.p.Layer(0))
			g.p.X = (g.p.X + 1) % x.Width
			x.Set(g.e, ClassPrivate, g.p.Layer(0))
			i++
		}
	})
}

// BenchmarkShift moves every glyph a column, all leaving before any enters, as a
// drifting layer does.
func BenchmarkShift(b *testing.B) {
	benchEach(b, func(b *testing.B, x *Index[entity], glyphs []placed, _ []Point) {
		for b.Loop() {
			for i := range glyphs {
				x.RemoveEntityAt(glyphs[i].e, glyphs[i].p.Layer(0))
				glyphs[i].p.X = (glyphs[i].p.X + 1) % x.Width
			}
			for i := range glyphs {
				x.Set(glyphs[i].e, ClassPrivate, glyphs[i].p.Layer(0))
			}
		}
	})
}

// BenchmarkWallMove moves a wall a cell and brings the planes to it.
func BenchmarkWallMove(b *testing.B) {
	benchEach(b, func(b *testing.B, x *Index[entity], _ []placed, points []Point) {
		x.Blocks(0, 0, benchMask)
		x.Blocks(0, 0, 0)
		wall := mk(ClassShared, 1<<30) | benchWall | entity(benchMask)<<48
		at := points[0].Layer(0)
		x.Set(wall, ClassShared, at)
		x.BlockChangedAt(at)
		i := 0
		for b.Loop() {
			x.RemoveEntityAt(wall, at)
			x.BlockChangedAt(at)
			at = points[i&(benchPoints-1)].Layer(0)
			x.Set(wall, ClassShared, at)
			x.BlockChangedAt(at)
			i++
		}
	})
}

// BenchmarkDirectionalScan finds the nearest glyph right of a cell, then below it,
// within three rows or columns of it, as a motion's marker does.
func BenchmarkDirectionalScan(b *testing.B) {
	benchEach(b, func(b *testing.B, x *Index[entity], _ []placed, points []Point) {
		private := func(e entity) bool { return e.class() == ClassPrivate }
		n, i := 0, 0
		for b.Loop() {
			p := points[i&(benchPoints-1)]
			if _, _, _, ok := x.FindClosestEntityInDirection(p.X, p.Y, 1, 0, Rect{MinY: p.Y - 3, MaxY: p.Y + 3}, ScopeAll, private); ok {
				n++
			}
			if _, _, _, ok := x.FindClosestEntityInDirection(p.X, p.Y, 0, 1, Rect{MinX: p.X - 3, MaxX: p.X + 3}, ScopeAll, private); ok {
				n++
			}
			i++
		}
		_ = n
	})
}

// BenchmarkStats reads the gauges a status snapshot publishes.
func BenchmarkStats(b *testing.B) {
	benchEach(b, func(b *testing.B, x *Index[entity], _ []placed, _ []Point) {
		n := 0
		for b.Loop() {
			n += x.ComputeStats().EntitiesTotal
		}
		_ = n
	})
}

// BenchmarkLoad empties the map and fills it from a copy, cell by cell, then asks
// one probe, which rebuilds the planes: a correction's install.
func BenchmarkLoad(b *testing.B) {
	benchEach(b, func(b *testing.B, x *Index[entity], _ []placed, _ []Point) {
		type held struct {
			index  int32
			shared int
			count  int
		}
		var cells []held
		var occupants []entity
		x.EachOccupied(func(index int32, in []entity, shared int) {
			cells = append(cells, held{index, shared, len(in)})
			occupants = append(occupants, in...)
		})
		w, h := x.Width, x.Height
		for b.Loop() {
			x.Resize(w, h, 1)
			at := 0
			for _, c := range cells {
				x.Load(c.index, occupants[at:at+c.count], c.shared)
				at += c.count
			}
			x.Blocks(0, 0, benchMask)
		}
	})
}
