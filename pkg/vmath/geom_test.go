package vmath

import (
	"math"
	"testing"
)

func TestNormalize2DUnitMagnitude(t *testing.T) {
	rng := NewFastRand(3)
	for range 20000 {
		x, y := signedRand(rng, 1<<33), signedRand(rng, 1<<33)
		nx, ny := Normalize2D(x, y)
		if x == 0 && y == 0 {
			if nx != 0 || ny != 0 {
				t.Fatalf("zero vector normalized to (%d,%d)", nx, ny)
			}
			continue
		}
		if m := ToFloat(Magnitude(nx, ny)); math.Abs(m-1) > 1e-6 {
			t.Fatalf("Normalize2D(%d,%d) magnitude %v", x, y, m)
		}
	}
}

func TestNormalize2DMatchesFloat(t *testing.T) {
	rng := NewFastRand(4)
	for range 10000 {
		x, y := signedRand(rng, 1<<33), signedRand(rng, 1<<33)
		if x == 0 && y == 0 {
			continue
		}
		nx, ny := Normalize2D(x, y)
		fx, fy := Normalize2DF(float64(x), float64(y))
		if math.Abs(ToFloat(nx)-fx) > 1e-6 || math.Abs(ToFloat(ny)-fy) > 1e-6 {
			t.Fatalf("Normalize2D/2DF diverge at (%d,%d)", x, y)
		}
	}
}

func TestMagnitudeMatchesFloat(t *testing.T) {
	rng := NewFastRand(8)
	for range 10000 {
		x, y := signedRand(rng, 1<<34), signedRand(rng, 1<<34)
		got := float64(Magnitude(x, y))
		want := math.Hypot(float64(x), float64(y))
		if math.Abs(got-want) > want*1e-9+1 {
			t.Fatalf("Magnitude(%d,%d) = %v, want %v", x, y, got, want)
		}
	}
}

func TestMagnitudeSqConsistency(t *testing.T) {
	x, y := FromFloat(3), FromFloat(4)
	if got := ToFloat(MagnitudeSq(x, y)); math.Abs(got-25) > 1e-6 {
		t.Errorf("MagnitudeSq = %v, want 25", got)
	}
	if got := ToFloat(Magnitude(x, y)); math.Abs(got-5) > 1e-6 {
		t.Errorf("Magnitude = %v, want 5", got)
	}
}

func TestMagnitudeApproxErrorBound(t *testing.T) {
	// alpha-max-beta-min with (1, 0.375) peaks at +6.8%, NOT the ~4% claimed in
	// older doc comments; DistanceApprox callers must tolerate this
	const maxRel = 0.07
	rng := NewFastRand(9)
	worst := 0.0
	for range 20000 {
		x, y := signedRand(rng, 1<<34), signedRand(rng, 1<<34)
		exact := float64(Magnitude(x, y))
		approx := float64(MagnitudeApprox(x, y))
		if exact < 1 {
			continue
		}
		rel := math.Abs(approx-exact) / exact
		worst = math.Max(worst, rel)
		if rel > maxRel {
			t.Fatalf("MagnitudeApprox(%d,%d) rel error %g", x, y, rel)
		}
	}
	t.Logf("worst observed relative error: %.4f", worst)
}

func TestPerpendicularOrthogonal(t *testing.T) {
	rng := NewFastRand(10)
	for range 5000 {
		x, y := signedRand(rng, 1<<33), signedRand(rng, 1<<33)
		px, py := Perpendicular(x, y)
		if d := DotProduct(x, y, px, py); d != 0 {
			t.Fatalf("Perpendicular(%d,%d) dot = %d", x, y, d)
		}
	}
}

func TestRotateVectorPreservesMagnitude(t *testing.T) {
	v := FromFloat(7.5)
	base := Magnitude(v, 0)
	for i := range LUTSize {
		angle := int64(i) << (Shift - 10)
		rx, ry := RotateVector(v, 0, angle)
		m := Magnitude(rx, ry)
		if math.Abs(float64(m-base)) > float64(base)*1e-5+16 {
			t.Fatalf("rotation by index %d changed magnitude %d -> %d", i, base, m)
		}
	}
}

