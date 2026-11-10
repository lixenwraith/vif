package vmath

import (
	"math"
	"testing"
)

// signedRand returns a value in [-bound, bound)
func signedRand(rng *FastRand, bound int64) int64 {
	return int64(rng.Next()%uint64(2*bound)) - bound
}

func TestScalarHelpers(t *testing.T) {
	if got := Lerp(-3, 11, 0.5); math.Abs(got-4) > 1e-12 {
		t.Errorf("Lerp midpoint = %v, want 4", got)
	}
	if Clamp(-2, -1, 1) != -1 || Clamp(2, -1, 1) != 1 || Clamp(0.5, -1, 1) != 0.5 {
		t.Error("Clamp")
	}
	if Sign(-5) != -1 || Sign(0) != 0 || Sign(5) != 1 {
		t.Error("Sign")
	}
	if Abs(-5) != 5 || Abs(5) != 5 {
		t.Error("Abs")
	}
}

// A ramp covers the area under its rate, alike in one step or many, and holds its end
// once it gets there; its rate stays exact over a span whose product overflows
func TestARampCoversTheAreaUnderItsRate(t *testing.T) {
	const tick = 50_000_000
	r := Ramp{}.EaseTo(20*MicroRate, 20*tick)
	var ticked int64
	for range 40 {
		var d int64
		d, r = r.Cover(tick)
		ticked += d
	}
	// A second easing up to 20 cells a second covers 10 cells, and the next at 20 covers 20
	once, _ := Ramp{}.EaseTo(20*MicroRate, 20*tick).Cover(40 * tick)
	if ticked != 30*CellSpan || once != ticked || r != (Ramp{To: 20 * MicroRate}) {
		t.Fatalf("covered %d a tick at a time and %d at once, want %d; ramp %+v", ticked, once, 30*CellSpan, r)
	}
	if got := (Ramp{To: 4e8, Done: 3e17, Span: 4e17}).Rate(); got != 3e8 {
		t.Fatalf("rate three quarters through = %d, want 3e8", got)
	}
}

// --- grid types ---

func TestPointCenterRoundTrip(t *testing.T) {
	for _, p := range []Point{{X: 0, Y: 0}, {X: 1, Y: 2}, {X: -1, Y: -2}, {X: -7, Y: 13}, {X: 1000, Y: -1000}} {
		px, py := p.Center()
		if got := PointAt(px, py); got != p {
			t.Errorf("PointAt(%v.Center()) = %v", p, got)
		}
	}
	// int() truncates toward zero; the grid needs floor
	if got := PointAt(-0.25, -1.75); got != (Point{X: -1, Y: -2}) {
		t.Errorf("PointAt negative = %v, want (-1,-2)", got)
	}
}

func TestPointAddSub(t *testing.T) {
	a, b := Point{X: 3, Y: -4}, Point{X: -1, Y: 6}
	if got := a.Add(b); got != (Point{X: 2, Y: 2}) {
		t.Errorf("Add = %v", got)
	}
	if got := a.Sub(b); got != (Point{X: 4, Y: -10}) {
		t.Errorf("Sub = %v", got)
	}
}

func TestAreaContainsBoundaries(t *testing.T) {
	a := Area{X: 2, Y: 3, Width: 4, Height: 5} // x in [2,6), y in [3,8)
	inside := []Point{{X: 2, Y: 3}, {X: 5, Y: 7}, {X: 3, Y: 5}}
	outside := []Point{{X: 1, Y: 3}, {X: 6, Y: 7}, {X: 2, Y: 2}, {X: 2, Y: 8}}
	for _, p := range inside {
		if !a.ContainsPoint(p) {
			t.Errorf("%v should be inside", p)
		}
	}
	for _, p := range outside {
		if a.ContainsPoint(p) {
			t.Errorf("%v should be outside", p)
		}
	}
}

func TestAreaCenter(t *testing.T) {
	if got := (Area{X: 2, Y: 3, Width: 4, Height: 5}).Center(); got != (Point{X: 4, Y: 5}) {
		t.Errorf("Center = %v", got)
	}
}

func TestAreaDistributePointCoversArea(t *testing.T) {
	a := Area{X: 5, Y: 9, Width: 4, Height: 3}
	rng := NewFastRand(1)
	seen := make(map[Point]bool)
	for i := range a.Width * a.Height {
		p := a.DistributePoint(i, rng)
		if !a.ContainsPoint(p) {
			t.Fatalf("index %d -> %v outside area", i, p)
		}
		if seen[p] {
			t.Fatalf("index %d -> duplicate %v", i, p)
		}
		seen[p] = true
	}
	if len(seen) != a.Width*a.Height {
		t.Fatalf("covered %d of %d cells", len(seen), a.Width*a.Height)
	}
}

