package physics

import (
	"math"
	"testing"

	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

func v3(x, y, z float64) vmath.Vec3 {
	return vmath.Vec3{X: vmath.FromFloat(x), Y: vmath.FromFloat(y), Z: vmath.FromFloat(z)}
}

func v3f(v vmath.Vec3) vmath.Vec3F { return vmath.V3ToFloat(v) }

func TestElasticCollision3DEqualMassSwap(t *testing.T) {
	posA, posB := v3(0, 0, 0), v3(1, 0, 0)
	velA, velB := v3(5, 0, 0), v3(0, 0, 0)

	nA, nB, ok := ElasticCollision3D(posA, posB, velA, velB, vmath.Scale, vmath.Scale, vmath.Scale)
	if !ok {
		t.Fatal("approaching bodies must collide")
	}
	if math.Abs(vmath.ToFloat(nA.X)) > 1e-4 {
		t.Fatalf("A retained velocity %v, want ~0", vmath.ToFloat(nA.X))
	}
	if math.Abs(vmath.ToFloat(nB.X)-5) > 1e-4 {
		t.Fatalf("B velocity %v, want 5", vmath.ToFloat(nB.X))
	}
}

func TestElasticCollision3DConservesMomentum(t *testing.T) {
	posA, posB := v3(0, 0, 0), v3(1, 2, 0)
	velA, velB := v3(4, 1, -2), v3(-1, 0, 1)
	massA, massB := vmath.Scale*2, vmath.Scale*5

	pBefore := vmath.V3Add(vmath.V3Scale(velA, massA), vmath.V3Scale(velB, massB))
	nA, nB, ok := ElasticCollision3D(posA, posB, velA, velB, massA, massB, vmath.Scale)
	if !ok {
		t.Skip("configuration is separating")
	}
	pAfter := vmath.V3Add(vmath.V3Scale(nA, massA), vmath.V3Scale(nB, massB))

	for _, d := range []int64{pAfter.X - pBefore.X, pAfter.Y - pBefore.Y, pAfter.Z - pBefore.Z} {
		if math.Abs(vmath.ToFloat(d)) > 1e-3 {
			t.Fatalf("momentum drift %v", vmath.ToFloat(d))
		}
	}
}

func TestElasticCollision3DSeparatingIsNoop(t *testing.T) {
	posA, posB := v3(0, 0, 0), v3(1, 0, 0)
	velA, velB := v3(-5, 0, 0), v3(0, 0, 0)
	nA, nB, ok := ElasticCollision3D(posA, posB, velA, velB, vmath.Scale, vmath.Scale, vmath.Scale)
	if ok {
		t.Fatal("separating bodies must not collide")
	}
	if nA != velA || nB != velB {
		t.Fatal("no-op collision altered velocities")
	}
}

func TestElasticCollision3DCoincidentIsNoop(t *testing.T) {
	p := v3(1, 1, 1)
	if _, _, ok := ElasticCollision3D(p, p, v3(1, 0, 0), v3(-1, 0, 0),
		vmath.Scale, vmath.Scale, vmath.Scale); ok {
		t.Fatal("coincident bodies must not resolve")
	}
}

func TestElasticCollision3DInPlaceMatchesValue(t *testing.T) {
	posA, posB := v3(0, 0, 0), v3(1, 2, -1)
	velA, velB := v3(4, 1, -2), v3(-1, 0, 1)

	wantA, wantB, wantOK := ElasticCollision3D(posA, posB, velA, velB,
		vmath.Scale*2, vmath.Scale*5, vmath.Scale/2)

	gotA, gotB := velA, velB
	gotOK := ElasticCollision3DInPlace(&posA, &posB, &gotA, &gotB,
		vmath.Scale*2, vmath.Scale*5, vmath.Scale/2)

	if gotOK != wantOK {
		t.Fatalf("in-place ok = %v, want %v", gotOK, wantOK)
	}
	if gotA != wantA || gotB != wantB {
		t.Fatalf("in-place diverged: %v/%v vs %v/%v", gotA, gotB, wantA, wantB)
	}
}

func TestElasticCollision3DFMatchesFixed(t *testing.T) {
	posA, posB := v3(0, 0, 0), v3(1, 2, -1)
	velA, velB := v3(4, 1, -2), v3(-1, 0, 1)

	fixA, fixB, fixOK := ElasticCollision3D(posA, posB, velA, velB,
		vmath.Scale*2, vmath.Scale*5, vmath.Scale/2)

	fpA, fpB := v3f(posA), v3f(posB)
	fvA, fvB := v3f(velA), v3f(velB)
	fltOK := ElasticCollision3DF(&fpA, &fpB, &fvA, &fvB, 2, 5, 0.5)

	if fixOK != fltOK {
		t.Fatalf("fixed/float collision disagree: %v vs %v", fixOK, fltOK)
	}
	if !fixOK {
		return
	}
	check := func(name string, fixed int64, flt float64) {
		if e := math.Abs(vmath.ToFloat(fixed) - flt); e > 1e-4 {
			t.Fatalf("%s diverged by %g", name, e)
		}
	}
	check("A.X", fixA.X, fvA.X)
	check("A.Y", fixA.Y, fvA.Y)
	check("B.X", fixB.X, fvB.X)
	check("B.Y", fixB.Y, fvB.Y)
}

func TestSeparateOverlap3D(t *testing.T) {
	posA, posB := v3(0, 0, 0), v3(1, 0, 0)
	r := vmath.Scale // radius 1 each -> minimum separation 2

	nA, nB, ok := SeparateOverlap3D(posA, posB, r, r, vmath.Scale, vmath.Scale)
	if !ok {
		t.Fatal("overlapping spheres must separate")
	}
	d := vmath.ToFloat(vmath.V3Mag(vmath.V3Sub(nB, nA)))
	if d < 2.0 {
		t.Fatalf("separation %v, want >= 2", d)
	}
	// equal masses split the correction evenly
	if math.Abs(vmath.ToFloat(nA.X)+vmath.ToFloat(nB.X)-1) > 1e-4 {
		t.Fatal("equal masses did not split the correction evenly")
	}

	if _, _, ok := SeparateOverlap3D(v3(0, 0, 0), v3(5, 0, 0), r, r, vmath.Scale, vmath.Scale); ok {
		t.Fatal("non-overlapping spheres must not separate")
	}
}

func TestSeparateOverlap3DFMatchesFixed(t *testing.T) {
	pa, pb := vmath.Vec3F{X: 0}, vmath.Vec3F{X: 1}
	if !SeparateOverlap3DF(&pa, &pb, 1, 1, 1, 1) {
		t.Fatal("float overlap must separate")
	}
	if d := pb.X - pa.X; d < 2.0 {
		t.Fatalf("float separation %v, want >= 2", d)
	}
}

func TestGravitationalAccel3DDirectionAndFalloff(t *testing.T) {
	a := GravitationalAccel3D(v3(0, 0, 0), v3(4, 0, 0), vmath.Scale*10, vmath.Scale)
	if a.X <= 0 {
		t.Fatalf("acceleration = %v, want toward B", vmath.ToFloat(a.X))
	}
	near := GravitationalAccel3D(v3(0, 0, 0), v3(2, 0, 0), vmath.Scale*10, vmath.Scale)
	if near.X <= a.X {
		t.Fatal("gravity did not increase with proximity")
	}
}

func TestGravitationalAccelWithRepulsion3D(t *testing.T) {
	const repulsionR = 3.0
	rr := vmath.FromFloat(repulsionR)
	strength := vmath.FromFloat(5)

	// inside the repulsion radius: pushed away from B
	in := GravitationalAccelWithRepulsion3D(v3(0, 0, 0), v3(1, 0, 0),
		vmath.Scale*10, vmath.Scale, rr, strength)
	if in.X >= 0 {
		t.Fatalf("inside repulsion: ax = %v, want negative", vmath.ToFloat(in.X))
	}

	// outside: attracted toward B
	out := GravitationalAccelWithRepulsion3D(v3(0, 0, 0), v3(8, 0, 0),
		vmath.Scale*10, vmath.Scale, rr, strength)
	if out.X <= 0 {
		t.Fatalf("outside repulsion: ax = %v, want positive", vmath.ToFloat(out.X))
	}

	if z := GravitationalAccelWithRepulsion3D(v3(0, 0, 0), v3(0, 0, 0),
		vmath.Scale, vmath.Scale, rr, strength); z != (vmath.Vec3{}) {
		t.Fatal("coincident bodies must yield zero acceleration")
	}
}

func TestReflectAxis3D(t *testing.T) {
	pos, vel := vmath.FromFloat(-5), vmath.FromFloat(-3)
	if !ReflectAxis3D(&pos, &vel, 0, vmath.FromFloat(10), vmath.Scale/2) {
		t.Fatal("below-low reflection not reported")
	}
	if pos != 0 || vel <= 0 {
		t.Fatalf("pos = %d, vel = %d after low reflection", pos, vel)
	}

	pos, vel = vmath.FromFloat(15), vmath.FromFloat(3)
	if !ReflectAxis3D(&pos, &vel, 0, vmath.FromFloat(10), vmath.Scale/2) {
		t.Fatal("above-high reflection not reported")
	}
	if pos != vmath.FromFloat(10) || vel >= 0 {
		t.Fatalf("pos = %d, vel = %d after high reflection", pos, vel)
	}

	pos, vel = vmath.FromFloat(5), vmath.FromFloat(3)
	if ReflectAxis3D(&pos, &vel, 0, vmath.FromFloat(10), vmath.Scale) {
		t.Fatal("in-range value reported a reflection")
	}
}

func TestV3NormalizeUnit(t *testing.T) {
	n := vmath.V3Normalize(v3(3, 4, 12)) // magnitude 13
	if m := vmath.ToFloat(vmath.V3Mag(n)); math.Abs(m-1) > 1e-5 {
		t.Fatalf("normalized magnitude = %v", m)
	}
	if vmath.V3Normalize(vmath.Vec3{}) != (vmath.Vec3{}) {
		t.Fatal("zero vector must normalize to zero")
	}
}

func TestV3ClampMagnitude(t *testing.T) {
	v := v3(30, 40, 0) // magnitude 50
	c := vmath.V3ClampMagnitude(v, vmath.FromFloat(5))
	if m := vmath.ToFloat(vmath.V3Mag(c)); math.Abs(m-5) > 1e-4 {
		t.Fatalf("clamped magnitude = %v, want 5", m)
	}
	small := v3(1, 1, 1)
	if vmath.V3ClampMagnitude(small, vmath.FromFloat(100)) != small {
		t.Fatal("under-limit vector was modified")
	}
}

func TestV3DampDtBounds(t *testing.T) {
	v := v3(10, 0, 0)
	// dt large enough to over-decay must clamp at zero, not invert
	d := vmath.V3DampDt(v, 0, vmath.FromFloat(10))
	if d.X != 0 {
		t.Fatalf("over-decay produced %v, want 0", vmath.ToFloat(d.X))
	}
	// no decay
	if n := vmath.V3DampDt(v, vmath.Scale, vmath.FromFloat(1)); n != v {
		t.Fatal("factor=Scale must not change the vector")
	}
}
