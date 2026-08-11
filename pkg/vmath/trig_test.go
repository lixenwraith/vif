package vmath

import (
	"math"
	"testing"
)

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
		rad := turn * TwoPi
		if e := math.Abs(SinF(rad) - math.Sin(rad)); e > tol {
			t.Fatalf("SinF(%v turn) error %g", turn, e)
		}
		if e := math.Abs(CosF(rad) - math.Cos(rad)); e > tol {
			t.Fatalf("CosF(%v turn) error %g", turn, e)
		}
	}
}

func TestSinCosUnitCircle(t *testing.T) {
	for i := range LUTSize {
		s, c := SinF_LUT[i], CosF_LUT[i]
		if e := math.Abs(s*s + c*c - 1); e > 1e-6 {
			t.Fatalf("index %d: sin^2+cos^2 off by %g", i, e)
		}
	}
}

func TestSinFCosFFloorNegativeAngles(t *testing.T) {
	step := TwoPi / float64(LUTSize)
	for _, angle := range []float64{-0.25 * step, -0.75 * step, -1.25 * step} {
		wantIdx := int(math.Floor(angle*radToIndex)) & LUTMask
		if got := SinF(angle); got != SinF_LUT[wantIdx] {
			t.Errorf("SinF(%v) = %v, want LUT[%d] = %v", angle, got, wantIdx, SinF_LUT[wantIdx])
		}
		if got := CosF(angle); got != CosF_LUT[wantIdx] {
			t.Errorf("CosF(%v) = %v, want LUT[%d] = %v", angle, got, wantIdx, CosF_LUT[wantIdx])
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
	if got := ExpDecayF(0); got != 1.0 {
		t.Errorf("ExpDecayF(0) = %v, want 1", got)
	}
	prev := math.Inf(1)
	for c := range ExpLUTMaxInput + 64 {
		v := ExpDecayF(c)
		if v < 0 || v > 1.0 {
			t.Fatalf("ExpDecayF(%d) = %v out of [0, 1]", c, v)
		}
		if v > prev {
			t.Fatalf("ExpDecayF not monotonic at %d: %v > %v", c, v, prev)
		}
		prev = v
	}
}

func TestExpDecayAgainstMathExp(t *testing.T) {
	for _, c := range []int{0, 1, 10, 30, 100, 255, 400, 511, 512, 5000} {
		x := float64(min(c, ExpLUTMaxInput))
		want := math.Exp(-x / ExpLUTDecayK)
		got := ExpDecayF(c)
		if math.Abs(got-want) > want*5e-3+1e-9 {
			t.Errorf("ExpDecayF(%d) = %g, want %g", c, got, want)
		}
	}
}

func TestExpDecayScaledFBounds(t *testing.T) {
	const boost = 3.0
	if got := ExpDecayScaledF(0, boost); got != 1.0+boost {
		t.Errorf("ExpDecayScaledF(0) = %v, want %v", got, 1.0+boost)
	}
	if got := ExpDecayScaledF(ExpLUTMaxInput, boost); got <= 1.0 || got > 1.0+boost {
		t.Errorf("ExpDecayScaledF saturation = %v", got)
	}
}