func TestAreaRandomPointInside(t *testing.T) {
	a := Area{X: -3, Y: 7, Width: 6, Height: 2}
	rng := NewFastRand(99)
	for range 2000 {
		if p := a.RandomPoint(rng); !a.ContainsPoint(p) {
			t.Fatalf("%v outside %v", p, a)
		}
	}
}

func TestAreaRandomPointSingleCellPreservesRNG(t *testing.T) {
	a := Area{X: 1, Y: 1, Width: 1, Height: 1}
	r1, r2 := NewFastRand(42), NewFastRand(42)
	if got := a.RandomPoint(r1); got != (Point{X: 1, Y: 1}) {
		t.Fatalf("RandomPoint = %v", got)
	}
	if r1.Next() != r2.Next() {
		t.Fatal("single-cell area consumed RNG state; spawn determinism would drift")
	}
}

// TestAVolumeOneLayerDeepIsItsFlatMap: on one layer a cell, an area and a body of
// the volume are the flat map's: the same centre and cell-of, the same cells
// held, the same draws from one stream, the same fit and cover.
func TestAVolumeOneLayerDeepIsItsFlatMap(t *testing.T) {
	rng, twin := NewFastRand(0x3d), NewFastRand(0x3d)
	for range 2000 {
		p, z := Point{X: int(signedRand(rng, 64)), Y: int(signedRand(rng, 64))}, int(signedRand(rng, 8))
		twin.SetState(rng.State())
		px, py := p.Center()
		if c := p.Layer(z).Center(); c != (Vec3{px, py, float64(z) + CellCenter}) || PointAt3(c) != p.Layer(z) || p.Layer(z).Flat() != p {
			t.Fatalf("cell %v on layer %d centres on %v", p, z, c)
		}
		a := Area{X: p.X, Y: p.Y, Width: 1 + rng.Intn(6), Height: 1 + rng.Intn(6)}
		b := a.Layer(z)
		twin.SetState(rng.State())
		if b.Flat() != a || b.Center() != a.Center().Layer(z) ||
			b.RandomPoint(rng) != a.RandomPoint(twin).Layer(z) || rng.State() != twin.State() {
			t.Fatalf("area %+v on layer %d is box %+v", a, z, b)
		}
		for i := -1; i <= a.Width*a.Height; i++ {
			if got, want := b.DistributePoint(i, rng), a.DistributePoint(i, twin).Layer(z); got != want {
				t.Fatalf("box %+v distributes index %d to %v, its area to %v", b, i, got, want)
			}
		}
		q := Point{X: p.X + rng.Intn(9) - 2, Y: p.Y + rng.Intn(9) - 2}
		if b.ContainsPoint(q.Layer(z)) != a.ContainsPoint(q) || b.ContainsPoint(q.Layer(z+1)) || b.ContainsPoint(q.Layer(z-1)) {
			t.Fatalf("box %+v and area %+v part on cell %v", b, a, q)
		}
		f := Footprint{Width: a.Width, Height: a.Height, HeaderX: rng.Intn(8) - 1, HeaderY: rng.Intn(8) - 1}
		o := Footprint{Width: 1 + rng.Intn(6), Height: 1 + rng.Intn(6), HeaderX: rng.Intn(6), HeaderY: rng.Intn(6)}
		if f.Deep(1).At(q.Layer(z)) != f.At(q.X, q.Y).Layer(z) || f.Deep(1).Valid() != f.Valid() ||
			f.Deep(1).Covers(o.Deep(1)) != f.Covers(o) || f.Deep(1).Flat() != f {
			t.Fatalf("footprint %+v one layer deep parts from it against %+v", f, o)
		}
	}
}

