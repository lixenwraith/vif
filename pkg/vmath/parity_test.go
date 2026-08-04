package vmath

import (
	"math"
	"testing"
)

func assertV3(t *testing.T, name string, a Vec3, b Vec3F, tol float64) {
	t.Helper()
	if math.Abs(ToFloat(a.X)-b.X) > tol ||
		math.Abs(ToFloat(a.Y)-b.Y) > tol ||
		math.Abs(ToFloat(a.Z)-b.Z) > tol {
		t.Errorf("%s: fixed %v != float %v", name, V3ToFloat(a), b)
	}
}

func TestV3FloatParity(t *testing.T) {
	a := Vec3{FromFloat(3), FromFloat(-4), FromFloat(12)} // magnitude 13
	b := Vec3{FromFloat(-1.5), FromFloat(2), FromFloat(0.25)}
	af, bf := V3ToFloat(a), V3ToFloat(b)

	if e := math.Abs(ToFloat(V3Dot(a, b)) - V3FDot(af, bf)); e > 1e-6 {
		t.Errorf("V3Dot/V3FDot diverge by %g", e)
	}
	assertV3(t, "V3ClampMagnitude",
		V3ClampMagnitude(a, FromFloat(5)), V3FClampMagnitude(af, 5), 1e-4)
	assertV3(t, "V3ClampMagnitude under limit",
		V3ClampMagnitude(a, FromFloat(100)), V3FClampMagnitude(af, 100), 1e-9)
	assertV3(t, "V3Damp", V3Damp(a, Scale/4), V3FDamp(af, 0.25), 1e-6)
	assertV3(t, "V3DampDt",
		V3DampDt(a, FromFloat(0.5), FromFloat(0.5)), V3FDampDt(af, 0.5, 0.5), 1e-6)

	// over-decay must clamp at zero, not invert
	if d := V3FDampDt(af, 0, 10); d != (Vec3F{}) {
		t.Errorf("V3FDampDt over-decay = %v, want zero", d)
	}
	if x, y := V3FXY(af); x != af.X || y != af.Y {
		t.Error("V3FXY")
	}
	if V3FFrom2D(1, 2, 3) != (Vec3F{1, 2, 3}) {
		t.Error("V3FFrom2D")
	}
}

func TestExpDecayScaledFMatchesFixed(t *testing.T) {
	for _, c := range []int{0, 1, 37, 255, 512, 5000} {
		got := ExpDecayScaledF(c, 3.0)
		want := ToFloat(ExpDecayScaled(c, FromFloat(3.0)))
		if math.Abs(got-want) > 1e-6 {
			t.Errorf("ExpDecayScaled(%d) = %v fixed, %v float", c, want, got)
		}
	}
}

func TestEllipseContainsPointFMatchesFixed(t *testing.T) {
	invRx, invRy := EllipseInvRadiiSq(FromFloat(4), FromFloat(2))
	fInvRx, fInvRy := EllipseInvRadiiSqF(4, 2)
	const cx, cy = 20, 20
	for dy := -4; dy <= 4; dy++ {
		for dx := -6; dx <= 6; dx++ {
			a := EllipseContainsPoint(cx+dx, cy+dy, cx, cy, invRx, invRy)
			b := EllipseContainsPointF(cx+dx, cy+dy, cx, cy, fInvRx, fInvRy)
			if a != b {
				t.Fatalf("offset (%d,%d): fixed %v, float %v", dx, dy, a, b)
			}
		}
	}
}

func TestPointCenterFParity(t *testing.T) {
	for _, p := range []Point{{0, 0}, {3, -4}, {-9, 9}, {1000, -1000}} {
		fx, fy := p.Center()
		px, py := p.CenterF()
		if math.Abs(ToFloat(fx)-px) > 1e-9 || math.Abs(ToFloat(fy)-py) > 1e-9 {
			t.Errorf("%v: fixed (%v,%v) != float (%v,%v)", p, ToFloat(fx), ToFloat(fy), px, py)
		}
		if got := PointAtF(px, py); got != p {
			t.Errorf("PointAtF(%v.CenterF()) = %v", p, got)
		}
	}
	// int() truncates toward zero; the grid needs floor
	if got := PointAtF(-0.25, -1.75); got != (Point{-1, -2}) {
		t.Errorf("PointAtF negative = %v, want (-1,-2)", got)
	}
}
