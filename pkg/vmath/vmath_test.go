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
	for _, p := range []Point{{0, 0}, {1, 2}, {-1, -2}, {-7, 13}, {1000, -1000}} {
		px, py := p.Center()
		if got := PointAt(px, py); got != p {
			t.Errorf("PointAt(%v.Center()) = %v", p, got)
		}
	}
	// int() truncates toward zero; the grid needs floor
	if got := PointAt(-0.25, -1.75); got != (Point{-1, -2}) {
		t.Errorf("PointAt negative = %v, want (-1,-2)", got)
	}
}

func TestPointAddSub(t *testing.T) {
	a, b := Point{3, -4}, Point{-1, 6}
	if got := a.Add(b); got != (Point{2, 2}) {
		t.Errorf("Add = %v", got)
	}
	if got := a.Sub(b); got != (Point{4, -10}) {
		t.Errorf("Sub = %v", got)
	}
}

func TestAreaContainsBoundaries(t *testing.T) {
	a := Area{X: 2, Y: 3, Width: 4, Height: 5} // x in [2,6), y in [3,8)
	inside := []Point{{2, 3}, {5, 7}, {3, 5}}
	outside := []Point{{1, 3}, {6, 7}, {2, 2}, {2, 8}}
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
	if got := (Area{X: 2, Y: 3, Width: 4, Height: 5}).Center(); got != (Point{4, 5}) {
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
	if got := a.RandomPoint(r1); got != (Point{1, 1}) {
		t.Fatalf("RandomPoint = %v", got)
	}
	if r1.Next() != r2.Next() {
		t.Fatal("single-cell area consumed RNG state; spawn determinism would drift")
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
