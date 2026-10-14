package vmath

import (
	"math"
	"testing"
)

func TestNormalize2DUnitMagnitude(t *testing.T) {
	rng := NewFastRand(3)
	for range 20_000 {
		x := float64(signedRand(rng, 1<<33))
		y := float64(signedRand(rng, 1<<33))
		nx, ny := Normalize2D(x, y)
		if x == 0 && y == 0 {
			if nx != 0 || ny != 0 {
				t.Fatalf("zero vector normalized to (%v,%v)", nx, ny)
			}
			continue
		}
		if got := Magnitude(nx, ny); math.Abs(got-1.0) > 1e-12 {
			t.Fatalf("normalized magnitude = %v", got)
		}
	}
}

func TestVec2Geometry(t *testing.T) {
	x, y := 30.0, 40.0
	cx, cy := ClampMagnitude(x, y, 5.0)
	if got := Magnitude(cx, cy); math.Abs(got-5.0) > 1e-12 {
		t.Errorf("clamped magnitude = %v, want 5", got)
	}

	px, py := Perpendicular(x, y)
	if got := DotProduct(x, y, px, py); got != 0.0 {
		t.Errorf("perpendicular dot = %v, want 0", got)
	}

	rx, ry := RotateVector(7.5, 0, math.Pi/3)
	if got := Magnitude(rx, ry); math.Abs(got-7.5) > 0.05 {
		t.Errorf("rotation changed magnitude to %v", got)
	}

	rfx, rfy := Reflect(3, -5, 0, 1)
	if rfx != 3 || rfy != 5 {
		t.Errorf("Reflect = (%v,%v), want (3,5)", rfx, rfy)
	}
}

func TestV3Operations(t *testing.T) {
	a := Vec3{X: 3, Y: -4, Z: 12} // magnitude 13
	b := Vec3{X: -1.5, Y: 2, Z: 0.25}

	if got, want := V3Dot(a, b), -9.5; math.Abs(got-want) > 1e-12 {
		t.Errorf("V3Dot = %g, want %g", got, want)
	}
	if got := V3Mag(V3ClampMagnitude(a, 5)); math.Abs(got-5) > 1e-12 {
		t.Errorf("V3ClampMagnitude magnitude = %g, want 5", got)
	}
	if got := V3ClampMagnitude(a, 100); got != a {
		t.Errorf("under-limit clamp = %v, want %v", got, a)
	}
	if got := V3Damp(a, 0.25); got != (Vec3{0.75, -1, 3}) {
		t.Errorf("V3Damp = %v", got)
	}

	// over-decay must clamp at zero, not invert
	if d := V3DampDt(a, 0, 10); d != (Vec3{}) {
		t.Errorf("V3DampDt over-decay = %v, want zero", d)
	}
	if x, y := V3XY(a); x != a.X || y != a.Y {
		t.Error("V3XY")
	}
	if V3From2D(1, 2, 3) != (Vec3{1, 2, 3}) {
		t.Error("V3From2D")
	}
}

func TestEllipseContainsPointSymmetric(t *testing.T) {
	fInvRx, fInvRy := EllipseInvRadiiSq(4, 2)
	const cx, cy = 20, 20
	for dy := -4; dy <= 4; dy++ {
		for dx := -6; dx <= 6; dx++ {
			a := EllipseContainsPoint(cx+dx, cy+dy, cx, cy, fInvRx, fInvRy)
			b := EllipseContainsPoint(cx-dx, cy-dy, cx, cy, fInvRx, fInvRy)
			if a != b {
				t.Fatalf("offset (%d,%d): asymmetric %v vs %v", dx, dy, a, b)
			}
		}
	}
}

func TestEllipseGeometry(t *testing.T) {
	invRx, invRy := EllipseInvRadiiSq(4, 2)
	if !EllipseContains(4, 0, invRx, invRy) {
		t.Error("point on horizontal semi-axis must be contained")
	}
	if EllipseContains(4, 0.1, invRx, invRy) {
		t.Error("point beyond ellipse boundary must be excluded")
	}
	if got := EllipseAlpha(0, 0, invRx, invRy, 0.8); got != 0 {
		t.Errorf("center alpha = %v, want 0", got)
	}
	if got := EllipseAlpha(99, 0, invRx, invRy, 0.8); got != 0 {
		t.Errorf("outside alpha = %v, want 0", got)
	}

	if !CircleContains(3, 0, 9) || CircleContains(3, 0.1, 9) {
		t.Error("circle containment boundary")
	}
	for _, v := range []float64{0, 1, 2.5, -3.75, 100} {
		if got := ScaleFromCircular(ScaleToCircular(v)); got != v {
			t.Errorf("circular scale round trip %v -> %v", v, got)
		}
	}
}

func TestFindUnblockedArcsUniform(t *testing.T) {
	free := make([]bool, EllipseSampleCount)
	segments := FindUnblockedArcs(free)
	if !IsFullCircle(segments) {
		t.Fatalf("all-free must be a full circle, got %v", segments)
	}
	if math.Abs(TotalArcLength(segments)-TwoPi) > 1e-12 {
		t.Fatalf("full-circle length = %v, want %v", TotalArcLength(segments), TwoPi)
	}

	blocked := make([]bool, EllipseSampleCount)
	for i := range blocked {
		blocked[i] = true
	}
	if got := FindUnblockedArcs(blocked); got != nil {
		t.Fatalf("all-blocked must be nil, got %v", got)
	}
}

func TestDistributeAnglesWithinSegments(t *testing.T) {
	blocked := make([]bool, EllipseSampleCount)
	for i := range blocked {
		blocked[i] = i%3 == 0
	}
	segments := FindUnblockedArcs(blocked)
	for _, angle := range DistributeAngles(segments, 32) {
		if angle < 0 || angle >= TwoPi {
			t.Fatalf("angle %v out of range", angle)
		}
		inside := false
		for _, segment := range segments {
			if NormalizeAngle(angle-segment.StartAngle) <= segment.Length+1e-12 {
				inside = true
				break
			}
		}
		if !inside {
			t.Fatalf("angle %v outside segments %v", angle, segments)
		}
	}
}

func TestAngleDiffRangeAndAntisymmetry(t *testing.T) {
	rng := NewFastRand(15)
	for range 20_000 {
		a := rng.Float64() * TwoPi
		b := rng.Float64() * TwoPi
		d := AngleDiff(a, b)
		if d < -math.Pi || d > math.Pi {
			t.Fatalf("AngleDiff(%v,%v) = %v", a, b, d)
		}
		if reverse := AngleDiff(b, a); math.Abs(reverse+d) > 1e-12 {
			t.Fatalf("AngleDiff not antisymmetric: %v vs %v", d, reverse)
		}
	}
}
