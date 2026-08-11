package vmath

import (
	"math"
	"testing"
)

func TestV3FOperations(t *testing.T) {
	a := Vec3F{X: 3, Y: -4, Z: 12} // magnitude 13
	b := Vec3F{X: -1.5, Y: 2, Z: 0.25}

	if got, want := V3FDot(a, b), -9.5; math.Abs(got-want) > 1e-12 {
		t.Errorf("V3FDot = %g, want %g", got, want)
	}
	if got := V3FMag(V3FClampMagnitude(a, 5)); math.Abs(got-5) > 1e-12 {
		t.Errorf("V3FClampMagnitude magnitude = %g, want 5", got)
	}
	if got := V3FClampMagnitude(a, 100); got != a {
		t.Errorf("under-limit clamp = %v, want %v", got, a)
	}
	if got := V3FDamp(a, 0.25); got != (Vec3F{0.75, -1, 3}) {
		t.Errorf("V3FDamp = %v", got)
	}

	// over-decay must clamp at zero, not invert
	if d := V3FDampDt(a, 0, 10); d != (Vec3F{}) {
		t.Errorf("V3FDampDt over-decay = %v, want zero", d)
	}
	if x, y := V3FXY(a); x != a.X || y != a.Y {
		t.Error("V3FXY")
	}
	if V3FFrom2D(1, 2, 3) != (Vec3F{1, 2, 3}) {
		t.Error("V3FFrom2D")
	}
}

func TestExpDecayScaledF(t *testing.T) {
	for _, c := range []int{0, 1, 37, 255, 512, 5000} {
		got := ExpDecayScaledF(c, 3.0)
		want := 1.0 + 3.0*ExpDecayF(c)
		if math.Abs(got-want) > 1e-12 {
			t.Errorf("ExpDecayScaledF(%d) = %v, want %v", c, got, want)
		}
	}
}

func TestEllipseContainsPointFSymmetric(t *testing.T) {
	fInvRx, fInvRy := EllipseInvRadiiSqF(4, 2)
	const cx, cy = 20, 20
	for dy := -4; dy <= 4; dy++ {
		for dx := -6; dx <= 6; dx++ {
			a := EllipseContainsPointF(cx+dx, cy+dy, cx, cy, fInvRx, fInvRy)
			b := EllipseContainsPointF(cx-dx, cy-dy, cx, cy, fInvRx, fInvRy)
			if a != b {
				t.Fatalf("offset (%d,%d): asymmetric %v vs %v", dx, dy, a, b)
			}
		}
	}
}

func TestPointCenterFRoundTrip(t *testing.T) {
	for _, p := range []Point{{0, 0}, {3, -4}, {-9, 9}, {1000, -1000}} {
		px, py := p.CenterF()
		if got := PointAtF(px, py); got != p {
			t.Errorf("PointAtF(%v.CenterF()) = %v", p, got)
		}
	}
	// int() truncates toward zero; the grid needs floor
	if got := PointAtF(-0.25, -1.75); got != (Point{-1, -2}) {
		t.Errorf("PointAtF negative = %v, want (-1,-2)", got)
	}
}
