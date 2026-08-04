package vmath

import (
	"math"
	"testing"
)

// rotToRad converts Q32.32 rotation units (Scale = 2pi) to radians
func rotToRad(a int64) float64 { return ToFloat(a) * TwoPi }

// angDelta returns the shortest absolute difference between two radian angles
func angDelta(a, b float64) float64 {
	d := math.Mod(a-b, TwoPi)
	if d > math.Pi {
		d -= TwoPi
	}
	if d < -math.Pi {
		d += TwoPi
	}
	return math.Abs(d)
}

func TestSinCosAgainstMath(t *testing.T) {
	// index truncation bounds the error at one LUT step: 2pi/1024
	const tol = 0.0065
	for i := range 8192 {
		turn := float64(i) / 8192
		a := int64(turn * ScaleF)
		rad := turn * TwoPi
		if e := math.Abs(ToFloat(Sin(a)) - math.Sin(rad)); e > tol {
			t.Fatalf("Sin(%v turn) error %g", turn, e)
		}
		if e := math.Abs(ToFloat(Cos(a)) - math.Cos(rad)); e > tol {
			t.Fatalf("Cos(%v turn) error %g", turn, e)
		}
	}
}

func TestSinCosUnitCircle(t *testing.T) {
	for i := range LUTSize {
		a := int64(i) << (Shift - 10)
		s, c := ToFloat(Sin(a)), ToFloat(Cos(a))
		if e := math.Abs(s*s + c*c - 1); e > 1e-6 {
			t.Fatalf("index %d: sin^2+cos^2 off by %g", i, e)
		}
	}
}

func TestSinFCosFMatchFixed(t *testing.T) {
	const tol = 0.0065 // allows a one-index disagreement between the two paths
	for i := range 4096 {
		turn := float64(i) / 4096
		a := int64(turn * ScaleF)
		rad := turn * TwoPi
		if e := math.Abs(ToFloat(Sin(a)) - SinF(rad)); e > tol {
			t.Fatalf("Sin/SinF diverge at %v turn by %g", turn, e)
		}
		if e := math.Abs(ToFloat(Cos(a)) - CosF(rad)); e > tol {
			t.Fatalf("Cos/CosF diverge at %v turn by %g", turn, e)
		}
	}
}

func TestAtan2FRange(t *testing.T) {
	rng := NewFastRand(0xA7A2)
	for range 50000 {
		dx := float64(signedRand(rng, 1<<33))
		dy := float64(signedRand(rng, 1<<33))
		if a := Atan2F(dy, dx); a < 0 || a >= TwoPi {
			t.Fatalf("Atan2F(%v,%v) = %v out of [0, 2pi)", dy, dx, a)
		}
	}
	// regression: near-zero |dy| with dx > 0 quantized the ratio to zero and
	// returned a full rotation instead of zero
	if a := Atan2F(-1, 1<<40); a != 0 {
		t.Fatalf("Atan2F Q4 boundary = %v, want 0", a)
	}
}

func TestAtan2FAxes(t *testing.T) {
	cases := []struct{ dy, dx, want float64 }{
		{0, 0, 0}, {0, 1, 0}, {1, 0, math.Pi / 2},
		{0, -1, math.Pi}, {-1, 0, 3 * math.Pi / 2},
	}
	for _, c := range cases {
		if got := Atan2F(c.dy, c.dx); math.Abs(got-c.want) > 1e-12 {
			t.Errorf("Atan2F(%v,%v) = %v, want %v", c.dy, c.dx, got, c.want)
		}
	}
}

func TestAtan2FAgainstMath(t *testing.T) {
	// one octant spans 1024 entries: |d atan/dr| <= 1 bounds the error at 1/1023
	const tol = 0.0025
	rng := NewFastRand(0xA7A3)
	for range 50000 {
		dx := float64(signedRand(rng, 1<<33))
		dy := float64(signedRand(rng, 1<<33))
		if dx == 0 && dy == 0 {
			continue
		}
		if e := angDelta(Atan2F(dy, dx), math.Atan2(dy, dx)); e > tol {
			t.Fatalf("Atan2F(%v,%v) delta %g", dy, dx, e)
		}
	}
}

func TestExpDecayBoundsAndMonotonicity(t *testing.T) {
	if got := ExpDecay(0); got != Scale {
		t.Errorf("ExpDecay(0) = %d, want Scale", got)
	}
	prev := int64(math.MaxInt64)
	for c := range ExpLUTMaxInput + 64 {
		v := ExpDecay(c)
		if v < 0 || v > Scale {
			t.Fatalf("ExpDecay(%d) = %d out of [0, Scale]", c, v)
		}
		if v > prev {
			t.Fatalf("ExpDecay not monotonic at %d: %d > %d", c, v, prev)
		}
		prev = v
	}
}

func TestExpDecayAgainstMathExp(t *testing.T) {
	for _, c := range []int{0, 1, 10, 30, 100, 255, 400, 511, 512, 5000} {
		x := float64(min(c, ExpLUTMaxInput))
		want := math.Exp(-x / ExpLUTDecayK)
		got := ToFloat(ExpDecay(c))
		if math.Abs(got-want) > want*5e-3+1e-9 {
			t.Errorf("ExpDecay(%d) = %g, want %g", c, got, want)
		}
	}
}

func TestExpDecayFMatchesExpDecay(t *testing.T) {
	for c := range 600 {
		if e := math.Abs(ToFloat(ExpDecay(c)) - ExpDecayF(c)); e > 1e-6 {
			t.Fatalf("ExpDecay/ExpDecayF diverge at %d by %g", c, e)
		}
	}
}

func TestExpDecayScaled(t *testing.T) {
	boost := FromFloat(3.0)
	if got := ExpDecayScaled(0, boost); got != Scale+boost {
		t.Errorf("ExpDecayScaled(0) = %d, want %d", got, Scale+boost)
	}
	if got := ExpDecayScaled(ExpLUTMaxInput, boost); got <= Scale || got > Scale+boost {
		t.Errorf("ExpDecayScaled saturation = %d", got)
	}
}