func TestReflectPreservesMagnitude(t *testing.T) {
	vx, vy := FromFloat(3), FromFloat(-5)
	base := ToFloat(Magnitude(vx, vy))
	for i := range LUTSize {
		angle := int64(i) << (Shift - 10)
		nx, ny := Cos(angle), Sin(angle)
		rx, ry := Reflect(vx, vy, nx, ny)
		if m := ToFloat(Magnitude(rx, ry)); math.Abs(m-base) > base*1e-5+1e-6 {
			t.Fatalf("reflect at index %d: |v| %v -> %v", i, base, m)
		}
	}
}

func TestReflectAxisHelpers(t *testing.T) {
	vx, vy := FromFloat(2), FromFloat(-3)
	if x, y := ReflectAxisX(vx, vy); x != -vx || y != vy {
		t.Error("ReflectAxisX")
	}
	if x, y := ReflectAxisY(vx, vy); x != vx || y != -vy {
		t.Error("ReflectAxisY")
	}
}

func TestClampMagnitude(t *testing.T) {
	maxMag := FromFloat(5)

	x, y := FromFloat(1), FromFloat(2)
	if cx, cy := ClampMagnitude(x, y, maxMag); cx != x || cy != y {
		t.Error("under-limit vector was modified")
	}

	x, y = FromFloat(30), FromFloat(40) // magnitude 50
	cx, cy := ClampMagnitude(x, y, maxMag)
	if m := ToFloat(Magnitude(cx, cy)); math.Abs(m-5) > 1e-5 {
		t.Errorf("clamped magnitude = %v, want 5", m)
	}
	// direction preserved
	if math.Abs(float64(cx)*4-float64(cy)*3) > float64(Scale) {
		t.Error("clamp changed direction")
	}
}

// --- ellipse ---

func TestEllipseContainsMatchesFloatOracle(t *testing.T) {
	const rxF, ryF = 5.0, 2.5
	invRx, invRy := EllipseInvRadiiSq(FromFloat(rxF), FromFloat(ryF))
	rng := NewFastRand(11)
	for range 50000 {
		dx, dy := signedRand(rng, 1<<35), signedRand(rng, 1<<35) // +/- 8 cells
		fx, fy := ToFloat(dx), ToFloat(dy)
		norm := fx*fx/(rxF*rxF) + fy*fy/(ryF*ryF)
		if math.Abs(norm-1) < 1e-3 {
			continue // fixed-point rounding band around the boundary
		}
		if got := EllipseContains(dx, dy, invRx, invRy); got != (norm <= 1.0) {
			t.Fatalf("EllipseContains(%v,%v) = %v, norm = %v", fx, fy, got, norm)
		}
	}
}

func TestEllipseContainsMatchesFloatVariant(t *testing.T) {
	const rxF, ryF = 4.0, 2.0
	invRx, invRy := EllipseInvRadiiSq(FromFloat(rxF), FromFloat(ryF))
	fInvRx, fInvRy := EllipseInvRadiiSqF(rxF, ryF)
	rng := NewFastRand(12)
	for range 20000 {
		dx, dy := signedRand(rng, 1<<35), signedRand(rng, 1<<35)
		fx, fy := ToFloat(dx), ToFloat(dy)
		a := EllipseContains(dx, dy, invRx, invRy)
		b := EllipseContainsF(fx, fy, fInvRx, fInvRy)
		if a != b && math.Abs(EllipseDistSqF(fx, fy, fInvRx, fInvRy)-1) > 1e-4 {
			t.Fatalf("fixed/float ellipse disagree at (%v,%v): %v vs %v", fx, fy, a, b)
		}
	}
}

func TestEllipseContainsPointCentered(t *testing.T) {
	invRx, invRy := EllipseInvRadiiSq(FromFloat(3), FromFloat(1.5))
	if !EllipseContainsPoint(10, 10, 10, 10, invRx, invRy) {
		t.Error("center cell must be contained")
	}
	if !EllipseContainsPoint(13, 10, 10, 10, invRx, invRy) {
		t.Error("cell on the X semi-axis must be contained")
	}
	if EllipseContainsPoint(14, 10, 10, 10, invRx, invRy) {
		t.Error("cell beyond the X semi-axis must be excluded")
	}
	if EllipseContainsPoint(10, 12, 10, 10, invRx, invRy) {
		t.Error("cell beyond the Y semi-axis must be excluded")
	}
}

