package vmath

import (
	"math"
	"testing"
)

func TestNormalize2DFUnitMagnitude(t *testing.T) {
	rng := NewFastRand(3)
	for range 20_000 {
		x := float64(signedRand(rng, 1<<33))
		y := float64(signedRand(rng, 1<<33))
		nx, ny := Normalize2DF(x, y)
		if x == 0 && y == 0 {
			if nx != 0 || ny != 0 {
				t.Fatalf("zero vector normalized to (%v,%v)", nx, ny)
			}
			continue
		}
		if got := MagnitudeF(nx, ny); math.Abs(got-1.0) > 1e-12 {
			t.Fatalf("normalized magnitude = %v", got)
		}
	}
}

func TestVec2FGeometry(t *testing.T) {
	x, y := 30.0, 40.0
	cx, cy := ClampMagnitudeF(x, y, 5.0)
	if got := MagnitudeF(cx, cy); math.Abs(got-5.0) > 1e-12 {
		t.Errorf("clamped magnitude = %v, want 5", got)
	}

	px, py := PerpendicularF(x, y)
	if got := DotProductF(x, y, px, py); got != 0.0 {
		t.Errorf("perpendicular dot = %v, want 0", got)
	}

	rx, ry := RotateVectorF(7.5, 0, math.Pi/3)
	if got := MagnitudeF(rx, ry); math.Abs(got-7.5) > 0.05 {
		t.Errorf("rotation changed magnitude to %v", got)
	}

	rfx, rfy := ReflectF(3, -5, 0, 1)
	if rfx != 3 || rfy != 5 {
		t.Errorf("ReflectF = (%v,%v), want (3,5)", rfx, rfy)
	}
}

func TestEllipseFGeometry(t *testing.T) {
	invRx, invRy := EllipseInvRadiiSqF(4, 2)
	if !EllipseContainsF(4, 0, invRx, invRy) {
		t.Error("point on horizontal semi-axis must be contained")
	}
	if EllipseContainsF(4, 0.1, invRx, invRy) {
		t.Error("point beyond ellipse boundary must be excluded")
	}
	if got := EllipseAlphaF(0, 0, invRx, invRy, 0.8); got != 0 {
		t.Errorf("center alpha = %v, want 0", got)
	}
	if got := EllipseAlphaF(99, 0, invRx, invRy, 0.8); got != 0 {
		t.Errorf("outside alpha = %v, want 0", got)
	}

	if !CircleContainsF(3, 0, 9) || CircleContainsF(3, 0.1, 9) {
		t.Error("circle containment boundary")
	}
	for _, v := range []float64{0, 1, 2.5, -3.75, 100} {
		if got := ScaleFromCircularF(ScaleToCircularF(v)); got != v {
			t.Errorf("circular scale round trip %v -> %v", v, got)
		}
	}
}

func TestFindUnblockedArcsFUniform(t *testing.T) {
	free := make([]bool, EllipseSampleCount)
	segments := FindUnblockedArcsF(free)
	if !IsFullCircleF(segments) {
		t.Fatalf("all-free must be a full circle, got %v", segments)
	}
	if math.Abs(TotalArcLengthF(segments)-TwoPi) > 1e-12 {
		t.Fatalf("full-circle length = %v, want %v", TotalArcLengthF(segments), TwoPi)
	}

	blocked := make([]bool, EllipseSampleCount)
	for i := range blocked {
		blocked[i] = true
	}
	if got := FindUnblockedArcsF(blocked); got != nil {
		t.Fatalf("all-blocked must be nil, got %v", got)
	}
}

func TestDistributeAnglesFWithinSegments(t *testing.T) {
	blocked := make([]bool, EllipseSampleCount)
	for i := range blocked {
		blocked[i] = i%3 == 0
	}
	segments := FindUnblockedArcsF(blocked)
	for _, angle := range DistributeAnglesF(segments, 32) {
		if angle < 0 || angle >= TwoPi {
			t.Fatalf("angle %v out of range", angle)
		}
		inside := false
		for _, segment := range segments {
			if NormalizeAngleF(angle-segment.StartAngle) <= segment.Length+1e-12 {
				inside = true
				break
			}
		}
		if !inside {
			t.Fatalf("angle %v outside segments %v", angle, segments)
		}
	}
}

func TestAngleDiffFRangeAndAntisymmetry(t *testing.T) {
	rng := NewFastRand(15)
	for range 20_000 {
		a := rng.Float64() * TwoPi
		b := rng.Float64() * TwoPi
		d := AngleDiffF(a, b)
		if d < -math.Pi || d > math.Pi {
			t.Fatalf("AngleDiffF(%v,%v) = %v", a, b, d)
		}
		if reverse := AngleDiffF(b, a); math.Abs(reverse+d) > 1e-12 {
			t.Fatalf("AngleDiffF not antisymmetric: %v vs %v", d, reverse)
		}
	}
}
