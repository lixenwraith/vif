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

// TestV3TurnsAndMirrorsAboutAnAxis: a cross product stands square to both by the
// right hand; a turn about an axis keeps a vector's length and its part along the
// axis and undoes by the opposite turn; a mirror flips the part along the normal.
func TestV3TurnsAndMirrorsAboutAnAxis(t *testing.T) {
	if got := V3Cross(Vec3{X: 1}, Vec3{Y: 1}); got != (Vec3{Z: 1}) {
		t.Fatalf("x cross y = %v, want z", got)
	}
	if got := V3Lerp(Vec3{-3, 0, 8}, Vec3{11, 4, -8}, 0.25); got != (Vec3{0.5, 1, 4}) {
		t.Fatalf("V3Lerp a quarter along = %v", got)
	}
	rng := NewFastRand(0x7a)
	vec := func() Vec3 {
		return Vec3{rng.Float64()*20 - 10, rng.Float64()*20 - 10, rng.Float64()*20 - 10}
	}
	for range 5000 {
		v, w, axis := vec(), vec(), V3Normalize(vec())
		c := V3Cross(v, w)
		if math.Abs(V3Dot(c, v)) > 1e-9 || math.Abs(V3Dot(c, w)) > 1e-9 || !nearV3(V3Cross(w, v), V3Scale(c, -1)) {
			t.Fatalf("%v cross %v = %v, not square to both or not antisymmetric", v, w, c)
		}
		angle := rng.Float64()*2*TwoPi - TwoPi
		r := V3Rotate(v, axis, angle)
		// The tables step a 1024th of a turn, so a turn is exact to that step's sine
		if math.Abs(V3Mag(r)-V3Mag(v)) > 0.01*V3Mag(v) || math.Abs(V3Dot(r, axis)-V3Dot(v, axis)) > 0.01*V3Mag(v) {
			t.Fatalf("%v turned %v about %v is %v", v, angle, axis, r)
		}
		if back := V3Rotate(r, axis, -angle); V3Mag(V3Sub(back, v)) > 0.02*V3Mag(v) {
			t.Fatalf("%v turned %v about %v and back is %v", v, angle, axis, back)
		}
		m := V3Reflect(v, axis)
		if math.Abs(V3Dot(m, axis)+V3Dot(v, axis)) > 1e-9 || V3Mag(V3Sub(V3Reflect(m, axis), v)) > 1e-9 {
			t.Fatalf("%v mirrored in %v is %v", v, axis, m)
		}
	}
}

// nearV3 reports two vectors equal to a part in 1e9 an axis, which leaves a form
// through a volume its own order of operations
func nearV3(a, b Vec3) bool {
	near := func(a, b float64) bool {
		return math.Abs(a-b) <= 1e-9*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
	}
	return near(a.X, b.X) && near(a.Y, b.Y) && near(a.Z, b.Z)
}

// TestThreeAxesInOnePlaneAreTwo: a vector with no depth turns about z, mirrors
// in a flat normal and lies in an ellipsoid as its two parts do in the plane.
func TestThreeAxesInOnePlaneAreTwo(t *testing.T) {
	rng := NewFastRand(0x2d)
	for range 5000 {
		x, y := rng.Float64()*40-20, rng.Float64()*40-20
		angle := rng.Float64()*2*TwoPi - TwoPi
		rx, ry := RotateVector(x, y, angle)
		if got := V3Rotate(Vec3{X: x, Y: y}, Vec3{Z: 1}, angle); !nearV3(got, Vec3{X: rx, Y: ry}) || got.Z != 0 {
			t.Fatalf("(%v, %v) turned %v about z is %v, the plane's (%v, %v)", x, y, angle, got, rx, ry)
		}
		nx, ny := Normalize2D(rng.Float64()*2-1, rng.Float64()*2-1)
		mx, my := Reflect(x, y, nx, ny)
		if got := V3Reflect(Vec3{X: x, Y: y}, Vec3{X: nx, Y: ny}); !nearV3(got, Vec3{X: mx, Y: my}) || got.Z != 0 {
			t.Fatalf("(%v, %v) mirrored in (%v, %v) is %v, the plane's (%v, %v)", x, y, nx, ny, got, mx, my)
		}
		radiusX, radiusY := rng.Float64()*12, rng.Float64()*12
		invX, invY := EllipseInvRadiiSq(radiusX, radiusY)
		inv := EllipsoidInvRadiiSq(radiusX, radiusY, rng.Float64()*12)
		if !nearV3(Vec3{X: EllipsoidDistSq(Vec3{X: x, Y: y}, inv)}, Vec3{X: EllipseDistSq(x, y, invX, invY)}) {
			t.Fatalf("(%v, %v) lies %v into the ellipsoid and %v into its ellipse", x, y,
				EllipsoidDistSq(Vec3{X: x, Y: y}, inv), EllipseDistSq(x, y, invX, invY))
		}
		px, py, cx, cy := rng.Intn(40), rng.Intn(40), rng.Intn(40), rng.Intn(40)
		if EllipsoidContainsPoint(Point3{X: px, Y: py, Z: 3}, Point3{X: cx, Y: cy, Z: 3}, inv) != EllipseContainsPoint(px, py, cx, cy, invX, invY) {
			t.Fatalf("cell (%d, %d) around (%d, %d) parts the ellipsoid from its ellipse", px, py, cx, cy)
		}
	}
	inv := EllipsoidInvRadiiSq(4, 2, 3)
	if !EllipsoidContains(Vec3{Z: 3}, inv) || EllipsoidContains(Vec3{Z: 3.01}, inv) || EllipsoidContains(Vec3{X: 3, Y: 1.5, Z: 2}, inv) {
		t.Fatal("an ellipsoid ends on its depth radius, and a corner of its box lies outside")
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
}

// TestAspectScalesVerticalExtents: the reference aspect leaves authored geometry
// exactly as written, and a square cell doubles every vertical extent of it while
// a vertical distance already reads as circular.
func TestAspectScalesVerticalExtents(t *testing.T) {
	for _, y := range []float64{0, 1, 2.5, -3.75, 100} {
		if RefAspect.Y(y) != y || RefAspect.Circular(y) != 2*y || RefAspect.FromCircular(2*y) != y {
			t.Fatalf("reference aspect moved %v", y)
		}
	}
	square := Aspect(1)
	if square.Y(2.5) != 5 || square.YInt(3) != 6 || square.Circular(4) != 4 || square.FromCircular(4) != 4 {
		t.Fatal("a square cell must double an authored Y and leave a distance as it is")
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
	// Over the whole orbit they stand evenly, the first half a spacing in
	for i, angle := range DistributeAngles(FindUnblockedArcs(make([]bool, EllipseSampleCount)), 4) {
		if want := TwoPi/8 + float64(i)*TwoPi/4; math.Abs(angle-want) > 1e-12 {
			t.Fatalf("angle %d of four round the orbit = %v, want %v", i, angle, want)
		}
	}
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