func TestEllipseAlphaBounds(t *testing.T) {
	invRx, invRy := EllipseInvRadiiSq(FromFloat(4), FromFloat(2))
	maxAlpha := FromFloat(0.8)
	if got := EllipseAlpha(0, 0, invRx, invRy, maxAlpha); got != 0 {
		t.Errorf("center alpha = %d, want 0", got)
	}
	if got := EllipseAlpha(FromFloat(99), 0, invRx, invRy, maxAlpha); got != 0 {
		t.Errorf("outside alpha = %d, want 0", got)
	}
	edge := EllipseAlpha(FromFloat(3.99), 0, invRx, invRy, maxAlpha)
	if edge <= 0 || edge > maxAlpha {
		t.Errorf("edge alpha = %d, want (0, %d]", edge, maxAlpha)
	}
}

func TestCircleContains(t *testing.T) {
	rSq := Mul(FromFloat(3), FromFloat(3))
	if !CircleContains(FromFloat(3), 0, rSq) {
		t.Error("on-boundary point must be contained")
	}
	if CircleContains(FromFloat(3), FromFloat(0.1), rSq) {
		t.Error("outside point must be excluded")
	}
}

func TestScaleToFromCircularRoundTrip(t *testing.T) {
	v := FromFloat(3.5)
	if got := ScaleFromCircular(ScaleToCircular(v)); got != v {
		t.Errorf("round trip %d -> %d", v, got)
	}
}

// --- arcs ---

func TestFindUnblockedArcsUniform(t *testing.T) {
	free := make([]bool, EllipseSampleCount)
	segs := FindUnblockedArcs(free)
	if !IsFullCircle(segs) {
		t.Fatalf("all-free must be a full circle, got %v", segs)
	}
	if TotalArcLength(segs) != Scale {
		t.Fatalf("full circle length = %d, want Scale", TotalArcLength(segs))
	}

	blocked := make([]bool, EllipseSampleCount)
	for i := range blocked {
		blocked[i] = true
	}
	if segs := FindUnblockedArcs(blocked); segs != nil {
		t.Fatalf("all-blocked must be nil, got %v", segs)
	}
	if FindUnblockedArcs(nil) != nil {
		t.Fatal("empty input must be nil")
	}
}

func TestFindUnblockedArcsTotalLength(t *testing.T) {
	const n = EllipseSampleCount
	const step = Scale / n
	rng := NewFastRand(13)
	for range 500 {
		blocked := make([]bool, n)
		freeCount := 0
		for i := range blocked {
			blocked[i] = rng.Intn(2) == 0
			if !blocked[i] {
				freeCount++
			}
		}
		if freeCount == 0 || freeCount == n {
			continue
		}
		if got, want := TotalArcLength(FindUnblockedArcs(blocked)), int64(freeCount)*step; got != want {
			t.Fatalf("total arc = %d, want %d (%d free of %d)", got, want, freeCount, n)
		}
	}
}

// inSegments reports whether angle falls inside any segment, handling wrap
func inSegments(segs []ArcSegment, angle int64) bool {
	for _, s := range segs {
		d := NormalizeAngle(angle - s.StartAngle)
		if d < s.Length {
			return true
		}
	}
	return false
}

func TestDistributeAnglesWithinSegments(t *testing.T) {
	const n = EllipseSampleCount
	rng := NewFastRand(14)
	for range 300 {
		blocked := make([]bool, n)
		free := 0
		for i := range blocked {
			blocked[i] = rng.Intn(3) == 0
			if !blocked[i] {
				free++
			}
		}
		if free == 0 {
			continue
		}
		segs := FindUnblockedArcs(blocked)
		for _, count := range []int{1, 3, 8} {
			for _, a := range DistributeAngles(segs, count) {
				if a < 0 || a >= Scale {
					t.Fatalf("angle %d out of range", a)
				}
				if !inSegments(segs, a) {
					t.Fatalf("angle %d outside every unblocked segment %v", a, segs)
				}
			}
		}
	}
}

func TestDistributeAnglesDegenerate(t *testing.T) {
	if DistributeAngles(nil, 4) != nil {
		t.Error("nil segments must yield nil")
	}
	if DistributeAngles([]ArcSegment{{Length: Scale}}, 0) != nil {
		t.Error("zero count must yield nil")
	}
}