// TestABoxHoldsEachOfItsCellsOnce: a box's cells are its width, height and depth
// from its corner, the far faces outside; an index distributes to each once, a
// draw lands inside, and a box of one cell draws nothing from the stream.
func TestABoxHoldsEachOfItsCellsOnce(t *testing.T) {
	b := Box{X: 5, Y: -9, Z: 2, Width: 4, Height: 3, Depth: 5}
	rng := NewFastRand(1)
	seen := make(map[Point3]bool)
	for i := range b.Width * b.Height * b.Depth {
		p := b.DistributePoint(i, rng)
		if !b.ContainsPoint(p) || seen[p] {
			t.Fatalf("index %d distributes to %v, outside the box or held twice", i, p)
		}
		seen[p] = true
	}
	for _, p := range []Point3{{X: 4, Y: -9, Z: 2}, {X: 9, Y: -9, Z: 2}, {X: 5, Y: -10, Z: 2}, {X: 5, Y: -6, Z: 2}, {X: 5, Y: -9, Z: 1}, {X: 5, Y: -9, Z: 7}} {
		if b.ContainsPoint(p) {
			t.Fatalf("box %+v holds %v, a cell past one of its faces", b, p)
		}
	}
	if c := b.Center(); c != (Point3{X: 7, Y: -8, Z: 4}) {
		t.Fatalf("box %+v centres on %v", b, c)
	}
	for range 2000 {
		if p := b.RandomPoint(rng); !b.ContainsPoint(p) {
			t.Fatalf("box %+v drew %v", b, p)
		}
	}
	before := rng.State()
	if p := (Box{X: 1, Y: 2, Z: 3, Width: 1, Height: 1, Depth: 1}).RandomPoint(rng); p != (Point3{X: 1, Y: 2, Z: 3}) || rng.State() != before {
		t.Fatalf("a box of one cell drew %v and moved the stream", p)
	}
}

// TestAFootprintWithDepthIsABodyAroundItsHeader: its box lies around the header
// on every axis; it is a body with a layer or more and its header among them,
// and covers one that reaches no further before or behind the header.
func TestAFootprintWithDepthIsABodyAroundItsHeader(t *testing.T) {
	f := Footprint3{Width: 3, Height: 2, Depth: 4, HeaderX: 1, HeaderY: 0, HeaderZ: 2}
	if got, want := f.At(Point3{X: 10, Y: 20, Z: 5}), (Box{X: 9, Y: 20, Z: 3, Width: 3, Height: 2, Depth: 4}); got != want {
		t.Fatalf("footprint %+v stands on %+v, want %+v", f, got, want)
	}
	for _, tt := range []struct {
		depth, headerZ int
		valid          bool
	}{{4, 2, true}, {1, 0, true}, {0, 0, false}, {4, 4, false}, {4, -1, false}} {
		g := f
		g.Depth, g.HeaderZ = tt.depth, tt.headerZ
		if g.Valid() != tt.valid {
			t.Fatalf("footprint %+v valid %v, want %v", g, g.Valid(), tt.valid)
		}
	}
	for _, tt := range []struct {
		depth, headerZ int
		covered        bool
	}{{4, 2, true}, {2, 1, true}, {3, 2, true}, {5, 2, false}, {4, 3, false}} {
		o := f
		o.Depth, o.HeaderZ = tt.depth, tt.headerZ
		if f.Covers(o) != tt.covered {
			t.Fatalf("footprint %+v covers %+v: %v, want %v", f, o, f.Covers(o), tt.covered)
		}
	}
}

// --- rng ---

func TestFastRandDeterministic(t *testing.T) {
	a, b := NewFastRand(0xDEADBEEF), NewFastRand(0xDEADBEEF)
	for i := range 1000 {
		if x, y := a.Next(), b.Next(); x != y {
			t.Fatalf("draw %d diverged: %d != %d", i, x, y)
		}
	}
}

func TestFastRandZeroSeed(t *testing.T) {
	r := NewFastRand(0)
	for range 100 {
		if r.Next() == 0 {
			t.Fatal("xorshift collapsed to zero state")
		}
	}
}

func TestFastRandFloat64Range(t *testing.T) {
	r := NewFastRand(6)
	for range 10000 {
		if v := r.Float64(); v < 0 || v >= 1 {
			t.Fatalf("Float64 = %v", v)
		}
	}
}

func TestFastRandIntnUniform(t *testing.T) {
	const n, draws = 16, 1 << 20
	r := NewFastRand(0x1234)
	var counts [n]int
	for range draws {
		counts[r.Intn(n)]++
	}
	expected := float64(draws) / n
	for i, c := range counts {
		if dev := math.Abs(float64(c)-expected) / expected; dev > 0.02 {
			t.Errorf("bucket %d deviates %.3f from uniform", i, dev)
		}
	}
}

func TestFastRandSeedDecorrelation(t *testing.T) {
	// Systems constructed in the same tick get near-identical nanosecond
	// seeds; their first draws must not correlate
	const streams, depth = 64, 8
	for d := range depth {
		var seen [streams]uint64
		for s := range streams {
			r := NewFastRand(uint64(1_700_000_000_000_000_000 + s))
			for range d + 1 {
				seen[s] = r.Next()
			}
		}
		// Top 8 bits of each stream should not collapse to a few values
		var distinct = make(map[uint8]bool)
		for _, v := range seen {
			distinct[uint8(v>>56)] = true
		}
		if len(distinct) < streams/2 {
			t.Errorf("draw %d: only %d distinct high bytes across %d streams", d, len(distinct), streams)
		}
	}
}
