package vmath

import (
	"math"
	"math/big"
	"testing"
)

// mulRef is an exact 128-bit oracle mirroring Mul's truncate-toward-zero semantics
func mulRef(a, b int64) int64 {
	neg := (a < 0) != (b < 0)
	p := new(big.Int).Mul(
		new(big.Int).Abs(big.NewInt(a)),
		new(big.Int).Abs(big.NewInt(b)),
	)
	p.Rsh(p, uint(Shift))
	r := p.Int64()
	if neg {
		return -r
	}
	return r
}

// signedRand returns a value in [-bound, bound)
func signedRand(rng *FastRand, bound int64) int64 {
	return int64(rng.Next()%uint64(2*bound)) - bound
}

func TestMulMatchesExactOracle(t *testing.T) {
	rng := NewFastRand(0xC0FFEE)
	const bound = int64(1) << 46 // product stays below 2^93
	for range 50000 {
		a, b := signedRand(rng, bound), signedRand(rng, bound)
		if got, want := Mul(a, b), mulRef(a, b); got != want {
			t.Fatalf("Mul(%d,%d) = %d, want %d", a, b, got, want)
		}
	}
}

func TestMulIdentity(t *testing.T) {
	for _, v := range []int64{0, 1, -1, Half, -Half, Scale, -Scale, Scale * 7, 123456789} {
		if got := Mul(v, Scale); got != v {
			t.Errorf("Mul(%d, Scale) = %d", v, got)
		}
		if got := Mul(Scale, v); got != v {
			t.Errorf("Mul(Scale, %d) = %d", v, got)
		}
		if got := Mul(v, 0); got != 0 {
			t.Errorf("Mul(%d, 0) = %d", v, got)
		}
	}
}

func TestFromToFloatRoundTrip(t *testing.T) {
	const tol = 2.0 / ScaleF
	for _, f := range []float64{0, 0.5, -0.5, 1, -1, 1e-6, 1234.5678, -9876.54321, 65536.125} {
		if got := ToFloat(FromFloat(f)); math.Abs(got-f) > tol {
			t.Errorf("round trip %v -> %v (delta %g)", f, got, got-f)
		}
	}
}

func TestFromToIntRoundTrip(t *testing.T) {
	for _, i := range []int{0, 1, -1, 7, -7, 4095, -4095} {
		if got := ToInt(FromInt(i)); got != i {
			t.Errorf("round trip %d -> %d", i, got)
		}
	}
}

func TestDivPrecision(t *testing.T) {
	rng := NewFastRand(7)
	for range 20000 {
		a := signedRand(rng, 1<<44)
		b := signedRand(rng, 1<<40)
		if b > -(1<<20) && b < (1<<20) {
			continue // keep |a/b| well inside Q32.32 range
		}
		got := ToFloat(Div(a, b))
		want := float64(a) / float64(b)
		if math.Abs(got-want) > math.Abs(want)*1e-9+1e-9 {
			t.Fatalf("Div(%d,%d) = %g, want %g", a, b, got, want)
		}
	}
}

func TestDivByZero(t *testing.T) {
	if got := Div(Scale, 0); got != 0 {
		t.Errorf("Div(Scale, 0) = %d, want 0", got)
	}
	if got := MulDiv(Scale, Scale, 0); got != 0 {
		t.Errorf("MulDiv(_, _, 0) = %d, want 0", got)
	}
}

func TestSqrt(t *testing.T) {
	for _, f := range []float64{0.25, 1, 2, 10, 1000, 65536} {
		got := ToFloat(Sqrt(FromFloat(f)))
		want := math.Sqrt(f)
		if math.Abs(got-want) > want*1e-9 {
			t.Errorf("Sqrt(%v) = %v, want %v", f, got, want)
		}
	}
	if Sqrt(0) != 0 || Sqrt(-Scale) != 0 {
		t.Error("Sqrt of non-positive must be 0")
	}
}

func TestLerpEndpoints(t *testing.T) {
	a, b := FromFloat(-3), FromFloat(11)
	if got := Lerp(a, b, 0); got != a {
		t.Errorf("Lerp t=0 = %d, want %d", got, a)
	}
	if got := Lerp(a, b, Scale); got != b {
		t.Errorf("Lerp t=Scale = %d, want %d", got, b)
	}
	if got := ToFloat(Lerp(a, b, Half)); math.Abs(got-4) > 1e-6 {
		t.Errorf("Lerp t=0.5 = %v, want 4", got)
	}
}

func TestSignAbs(t *testing.T) {
	if Sign(-5) != -Scale || Sign(0) != 0 || Sign(5) != Scale {
		t.Error("Sign")
	}
	if Abs(-5) != 5 || Abs(5) != 5 || IntAbs(-5) != 5 {
		t.Error("Abs")
	}
}

// --- grid types ---

func TestPointCenterRoundTrip(t *testing.T) {
	for _, p := range []Point{{0, 0}, {1, 2}, {-1, -2}, {-7, 13}, {1000, -1000}} {
		if got := PointAt(p.Center()); got != p {
			t.Errorf("PointAt(%v.Center()) = %v", p, got)
		}
	}
}

func TestPointCenterMatchesCenteredFromGrid(t *testing.T) {
	for _, p := range []Point{{0, 0}, {3, -4}, {-9, 9}} {
		px, py := p.Center()
		cx, cy := CenteredFromGrid(p.X, p.Y)
		if px != cx || py != cy {
			t.Errorf("%v: (%d,%d) != (%d,%d)", p, px, py, cx, cy)
		}
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

func TestFastRandIntnRange(t *testing.T) {
	r := NewFastRand(5)
	for _, n := range []int{1, 2, 7, 31, 1000} {
		for range 5000 {
			if v := r.Intn(n); v < 0 || v >= n {
				t.Fatalf("Intn(%d) = %d", n, v)
			}
		}
	}
	if r.Intn(0) != 0 || r.Intn(-5) != 0 {
		t.Error("Intn of non-positive must be 0")
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