func TestNormalizeAngleRange(t *testing.T) {
	for _, a := range []int64{0, -1, Scale, -Scale, 5*Scale + 7, -5*Scale - 7, math.MaxInt64, math.MinInt64} {
		if n := NormalizeAngle(a); n < 0 || n >= Scale {
			t.Fatalf("NormalizeAngle(%d) = %d", a, n)
		}
	}
	if got := NormalizeAngle(-1); got != Scale-1 {
		t.Errorf("NormalizeAngle(-1) = %d, want %d", got, Scale-1)
	}
	if got := NormalizeAngle(Scale + 7); got != 7 {
		t.Errorf("NormalizeAngle(Scale+7) = %d, want 7", got)
	}
}

func TestAngleDiffRangeAndAntisymmetry(t *testing.T) {
	rng := NewFastRand(15)
	for range 20000 {
		a := int64(rng.Next() & uint64(Mask))
		b := int64(rng.Next() & uint64(Mask))
		d := AngleDiff(a, b)
		if d < -Scale/2 || d > Scale/2 {
			t.Fatalf("AngleDiff(%d,%d) = %d out of range", a, b, d)
		}
		if r := AngleDiff(b, a); r != -d && Abs(d) != Scale/2 {
			t.Fatalf("AngleDiff not antisymmetric: %d vs %d", d, r)
		}
	}
}

func TestAngleToEllipsePosOnBoundary(t *testing.T) {
	rx, ry := FromFloat(6), FromFloat(3)
	invRx, invRy := EllipseInvRadiiSq(rx, ry)
	for i := range LUTSize {
		angle := int64(i) << (Shift - 10)
		px, py := AngleToEllipsePos(angle, 0, 0, rx, ry)
		d := EllipseDistSq(px, py, invRx, invRy)
		if math.Abs(ToFloat(d)-1) > 1e-4 {
			t.Fatalf("index %d: point off the ellipse boundary (normSq = %v)", i, ToFloat(d))
		}
	}
}

func TestArcFixedFloatParity(t *testing.T) {
	rng := NewFastRand(16)
	for range 300 {
		blocked := make([]bool, EllipseSampleCount)
		for i := range blocked {
			blocked[i] = rng.Intn(2) == 0
		}
		a := FindUnblockedArcs(blocked)
		b := FindUnblockedArcsF(blocked)
		if len(a) != len(b) {
			t.Fatalf("segment count diverged: %d vs %d", len(a), len(b))
		}
		for i := range a {
			if e := math.Abs(rotToRad(a[i].Length) - b[i].Length); e > 1e-6 {
				t.Fatalf("segment %d length diverged by %g rad", i, e)
			}
		}
	}
}

func TestScaleCircularParity(t *testing.T) {
	// Aspect correction must agree between the fixed and float paths, or
	// physics and rendering drift on the Y axis
	for _, f := range []float64{0, 1, 2.5, -3.75, 100} {
		v := FromFloat(f)
		if got, want := ToFloat(ScaleToCircular(v)), ScaleToCircularF(f); math.Abs(got-want) > 1e-9 {
			t.Errorf("ScaleToCircular(%v) = %v, want %v", f, got, want)
		}
		if got, want := ToFloat(ScaleFromCircular(v)), ScaleFromCircularF(f); math.Abs(got-want) > 1e-9 {
			t.Errorf("ScaleFromCircular(%v) = %v, want %v", f, got, want)
		}
	}
}

func TestEllipseContainsPointSymmetric(t *testing.T) {
	// Grid-index containment takes cell indices on both sides, so the result
	// must be symmetric about the center; asymmetry means a half-cell offset
	// leaked into the conversion
	invRx, invRy := EllipseInvRadiiSq(FromFloat(4), FromFloat(2))
	const cx, cy = 20, 20
	for dy := -4; dy <= 4; dy++ {
		for dx := -6; dx <= 6; dx++ {
			a := EllipseContainsPoint(cx+dx, cy+dy, cx, cy, invRx, invRy)
			b := EllipseContainsPoint(cx-dx, cy-dy, cx, cy, invRx, invRy)
			if a != b {
				t.Fatalf("asymmetric at offset (%d,%d): %v vs %v", dx, dy, a, b)
			}
		}
	}
}

func TestDotProductPerpendicularExact(t *testing.T) {
	rng := NewFastRand(0xD07)
	for range 20000 {
		x, y := signedRand(rng, 1<<34), signedRand(rng, 1<<34)
		px, py := Perpendicular(x, y)
		if d := DotProduct(x, y, px, py); d != 0 {
			t.Fatalf("DotProduct(%d,%d,perp) = %d, want exact 0", x, y, d)
		}
	}
}
